package transport_test

import (
	"log"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// deadlineRecordingConn records every write deadline set on it. ReadMessage
// blocks until Close so the peer stays up for the duration of the test.
type deadlineRecordingConn struct {
	mu        sync.Mutex
	deadlines []time.Time
	closed    chan struct{}
	closeOnce sync.Once
}

func newDeadlineRecordingConn() *deadlineRecordingConn {
	return &deadlineRecordingConn{closed: make(chan struct{})}
}

func (m *deadlineRecordingConn) SetWriteDeadline(t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deadlines = append(m.deadlines, t)
	return nil
}

func (m *deadlineRecordingConn) recorded() []time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]time.Time(nil), m.deadlines...)
}

func (m *deadlineRecordingConn) Close() error {
	m.closeOnce.Do(func() { close(m.closed) })
	return nil
}
func (m *deadlineRecordingConn) WriteControl(int, []byte, time.Time) error { return nil }
func (m *deadlineRecordingConn) WriteMessage(int, []byte) error            { return nil }
func (m *deadlineRecordingConn) ReadMessage() (int, []byte, error) {
	<-m.closed
	return 0, nil, websocket.ErrCloseSent
}
func (m *deadlineRecordingConn) SetPongHandler(func(string) error) {}
func (m *deadlineRecordingConn) SetPingHandler(func(string) error) {}
func (m *deadlineRecordingConn) Subprotocol() string               { return "" }

// A data frame must go out under a write deadline, so a peer that stops
// draining its socket cannot park the writer forever.
func TestWSDataFrameWriteHasDeadline(t *testing.T) {
	conn := newDeadlineRecordingConn()
	peer := transport.NewWebsocketPeer(conn, &serialize.JSONSerializer{}, websocket.TextMessage, log.Default(), 0, 0)
	t.Cleanup(peer.Close)

	before := time.Now()
	peer.Send() <- &wamp.Hello{Realm: "realm1", Details: wamp.Dict{}}

	require.Eventually(t, func() bool { return len(conn.recorded()) > 0 },
		time.Second, 5*time.Millisecond, "no write deadline was set for the data frame")
	deadline := conn.recorded()[0]
	require.True(t, deadline.After(before.Add(time.Second)), "deadline %v is not in the future", deadline)
	require.True(t, deadline.Before(before.Add(10*time.Minute)), "deadline %v is unbounded", deadline)
}
