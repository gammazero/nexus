package transport_test

import (
	"errors"
	"fmt"
	"log"
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

type mockWSConnection struct{}

func (m mockWSConnection) Close() error { return nil }

func (m mockWSConnection) WriteControl(messageType int, data []byte, deadline time.Time) error {
	return nil
}

func (m mockWSConnection) WriteMessage(messageType int, data []byte) error {
	return nil
}

func (m mockWSConnection) ReadMessage() (messageType int, p []byte, err error) {
	err = fmt.Errorf("implement me")
	return
}

func (m mockWSConnection) SetPongHandler(h func(appData string) error) {}

func (m mockWSConnection) SetPingHandler(h func(appData string) error) {}

func (m mockWSConnection) Subprotocol() string { return "" }

func newMockSession() transport.WebsocketConnection {
	return &mockWSConnection{}
}

func TestCloseWebsocketPeer(t *testing.T) {
	peer := transport.NewWebsocketPeer(newMockSession(), &serialize.JSONSerializer{}, websocket.TextMessage, log.Default(), 0, 0)

	// Close the client connection.
	peer.Close()

	// Try closing the client connection again. It should not cause an error.
	peer.Close()
}

var (
	errClosed      = errors.New("use of closed network connection")
	errReadFailed  = errors.New("simulated read error")
	errTimeout     = errors.New("i/o timeout")
	helloJSON      = []byte(`[1,"test",{}]`)
	testKeepAlive  = 100 * time.Millisecond
	shortStopDelay = 100 * time.Millisecond
	testTimeout    = 2 * time.Second
)

// blockingConn is a WebsocketConnection whose reads and writes block until
// the test releases them or the connection is closed. Closing the connection
// unblocks both, as with a gorilla websocket.Conn.
type blockingConn struct {
	readErr      chan struct{} // close to make ReadMessage return an error
	writeEntered chan struct{} // closed when WriteMessage is first called
	writeRelease chan struct{} // close to let WriteMessage succeed
	closeC       chan struct{} // closed by Close

	enterOnce sync.Once
	closeOnce sync.Once

	completedWrites atomic.Int32
	abortedWrites   atomic.Int32
}

func newBlockingConn() *blockingConn {
	return &blockingConn{
		readErr:      make(chan struct{}),
		writeEntered: make(chan struct{}),
		writeRelease: make(chan struct{}),
		closeC:       make(chan struct{}),
	}
}

func (c *blockingConn) ReadMessage() (int, []byte, error) {
	select {
	case <-c.readErr:
		return 0, nil, errReadFailed
	case <-c.closeC:
		return 0, nil, errClosed
	}
}

func (c *blockingConn) WriteMessage(int, []byte) error {
	c.enterOnce.Do(func() { close(c.writeEntered) })
	select {
	case <-c.writeRelease:
		c.completedWrites.Add(1)
		return nil
	case <-c.closeC:
		c.abortedWrites.Add(1)
		return errClosed
	}
}

func (c *blockingConn) Close() error {
	c.closeOnce.Do(func() { close(c.closeC) })
	return nil
}

func (c *blockingConn) isClosed() bool {
	select {
	case <-c.closeC:
		return true
	default:
		return false
	}
}

func (c *blockingConn) WriteControl(int, []byte, time.Time) error { return nil }
func (c *blockingConn) SetPongHandler(func(string) error)         {}
func (c *blockingConn) SetPingHandler(func(string) error)         {}
func (c *blockingConn) Subprotocol() string                       { return "" }

// deadlineConn is a WebsocketConnection that supports read and write
// deadlines. ReadMessage returns a HELLO message every dataEvery, or blocks if
// dataEvery is zero, and fails once the read deadline passes. WriteMessage
// succeeds immediately unless stuckWrites is set, in which case it blocks
// until the write deadline passes.
type deadlineConn struct {
	dataEvery   time.Duration
	stuckWrites bool
	closeC      chan struct{}
	closeOnce   sync.Once

	mu            sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
	pongHandler   func(string) error
	pingHandler   func(string) error

	// State seen by the first call to ReadMessage.
	firstRead         sync.Once
	deadlineAtFirstRd bool
	handlersAtFirstRd bool
	writeTimedOut     atomic.Bool
	readTimedOut      atomic.Bool
}

func newDeadlineConn() *deadlineConn {
	return &deadlineConn{closeC: make(chan struct{})}
}

// wait waits for d or until the deadline passes or the connection closes.
func (c *deadlineConn) wait(d time.Duration, deadline time.Time) error {
	var dl <-chan time.Time
	if !deadline.IsZero() {
		dl = time.After(time.Until(deadline))
	}
	var tick <-chan time.Time
	if d != 0 {
		tick = time.After(d)
	}
	select {
	case <-tick:
		return nil
	case <-dl:
		return errTimeout
	case <-c.closeC:
		return errClosed
	}
}

func (c *deadlineConn) ReadMessage() (int, []byte, error) {
	c.mu.Lock()
	c.firstRead.Do(func() {
		c.deadlineAtFirstRd = !c.readDeadline.IsZero()
		c.handlersAtFirstRd = c.pingHandler != nil && c.pongHandler != nil
	})
	deadline := c.readDeadline
	c.mu.Unlock()

	if err := c.wait(c.dataEvery, deadline); err != nil {
		if errors.Is(err, errTimeout) {
			c.readTimedOut.Store(true)
		}
		return 0, nil, err
	}
	return websocket.TextMessage, helloJSON, nil
}

func (c *deadlineConn) WriteMessage(int, []byte) error {
	if !c.stuckWrites {
		return nil
	}
	c.mu.Lock()
	deadline := c.writeDeadline
	c.mu.Unlock()
	err := c.wait(0, deadline)
	if errors.Is(err, errTimeout) {
		c.writeTimedOut.Store(true)
	}
	return err
}

func (c *deadlineConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	c.mu.Unlock()
	return nil
}

func (c *deadlineConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.writeDeadline = t
	c.mu.Unlock()
	return nil
}

func (c *deadlineConn) SetPongHandler(h func(string) error) {
	c.mu.Lock()
	c.pongHandler = h
	c.mu.Unlock()
}

func (c *deadlineConn) SetPingHandler(h func(string) error) {
	c.mu.Lock()
	c.pingHandler = h
	c.mu.Unlock()
}

func (c *deadlineConn) Close() error {
	c.closeOnce.Do(func() { close(c.closeC) })
	return nil
}

func (c *deadlineConn) WriteControl(int, []byte, time.Time) error { return nil }
func (c *deadlineConn) Subprotocol() string                       { return "" }

func newTestPeer(conn transport.WebsocketConnection, keepAlive time.Duration) wamp.Peer {
	return transport.NewWebsocketPeer(conn, &serialize.JSONSerializer{},
		websocket.TextMessage, log.Default(), keepAlive, 0)
}

// requireCloses calls peer.Close and fails if it does not return within
// testTimeout.
func requireCloses(t *testing.T, peer wamp.Peer) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		peer.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		require.FailNow(t, "peer.Close() deadlocked")
	}
}

