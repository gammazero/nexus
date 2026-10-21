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

// stuckWriteMockConn simulates a real-world scenario from gammazero/nexus#242:
// the websocket connection's read side is broken (peer/network gone) so
// ReadMessage returns an error, but the write side is still half-buffered —
// e.g. peer FIN'd one direction but its kernel-side receive buffer is
// still draining slowly, or middleware is silently sinking writes. In
// that state WriteMessage can block for an arbitrary time.
//
// recvHandler's spontaneous-error branch calls w.cancelSender() and then
// waits at <-w.writerDone, but the sendHandler can't react to the
// cancellation while WriteMessage is in progress. Without the fix in this
// commit, recvHandler is stuck waiting on writerDone forever, and the
// deferred w.conn.Close() (which would unstick WriteMessage) never runs
// because the function can't return. Both goroutines leak.
type stuckWriteMockConn struct {
	mu         sync.Mutex
	readBlock  chan struct{} // close to make ReadMessage return an error
	writeBlock chan struct{} // close to make WriteMessage return
	closed     atomic.Bool
}

func newStuckWriteMockConn() *stuckWriteMockConn {
	return &stuckWriteMockConn{
		readBlock:  make(chan struct{}),
		writeBlock: make(chan struct{}),
	}
}

func (m *stuckWriteMockConn) ReadMessage() (int, []byte, error) {
	<-m.readBlock
	return 0, nil, errors.New("simulated read error")
}

func (m *stuckWriteMockConn) WriteMessage(int, []byte) error {
	<-m.writeBlock
	if m.closed.Load() {
		return errors.New("write on closed conn")
	}
	return nil
}

func (m *stuckWriteMockConn) Close() error {
	if m.closed.Swap(true) {
		return nil
	}
	// Closing the underlying conn must unblock anything stuck in
	// WriteMessage or ReadMessage — same contract as gorilla.Conn.
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-m.readBlock:
	default:
		close(m.readBlock)
	}
	select {
	case <-m.writeBlock:
	default:
		close(m.writeBlock)
	}
	return nil
}

func (m *stuckWriteMockConn) WriteControl(int, []byte, time.Time) error { return nil }
func (m *stuckWriteMockConn) SetPongHandler(func(string) error)         {}
func (m *stuckWriteMockConn) SetPingHandler(func(string) error)         {}
func (m *stuckWriteMockConn) Subprotocol() string                       { return "" }

// silentMockConn simulates a websocket whose underlying network has
// gone silent in BOTH directions — no data, no FIN, no RST, no
// kernel-side write completion. ReadMessage blocks until the read
// deadline expires or the connection is closed. WriteMessage also
// blocks indefinitely (until close), modelling a kernel send buffer
// that has filled because the peer is no longer ACKing TCP segments.
//
// This is the leak vector from gammazero/nexus#242 that the existing
// keepalive logic does NOT catch: the sender's ticker tries to send a
// PING, WriteMessage hangs, the missed-pong counter never advances,
// and recvHandler has no read deadline to bail it out either. Both
// goroutines leak forever. The fix sets a read deadline (refreshed by
// incoming ping/pong) so the receiver eventually times out and tears
// the conn down, which then unsticks the sender.
type silentMockConn struct {
	mu       sync.Mutex
	deadline time.Time
	closeC   chan struct{}
}

func newSilentMockConn() *silentMockConn { return &silentMockConn{closeC: make(chan struct{})} }

func (m *silentMockConn) ReadMessage() (int, []byte, error) {
	for {
		m.mu.Lock()
		dl := m.deadline
		m.mu.Unlock()
		if !dl.IsZero() {
			d := time.Until(dl)
			if d <= 0 {
				return 0, nil, errors.New("i/o timeout")
			}
			select {
			case <-m.closeC:
				return 0, nil, errors.New("use of closed network connection")
			case <-time.After(d):
				return 0, nil, errors.New("i/o timeout")
			}
		}
		// Deadline not yet set — poll briefly.
		select {
		case <-m.closeC:
			return 0, nil, errors.New("use of closed network connection")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (m *silentMockConn) WriteMessage(int, []byte) error {
	<-m.closeC
	return errors.New("write on closed conn")
}

func (m *silentMockConn) SetReadDeadline(t time.Time) error {
	m.mu.Lock()
	m.deadline = t
	m.mu.Unlock()
	return nil
}
func (m *silentMockConn) SetWriteDeadline(time.Time) error          { return nil }
func (m *silentMockConn) WriteControl(int, []byte, time.Time) error { return nil }
func (m *silentMockConn) SetPongHandler(func(string) error)         {}
func (m *silentMockConn) SetPingHandler(func(string) error)         {}
func (m *silentMockConn) Subprotocol() string                       { return "" }
func (m *silentMockConn) Close() error {
	select {
	case <-m.closeC:
	default:
		close(m.closeC)
	}
	return nil
}

// TestWSRecvHandlerExitsOnSilentReadDeadline pins the second half of
// gammazero/nexus#242: with KeepAlive configured, recvHandler must NOT
// block forever in ReadMessage when the peer has gone silent. The fix
// sets a read deadline of 2× KeepAlive on the underlying conn (when it
// supports SetReadDeadline) so an unresponsive peer triggers an i/o
// timeout and recvHandler exits cleanly.
func TestWSRecvHandlerExitsOnSilentReadDeadline(t *testing.T) {
	mock := newSilentMockConn()
	const keepAlive = 100 * time.Millisecond
	peer := transport.NewWebsocketPeer(mock, &serialize.JSONSerializer{}, websocket.TextMessage, log.Default(), keepAlive, 0)
	defer peer.Close()

	// recvHandler closes the Recv channel via deferred close(w.rd) when it
	// exits. With the fix this happens within ~2*keepAlive (initial deadline)
	// + slack. Without the fix recvHandler stays blocked in ReadMessage.
	select {
	case _, ok := <-peer.Recv():
		require.False(t, ok, "expected closed Recv channel; recvHandler should have exited")
	case <-time.After(2 * time.Second):
		t.Fatal("recvHandler did not exit despite silent peer + read-deadline support")
	}
}

// TestWSRecvHandlerCleanupDoesNotDeadlock pins gammazero/nexus#242. With
// the fix, after a spontaneous read error recvHandler must close the
// connection before waiting for the writer, so a sendHandler stuck in
// WriteMessage gets unblocked and writerDone fires. Without the fix the
// peer's recvHandler hangs at <-w.writerDone forever.
func TestWSRecvHandlerCleanupDoesNotDeadlock(t *testing.T) {
	mock := newStuckWriteMockConn()
	peer := transport.NewWebsocketPeer(mock, &serialize.JSONSerializer{}, websocket.TextMessage, log.Default(), 0, 0)

	// Park sendHandler in WriteMessage by queueing a message that the mock
	// will accept onto wr but block when sendHandler tries to write it.
	peer.Send() <- &wamp.Hello{Realm: "test"}

	// Give sendHandler a moment to actually pick the message off wr and
	// enter WriteMessage where it'll block on writeBlock.
	time.Sleep(50 * time.Millisecond)

	// Spontaneous-error branch: ReadMessage returns an error, recvHandler
	// goes into cleanup. With the fix it closes the conn, which unblocks
	// the stuck WriteMessage. Without the fix it hangs.
	close(mock.readBlock)

	// Wait for the peer to fully wind down. peer.Close() also waits for
	// recvDone internally; if recvHandler deadlocked, this never returns
	// and the test deadline trips.
	done := make(chan struct{})
	go func() {
		peer.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "peer.Close() deadlocked — recvHandler is stuck waiting on writerDone")
	}
}
