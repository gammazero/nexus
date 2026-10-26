package transport

import (
	"context"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// These tests check that a message queued before the sender is told to stop
// is still written. Close stops the sender, and then discards whatever is left
// in the queue, so a message queued just before Close, such as the ABORT the
// router sends when a HELLO names no realm, used to be lost whenever the
// sender saw the stop before the message. That happens when the sender has not
// yet started, which these tests force by queuing the message and stopping the
// sender before starting it. With both ready, select picks either, so the
// tests repeat to catch the loss.

const sendStopRuns = 100

// TestRawSocketSenderWritesQueuedOnStop checks this for the rawsocket peer.
func TestRawSocketSenderWritesQueuedOnStop(t *testing.T) {
	for range sendStopRuns {
		client, server := net.Pipe()
		rs := &rawSocketPeer{
			conn:         server,
			serializer:   &serialize.JSONSerializer{},
			sendLimit:    1 << 16,
			writerDone:   make(chan struct{}),
			pongReady:    make(chan struct{}, 1),
			writeTimeout: time.Second,
			wr:           make(chan wamp.Message, 1),
			log:          log.New(io.Discard, "", 0),
		}
		rs.ctxSender, rs.cancelSender = context.WithCancel(context.Background())

		rs.wr <- &wamp.Abort{Reason: wamp.ErrNoSuchRealm, Details: wamp.Dict{}}
		rs.cancelSender()
		go rs.sendHandler()

		require.NoError(t, client.SetReadDeadline(time.Now().Add(time.Second)))
		var header [4]byte
		_, err := io.ReadFull(client, header[:])
		require.NoError(t, err, "queued message not written")
		body := make([]byte, int(header[1])<<16|int(header[2])<<8|int(header[3]))
		_, err = io.ReadFull(client, body)
		require.NoError(t, err)
		<-rs.writerDone
		_ = client.Close()
		_ = server.Close()
	}
}

// writesConn is a WebsocketConnection that records the messages written to
// it, and whose reads block until it is closed.
type writesConn struct {
	mu     sync.Mutex
	writes int
	closed chan struct{}
	once   sync.Once
}

func (c *writesConn) WriteMessage(int, []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes++
	return nil
}

func (c *writesConn) ReadMessage() (int, []byte, error) {
	<-c.closed
	return 0, nil, io.EOF
}

func (c *writesConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *writesConn) WriteControl(int, []byte, time.Time) error { return nil }
func (c *writesConn) SetPongHandler(func(string) error)         {}
func (c *writesConn) SetPingHandler(func(string) error)         {}
func (c *writesConn) Subprotocol() string                       { return "" }

// TestWebsocketSenderWritesQueuedOnStop checks this for both websocket
// senders, with and without keepalive.
func TestWebsocketSenderWritesQueuedOnStop(t *testing.T) {
	for _, keepAlive := range []bool{false, true} {
		for range sendStopRuns {
			conn := &writesConn{closed: make(chan struct{})}
			w := &websocketPeer{
				conn:         conn,
				serializer:   &serialize.JSONSerializer{},
				payloadType:  websocket.TextMessage,
				writerDone:   make(chan struct{}),
				wr:           make(chan wamp.Message, 1),
				pings:        make(chan string, 1),
				writeTimeout: time.Second,
				keepAlive:    time.Hour,
				log:          log.New(io.Discard, "", 0),
			}
			w.ctxSender, w.cancelSender = context.WithCancel(context.Background())

			w.wr <- &wamp.Abort{Reason: wamp.ErrNoSuchRealm, Details: wamp.Dict{}}
			w.cancelSender()
			if keepAlive {
				go w.sendHandlerKeepAlive()
			} else {
				go w.sendHandler()
			}
			<-w.writerDone

			conn.mu.Lock()
			writes := conn.writes
			conn.mu.Unlock()
			require.Equal(t, 1, writes, "queued message not written, keepalive %v", keepAlive)
		}
	}
}
