package transport_test

import (
	"io"
	"log"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// RawSocket frame types, in the first byte of a frame header.
const (
	rawSocketMagic = 0x7f
	frameMessage   = 0x00
	framePing      = 0x01
	framePong      = 0x02
)

// verifyNoLeaks checks, when the test ends, that it left no goroutines
// running. Call it before anything else in the test, so that the check runs
// after all other cleanups.
func verifyNoLeaks(t *testing.T) {
	t.Helper()
	ignore := goleak.IgnoreCurrent()
	t.Cleanup(func() { goleak.VerifyNone(t, ignore) })
}

// newPipedRawSocket returns a server-side rawsocket peer on one end of a
// net.Pipe, and the other end, from which the test plays the client. The
// handshake is done, with JSON serialization. recvLimit is the most bytes the
// peer accepts in a message, rounded up to a power of two of at least 512.
// The peer and the client end are closed when the test ends.
func newPipedRawSocket(t *testing.T, recvLimit int) (net.Conn, wamp.Peer) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	handshakeErr := make(chan error, 1)
	go func() {
		// Magic byte, then the client's max length of 2^9 and JSON.
		if _, err := client.Write([]byte{rawSocketMagic, 0x01, 0, 0}); err != nil {
			handshakeErr <- err
			return
		}
		var resp [4]byte
		_, err := io.ReadFull(client, resp[:])
		handshakeErr <- err
	}()

	peer, err := transport.AcceptRawSocket(server, log.New(io.Discard, "", 0), recvLimit, 0)
	require.NoError(t, err)
	t.Cleanup(peer.Close)
	require.NoError(t, <-handshakeErr, "client handshake failed")
	return client, peer
}

// writeFrame writes a frame of the given type holding body to conn.
func writeFrame(t *testing.T, conn net.Conn, frameType byte, body []byte) {
	t.Helper()
	n := len(body)
	_, err := conn.Write([]byte{frameType, byte(n >> 16), byte(n >> 8), byte(n)})
	require.NoError(t, err)
	if n != 0 {
		_, err = conn.Write(body)
		require.NoError(t, err)
	}
}

// readFrame reads a frame from conn, and returns its type and body.
func readFrame(conn net.Conn) (byte, []byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return 0, nil, err
	}
	body := make([]byte, int(header[1])<<16|int(header[2])<<8|int(header[3]))
	if _, err := io.ReadFull(conn, body); err != nil {
		return 0, nil, err
	}
	return header[0], body, nil
}

// writeHello writes a HELLO message frame to conn.
func writeHello(t *testing.T, conn net.Conn) {
	t.Helper()
	b, err := (&serialize.JSONSerializer{}).Serialize(&wamp.Hello{Realm: "test", Details: wamp.Dict{}})
	require.NoError(t, err)
	writeFrame(t, conn, frameMessage, b)
}

// requireRecvHello fails if the next message from peer is not a HELLO.
func requireRecvHello(t *testing.T, peer wamp.Peer) {
	t.Helper()
	select {
	case msg, ok := <-peer.Recv():
		require.True(t, ok, "Recv closed")
		_, ok = msg.(*wamp.Hello)
		require.True(t, ok, "expected HELLO, got %T", msg)
	case <-time.After(testTimeout):
		require.FailNow(t, "HELLO not delivered")
	}
}

// requireRecvDone fails if the peer's Recv channel is not closed, without any
// message, within testTimeout.
func requireRecvDone(t *testing.T, peer wamp.Peer) {
	t.Helper()
	select {
	case msg, ok := <-peer.Recv():
		require.False(t, ok, "expected closed Recv, got %T", msg)
	case <-time.After(testTimeout):
		require.FailNow(t, "Recv not closed")
	}
}

// TestRawSocketEOFAfterHandshake verifies that the peer stops, and leaks no
// goroutines, when the remote peer closes the connection right after the
// handshake.
func TestRawSocketEOFAfterHandshake(t *testing.T) {
	verifyNoLeaks(t)
	client, peer := newPipedRawSocket(t, 0)

	require.NoError(t, client.Close())
	requireRecvDone(t, peer)
}

