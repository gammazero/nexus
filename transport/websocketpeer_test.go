package transport_test

import (
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

// pingCountingConn is a mock WebsocketConnection that counts ping
// messages written to it, blocks ReadMessage until conn.Close is
// called, and never invokes the pong handler — simulating a remote
// peer that has gone silent.
type pingCountingConn struct {
	pings     atomic.Int32
	closed    chan struct{}
	closeOnce sync.Once
}

func newPingCountingConn() *pingCountingConn {
	return &pingCountingConn{closed: make(chan struct{})}
}

func (m *pingCountingConn) Close() error {
	m.closeOnce.Do(func() { close(m.closed) })
	return nil
}

func (m *pingCountingConn) WriteControl(int, []byte, time.Time) error { return nil }

func (m *pingCountingConn) WriteMessage(messageType int, _ []byte) error {
	if messageType == websocket.PingMessage {
		m.pings.Add(1)
	}
	return nil
}

func (m *pingCountingConn) ReadMessage() (int, []byte, error) {
	<-m.closed
	return 0, nil, fmt.Errorf("conn closed")
}

func (m *pingCountingConn) SetPongHandler(func(string) error) {}
func (m *pingCountingConn) SetPingHandler(func(string) error) {}
func (m *pingCountingConn) Subprotocol() string               { return "" }

// TestWebsocketC10_KeepAliveMissedPongs verifies the websocketPeer
// detects a silent peer (no pongs in response to keepalive pings)
// and closes the connection. The current implementation considers
// the peer dead after pendingPongs reaches 2 (i.e. 2 unanswered
// pings). After detection, sendHandlerKeepAlive calls conn.Close()
// directly, which causes recvHandler's blocked ReadMessage to
// return; recvHandler then defer-closes Recv. Close-on-the-peer
// must remain idempotent (sync.Once).
func TestWebsocketC10_KeepAliveMissedPongs(t *testing.T) {
	mock := newPingCountingConn()
	const ka = 50 * time.Millisecond

	peer := transport.NewWebsocketPeer(mock, &serialize.JSONSerializer{},
		websocket.TextMessage, log.New(log.Writer(), "", 0), ka, 0)
	t.Cleanup(func() { peer.Close() })

	// After ~3 intervals, the keepalive ticker will have sent 2
	// pings, observed pendingPongs >= 2 on the third tick, and
	// closed the conn.
	select {
	case <-mock.closed:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "conn was not closed despite missed pongs")
	}

	require.GreaterOrEqual(t, int(mock.pings.Load()), 2,
		"expected at least 2 pings before close, got %d", mock.pings.Load())

	// recvHandler should observe the closed conn (its ReadMessage
	// returns) and defer-close peer.Recv(). Wait for that signal.
	select {
	case msg, ok := <-peer.Recv():
		require.False(t, ok, "expected closed Recv, got msg %v", msg)
	case <-time.After(time.Second):
		require.FailNow(t, "peer.Recv() did not close after keepalive timeout")
	}

	// Close must remain idempotent (sync.Once).
	require.NotPanics(t, func() { peer.Close() }, "first peer.Close after keepalive close")
	require.NotPanics(t, func() { peer.Close() }, "second peer.Close after keepalive close")
}
