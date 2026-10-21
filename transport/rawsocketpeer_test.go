package transport_test

import (
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// rawSocketMagic is the RawSocket protocol magic byte (spec §5.4.1).
const rawSocketMagic = 0x7f

// newPipedRawSocket sets up a server-side rawSocketPeer backed by a
// net.Pipe(). The client side of the pipe is returned to the caller
// for raw-byte-level driving of test scenarios. The handshake bytes
// (4 bytes each direction) are exchanged before this returns, so the
// caller is at the message-frame boundary.
//
// The recvLimit is rounded UP to the nearest power-of-2 ≥ 512; pass
// 1 to get the smallest available limit (512).
func newPipedRawSocket(t *testing.T, recvLimit int) (clientConn net.Conn, peer wamp.Peer) {
	t.Helper()
	client, server := net.Pipe()

	// Client-side handshake: magic + (sendLimit<<4 | json) + 0 + 0.
	// We pick sendLimit=0 (which becomes 2^9=512) and serialization=1 (JSON).
	handshakeDone := make(chan struct{})
	go func() {
		defer close(handshakeDone)
		if _, err := client.Write([]byte{rawSocketMagic, 0<<4 | 1, 0, 0}); err != nil {
			t.Errorf("client handshake write: %v", err)
			return
		}
		var resp [4]byte
		if _, err := io.ReadFull(client, resp[:]); err != nil {
			t.Errorf("client handshake read: %v", err)
			return
		}
		if resp[0] != rawSocketMagic {
			t.Errorf("client handshake: got magic %#x, want %#x", resp[0], rawSocketMagic)
		}
	}()

	logger := log.New(io.Discard, "", 0)
	peer, err := transport.AcceptRawSocket(server, logger, recvLimit, 0)
	require.NoError(t, err)

	select {
	case <-handshakeDone:
	case <-time.After(time.Second):
		require.FailNow(t, "client handshake did not complete")
	}
	return client, peer
}

// writeFrame writes a 4-byte length prefix + body to conn. The first
// byte of the prefix is the WAMP rawsocket type byte (0=msg, 1=ping,
// 2=pong); the next 3 bytes are the body length in big-endian.
func writeFrame(t *testing.T, conn net.Conn, frameType byte, body []byte) {
	t.Helper()
	header := []byte{
		frameType,
		byte((len(body) >> 16) & 0xff),
		byte((len(body) >> 8) & 0xff),
		byte(len(body) & 0xff),
	}
	if _, err := conn.Write(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if len(body) > 0 {
		if _, err := conn.Write(body); err != nil {
			t.Fatalf("write body: %v", err)
		}
	}
}

// TestRawSocketC3_EOFAfterHandshake verifies that the rawSocketPeer
// recvHandler/sendHandler exit cleanly when the remote side closes
// the connection immediately after the handshake — no goroutine leak.
// Adjacent to gammazero/nexus#242, which fixed two similar leak
// vectors on the websocket peer.
func TestRawSocketC3_EOFAfterHandshake(t *testing.T) {
	client, peer := newPipedRawSocket(t, 1)
	t.Cleanup(func() { peer.Close() })

	// Remote drops the connection without sending any WAMP frame.
	require.NoError(t, client.Close())

	// recvHandler should observe EOF, defer-close peer.Recv(), and
	// the router-side observer (us) reads zero/closed.
	select {
	case msg, ok := <-peer.Recv():
		require.False(t, ok, "expected closed Recv, got msg %v", msg)
	case <-time.After(time.Second):
		require.FailNow(t, "peer.Recv() did not close after remote EOF")
	}
}

// TestRawSocketC1_OversizedFrame verifies that the rawSocketPeer
// rejects a frame whose declared length exceeds the negotiated
// recvLimit. The spec (§5.4) allows the receiver to close the
// connection on protocol violation.
func TestRawSocketC1_OversizedFrame(t *testing.T) {
	const recvLimit = 1 // → 2^9 = 512 bytes
	client, peer := newPipedRawSocket(t, recvLimit)
	t.Cleanup(func() { peer.Close() })

	// Declare a body length of 2048 — bigger than the 512-byte limit.
	// Don't bother sending the body; recvHandler should reject on the
	// header alone.
	header := []byte{0x00, 0x00, 0x08, 0x00}
	_, err := client.Write(header)
	require.NoError(t, err)

	// recvHandler should close the connection. peer.Recv() should
	// close as a result.
	select {
	case msg, ok := <-peer.Recv():
		require.False(t, ok, "expected closed Recv, got msg %v", msg)
	case <-time.After(time.Second):
		require.FailNow(t, "peer.Recv() did not close after oversized frame")
	}
	_ = client.Close()
}

// TestRawSocketC2_TruncatedFrame verifies that the rawSocketPeer
// handles a frame whose body is short of the declared length. The
// underlying io.ReadFull should return ErrUnexpectedEOF; recvHandler
// logs and exits.
func TestRawSocketC2_TruncatedFrame(t *testing.T) {
	client, peer := newPipedRawSocket(t, 4) // 2^13 = 8192
	t.Cleanup(func() { peer.Close() })

	// Declare a body length of 100, then send only 50 bytes before
	// closing the conn. recvHandler will block in io.ReadFull
	// expecting 100 and observe the EOF after 50.
	header := []byte{0x00, 0x00, 0x00, 100}
	_, err := client.Write(header)
	require.NoError(t, err)
	_, err = client.Write(make([]byte, 50))
	require.NoError(t, err)
	require.NoError(t, client.Close())

	select {
	case msg, ok := <-peer.Recv():
		require.False(t, ok, "expected closed Recv, got msg %v", msg)
	case <-time.After(time.Second):
		require.FailNow(t, "peer.Recv() did not close after truncated frame")
	}
}

// TestRawSocketC5_DeserializerErrorAborts pins WAMP §5.3.1 strict
// behavior: when a frame's body is well-framed but its payload fails
// deserialization, recvHandler logs the error, closes the connection,
// and exits — same teardown as a torn read. The peer's Recv channel
// closes; no further frames are processed even if more arrive on the
// wire.
//
// Prior to the recvHandler fix in this commit, recvHandler silently
// skipped the bad frame (`continue MsgLoop`) and kept reading —
// permissive but at odds with the spec.
func TestRawSocketC5_DeserializerErrorAborts(t *testing.T) {
	client, peer := newPipedRawSocket(t, 4) // 2^13 = 8192
	t.Cleanup(func() { peer.Close() })

	// First (and only) frame the server processes: body is
	// `not valid json {` — well-framed but fails JSON
	// deserialization.
	writeFrame(t, client, 0x00, []byte("not valid json {"))

	// peer.Recv() must close (recvHandler returned via the new
	// abort-on-deserialize-error path). A subsequent frame is
	// irrelevant — we already proved recvHandler stopped.
	select {
	case msg, ok := <-peer.Recv():
		require.False(t, ok, "expected closed Recv after malformed frame, got msg %v", msg)
	case <-time.After(time.Second):
		require.FailNow(t, "peer.Recv() did not close after malformed frame")
	}
	_ = client.Close()
}

// TestRawSocketC9_PingStorm verifies the rawSocketPeer handles a
// rapid burst of PING frames inline (no per-ping goroutine spawn,
// no memory growth, connection stays responsive). After the storm,
// a regular WAMP message must still be delivered.
func TestRawSocketC9_PingStorm(t *testing.T) {
	client, peer := newPipedRawSocket(t, 6) // 2^15 = 32768
	t.Cleanup(func() { peer.Close() })

	const pingCount = 1000

	// Concurrently consume PONGs from the server side so the
	// client's send buffer doesn't fill up (net.Pipe is unbuffered).
	pongDone := make(chan struct{})
	go func() {
		defer close(pongDone)
		for range pingCount {
			var resp [4]byte
			if _, err := io.ReadFull(client, resp[:]); err != nil {
				return
			}
			if resp[0] != 0x02 {
				t.Errorf("expected PONG (type 0x02), got %#x", resp[0])
				return
			}
			length := int(resp[1])<<16 | int(resp[2])<<8 | int(resp[3])
			if length > 0 {
				if _, err := io.CopyN(io.Discard, client, int64(length)); err != nil {
					return
				}
			}
		}
	}()

	// Burst PINGs of varying sizes (1, 2, ..., pingCount bytes
	// modulo a small max so we stay well within recvLimit).
	for i := 1; i <= pingCount; i++ {
		size := (i % 64) + 1
		writeFrame(t, client, 0x01, make([]byte, size))
	}

	// Wait for all PONGs.
	select {
	case <-pongDone:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "PING storm did not produce expected PONG count")
	}

	// Connection must still be responsive: send a HELLO and observe
	// it delivered to peer.Recv().
	hello := wamp.Hello{Realm: "test", Details: wamp.Dict{}}
	helloBytes, err := (&serialize.JSONSerializer{}).Serialize(&hello)
	require.NoError(t, err)
	writeFrame(t, client, 0x00, helloBytes)

	select {
	case msg, ok := <-peer.Recv():
		require.True(t, ok, "Recv closed unexpectedly after PING storm")
		_, isHello := msg.(*wamp.Hello)
		require.True(t, isHello, "expected HELLO after PING storm, got %T", msg)
	case <-time.After(time.Second):
		require.FailNow(t, "peer.Recv() did not deliver post-storm HELLO")
	}
	_ = client.Close()
}

// TestRawSocketC9_PingPong verifies that PONG frames received from
// the remote side are silently consumed (per spec §5.4.4): the
// peer's recvHandler discards the body and continues. This is the
// inverse of the PING test — exercises the case 2 branch.
func TestRawSocketC9_PingPong(t *testing.T) {
	client, peer := newPipedRawSocket(t, 4)
	t.Cleanup(func() { peer.Close() })

	// Send a PONG frame followed by a HELLO.
	writeFrame(t, client, 0x02, []byte("opaque pong payload"))
	hello := wamp.Hello{Realm: "test", Details: wamp.Dict{}}
	helloBytes, err := (&serialize.JSONSerializer{}).Serialize(&hello)
	require.NoError(t, err)
	writeFrame(t, client, 0x00, helloBytes)

	select {
	case msg, ok := <-peer.Recv():
		require.True(t, ok, "Recv closed after PONG")
		_, isHello := msg.(*wamp.Hello)
		require.True(t, isHello, "expected HELLO after PONG discard, got %T", msg)
	case <-time.After(time.Second):
		require.FailNow(t, "peer.Recv() did not deliver post-PONG HELLO")
	}
	_ = client.Close()
}

// TestRawSocketC3_HandshakeFailureDoesNotLeak verifies that when the
// server-side handshake itself fails (e.g. client sends garbage in
// the magic byte), AcceptRawSocket returns an error and no peer is
// constructed — no goroutines started, nothing to leak.
func TestRawSocketC3_HandshakeFailureDoesNotLeak(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Garbage in magic byte → server should error out.
		_, _ = client.Write([]byte{0x00, 0x00, 0x00, 0x00})
	}()

	logger := log.New(io.Discard, "", 0)
	_, err := transport.AcceptRawSocket(server, logger, 1, 0)
	require.Error(t, err, "expected handshake error on bad magic")

	wg.Wait()
}