// TestRawSocketOversizedFrame verifies that the peer closes the connection
// when a frame header declares a length over the receive limit, without
// reading the body.
func TestRawSocketOversizedFrame(t *testing.T) {
	verifyNoLeaks(t)
	client, peer := newPipedRawSocket(t, 512)

	// Declare a 2048-byte body, over the 512-byte limit, and send no body.
	_, err := client.Write([]byte{frameMessage, 0x00, 0x08, 0x00})
	require.NoError(t, err)
	requireRecvDone(t, peer)
}

// TestRawSocketTruncatedFrame verifies that the peer stops when the
// connection closes before the whole body of a frame has arrived.
func TestRawSocketTruncatedFrame(t *testing.T) {
	verifyNoLeaks(t)
	client, peer := newPipedRawSocket(t, 512)

	_, err := client.Write([]byte{frameMessage, 0, 0, 100})
	require.NoError(t, err)
	_, err = client.Write(make([]byte, 50))
	require.NoError(t, err)
	require.NoError(t, client.Close())
	requireRecvDone(t, peer)
}

// TestRawSocketDeserializeErrorAborts verifies that a message that cannot be
// deserialized is answered with ABORT wamp.error.protocol_violation, and that
// the connection is then closed instead of reading further messages.
func TestRawSocketDeserializeErrorAborts(t *testing.T) {
	verifyNoLeaks(t)
	client, peer := newPipedRawSocket(t, 512)

	writeFrame(t, client, frameMessage, []byte("not valid json {"))

	frameType, body, err := readFrame(client)
	require.NoError(t, err)
	require.Equal(t, byte(frameMessage), frameType)
	msg, err := (&serialize.JSONSerializer{}).Deserialize(body)
	require.NoError(t, err)
	abort, ok := msg.(*wamp.Abort)
	require.True(t, ok, "expected ABORT, got %T", msg)
	require.Equal(t, wamp.ErrProtocolViolation, abort.Reason)

	_, _, err = readFrame(client)
	require.ErrorIs(t, err, io.EOF, "connection not closed after ABORT")
	requireRecvDone(t, peer)
}

// TestRawSocketPingStorm verifies that the peer answers each of many PINGs
// with a PONG holding the same body, and still delivers a message afterward.
func TestRawSocketPingStorm(t *testing.T) {
	verifyNoLeaks(t)
	client, peer := newPipedRawSocket(t, 512)

	const pings = 1000
	pongErr := make(chan error, 1)
	go func() {
		for i := 1; i <= pings; i++ {
			frameType, body, err := readFrame(client)
			if err == nil && (frameType != framePong || len(body) != i%64+1) {
				err = io.ErrUnexpectedEOF
			}
			if err != nil {
				pongErr <- err
				return
			}
		}
		pongErr <- nil
	}()

	for i := 1; i <= pings; i++ {
		writeFrame(t, client, framePing, make([]byte, i%64+1))
	}
	select {
	case err := <-pongErr:
		require.NoError(t, err, "wrong or missing PONG")
	case <-time.After(5 * time.Second):
		require.FailNow(t, "PONGs not received")
	}

	writeHello(t, client)
	requireRecvHello(t, peer)
}

// TestRawSocketPong verifies that the peer discards a PONG and keeps
// delivering messages.
func TestRawSocketPong(t *testing.T) {
	verifyNoLeaks(t)
	client, peer := newPipedRawSocket(t, 512)

	writeFrame(t, client, framePong, []byte("opaque pong payload"))
	writeHello(t, client)
	requireRecvHello(t, peer)
}

// TestRawSocketHandshakeFailure verifies that AcceptRawSocket fails, closes
// the connection, and leaks no goroutines, when the client sends a bad magic
// byte.
func TestRawSocketHandshakeFailure(t *testing.T) {
	verifyNoLeaks(t)
	client, server := net.Pipe()
	defer client.Close()

	go func() { _, _ = client.Write([]byte{0x00, 0x00, 0x00, 0x00}) }()

	_, err := transport.AcceptRawSocket(server, log.New(io.Discard, "", 0), 0, 0)
	require.Error(t, err)

	// The server end is closed, so the client sees EOF.
	_, err = client.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
}