// requireRecvClosed fails if the peer's Recv channel is not closed within
// testTimeout, meaning that recvHandler did not exit.
func requireRecvClosed(t *testing.T, peer wamp.Peer) {
	t.Helper()
	timeout := time.After(testTimeout)
	for {
		select {
		case _, ok := <-peer.Recv():
			if !ok {
				return
			}
		case <-timeout:
			require.FailNow(t, "recvHandler did not exit")
		}
	}
}

// TestWSRecvErrorUnblocksStuckWrite pins gammazero/nexus#242. After a read
// error, recvHandler waits for sendHandler to exit. If sendHandler is stuck in
// WriteMessage, because the remote peer stopped reading, recvHandler must
// close the connection to unblock it instead of waiting forever.
func TestWSRecvErrorUnblocksStuckWrite(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	defer transport.SetSenderStopTimeout(shortStopDelay)()

	conn := newBlockingConn()
	peer := newTestPeer(conn, 0)
	peer.Send() <- &wamp.Hello{Realm: "test"}
	<-conn.writeEntered

	close(conn.readErr)
	requireRecvClosed(t, peer)
	requireCloses(t, peer)
	require.Equal(t, int32(1), conn.abortedWrites.Load())
}

// TestWSCloseUnblocksStuckWrite verifies that Close does not wait forever for
// a sendHandler that is stuck in WriteMessage.
func TestWSCloseUnblocksStuckWrite(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	defer transport.SetSenderStopTimeout(shortStopDelay)()

	conn := newBlockingConn()
	peer := newTestPeer(conn, 0)
	peer.Send() <- &wamp.Hello{Realm: "test"}
	<-conn.writeEntered

	requireCloses(t, peer)
	require.Equal(t, int32(1), conn.abortedWrites.Load())
}

