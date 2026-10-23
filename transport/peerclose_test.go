package transport_test

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// closeConcurrently calls Close from several goroutines at once, and then once
// more after they return.
func closeConcurrently(closePeer func()) {
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(closePeer)
	}
	wg.Wait()
	closePeer()
}

// TestCloseRawSocketPeerConcurrently checks that Close is idempotent and safe
// to call concurrently. A second Close used to panic with "close of closed
// channel".
func TestCloseRawSocketPeerConcurrently(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	go func() {
		// Client handshake: magic byte, then max length 2^9 and JSON.
		_, _ = client.Write([]byte{0x7f, 0x01, 0, 0})
		var resp [4]byte
		_, _ = io.ReadFull(client, resp[:])
		// Keep reading so the peer never blocks writing to the pipe.
		_, _ = io.Copy(io.Discard, client)
	}()

	peer, err := transport.AcceptRawSocket(server, log.New(io.Discard, "", 0), 0, 0)
	require.NoError(t, err)

	closeConcurrently(peer.Close)
}

// TestCloseWebsocketPeerConcurrently checks that Close is idempotent and safe
// to call concurrently. Concurrent calls used to all pass the check for an
// earlier Close, and then panic with "close of closed channel".
func TestCloseWebsocketPeerConcurrently(t *testing.T) {
	// The race needs several tries to show up.
	for range 500 {
		peer := transport.NewWebsocketPeer(newMockSession(), &serialize.JSONSerializer{},
			websocket.TextMessage, log.New(io.Discard, "", 0), 0, 0)
		closeConcurrently(peer.Close)
	}
}

// stuckRawConn is the server side of a raw socket connection to a client that
// completes the handshake and then stops reading, so writes block until the
// connection is closed. Reads after the handshake block until failRead is
// closed, which makes them fail, or the connection is closed.
type stuckRawConn struct {
	net.Conn

	readCount    atomic.Int32 // bytes read so far, up to the handshake size
	failRead     chan struct{}
	writeEntered chan struct{} // closed when a message write starts
	closed       chan struct{}
	enterOnce    sync.Once
	closeOnce    sync.Once
}

const rawHandshakeSize = 4

// newStuckRawConn returns the server side of a raw socket connection whose
// client has completed the handshake and then stopped reading.
func newStuckRawConn(t *testing.T) *stuckRawConn {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	go func() {
		// Client handshake: magic byte, then max length 2^9 and JSON.
		_, _ = client.Write([]byte{0x7f, 0x01, 0, 0})
		var resp [rawHandshakeSize]byte
		_, _ = io.ReadFull(client, resp[:])
		// Read nothing more.
	}()
	return &stuckRawConn{
		Conn:         server,
		failRead:     make(chan struct{}),
		writeEntered: make(chan struct{}),
		closed:       make(chan struct{}),
	}
}

func (c *stuckRawConn) Read(p []byte) (int, error) {
	if left := rawHandshakeSize - int(c.readCount.Load()); left > 0 {
		n, err := c.Conn.Read(p[:min(len(p), left)])
		c.readCount.Add(int32(n))
		return n, err
	}
	select {
	case <-c.failRead:
		return 0, errors.New("simulated read error")
	case <-c.closed:
		return 0, net.ErrClosed
	}
}

func (c *stuckRawConn) Write(p []byte) (int, error) {
	if c.readCount.Load() >= rawHandshakeSize {
		// Past the handshake, so this write is for a message.
		c.enterOnce.Do(func() { close(c.writeEntered) })
	}
	return c.Conn.Write(p)
}

func (c *stuckRawConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

// stuckRawPeer returns a raw socket peer whose sender is stuck writing a
// message to a client that is not reading.
func stuckRawPeer(t *testing.T) (wamp.Peer, *stuckRawConn) {
	conn := newStuckRawConn(t)
	peer, err := transport.AcceptRawSocket(conn, log.New(io.Discard, "", 0), 0, 0)
	require.NoError(t, err)
	peer.Send() <- &wamp.Hello{Realm: "test"}
	<-conn.writeEntered
	return peer, conn
}

// requireReturnsWithin fails if fn does not return within testTimeout.
func requireReturnsWithin(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		require.FailNow(t, what+" did not return")
	}
}

// TestRawSocketCloseUnblocksStuckWrite checks that Close does not wait forever
// for a sender that is stuck writing to a client that stopped reading.
func TestRawSocketCloseUnblocksStuckWrite(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	defer transport.SetSenderStopTimeout(shortStopDelay)()

	peer, conn := stuckRawPeer(t)
	defer conn.Close()

	requireReturnsWithin(t, "Close", peer.Close)
}

// TestRawSocketRecvErrorUnblocksStuckWrite checks that after a read error the
// receiver does not wait forever for a sender that is stuck writing.
func TestRawSocketRecvErrorUnblocksStuckWrite(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	defer transport.SetSenderStopTimeout(shortStopDelay)()

	peer, conn := stuckRawPeer(t)
	defer conn.Close()

	close(conn.failRead)
	requireReturnsWithin(t, "recvHandler", func() {
		for range peer.Recv() {
		}
	})
	requireReturnsWithin(t, "Close", peer.Close)
}