// TestWSRecvErrorLetsInFlightWriteFinish verifies that a read error does not
// cut off a write that completes within the sender stop timeout.
func TestWSRecvErrorLetsInFlightWriteFinish(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	defer transport.SetSenderStopTimeout(2 * time.Second)()

	conn := newBlockingConn()
	peer := newTestPeer(conn, 0)
	peer.Send() <- &wamp.Hello{Realm: "test"}
	<-conn.writeEntered

	close(conn.readErr)
	time.Sleep(50 * time.Millisecond)
	require.False(t, conn.isClosed(), "connection closed while write in flight")
	close(conn.writeRelease)

	requireRecvClosed(t, peer)
	requireCloses(t, peer)
	require.Equal(t, int32(1), conn.completedWrites.Load())
	require.Zero(t, conn.abortedWrites.Load())
}

// TestWSRecvHandlerExitsOnSilentReadDeadline pins the second half of
// gammazero/nexus#242: with KeepAlive configured, recvHandler must not block
// forever in ReadMessage when the remote peer has gone silent.
func TestWSRecvHandlerExitsOnSilentReadDeadline(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	conn := newDeadlineConn()
	peer := newTestPeer(conn, testKeepAlive)

	requireRecvClosed(t, peer)
	require.True(t, conn.readTimedOut.Load(), "read should have timed out")
	requireCloses(t, peer)
}

// TestWSDataKeepsConnectionAlive verifies that receiving data counts as a sign
// of life, so a remote peer that keeps sending data is not disconnected when
// its pongs are delayed.
func TestWSDataKeepsConnectionAlive(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	conn := newDeadlineConn()
	conn.dataEvery = testKeepAlive / 4
	peer := newTestPeer(conn, testKeepAlive)

	// Receive data for well past both the 2*keepAlive read deadline and the
	// two missed pongs that close the connection.
	timeout := time.After(6 * testKeepAlive)
recvLoop:
	for {
		select {
		case _, ok := <-peer.Recv():
			require.True(t, ok, "connection closed while receiving data")
		case <-timeout:
			break recvLoop
		}
	}
	requireCloses(t, peer)
}

// TestWSWriteDeadline verifies that with KeepAlive configured, a write that
// cannot complete times out instead of blocking forever.
func TestWSWriteDeadline(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	conn := newDeadlineConn()
	conn.stuckWrites = true
	peer := newTestPeer(conn, testKeepAlive)
	peer.Send() <- &wamp.Hello{Realm: "test"}

	require.Eventually(t, conn.writeTimedOut.Load, testTimeout,
		10*time.Millisecond, "write should have timed out")
	requireCloses(t, peer)
}

// TestWSReadSetupBeforeFirstRead verifies that the read deadline and the
// ping and pong handlers are set before recvHandler starts reading, since
// gorilla does not allow setting them concurrently with ReadMessage.
func TestWSReadSetupBeforeFirstRead(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	for range 20 {
		conn := newDeadlineConn()
		conn.dataEvery = time.Millisecond
		peer := newTestPeer(conn, testKeepAlive)
		<-peer.Recv()
		requireCloses(t, peer)

		require.True(t, conn.deadlineAtFirstRd, "read deadline not set")
		require.True(t, conn.handlersAtFirstRd, "ping/pong handlers not set")
	}
}
