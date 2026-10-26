package transport_test

import (
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

func TestAcceptsSharedMessage(t *testing.T) {
	ws := newTestPeer(newRecordingConn(), 0)
	defer ws.Close()
	require.True(t, transport.AcceptsSharedMessage(ws))

	_, raw := newPipedRawSocket(t, 0)
	require.True(t, transport.AcceptsSharedMessage(raw))

	cli, rtr := transport.LinkedPeers()
	defer cli.Close()
	require.False(t, transport.AcceptsSharedMessage(rtr))
}

// TestWebsocketWritesSharedMessage checks that a websocket peer writes a
// SharedMessage as the message it wraps.
func TestWebsocketWritesSharedMessage(t *testing.T) {
	ev := &wamp.Event{Subscription: 1, Publication: 2, Details: wamp.Dict{}, Arguments: wamp.List{"x"}}
	want, err := (&serialize.JSONSerializer{}).Serialize(ev)
	require.NoError(t, err)

	conn := newRecordingConn()
	peer := newTestPeer(conn, 0)
	peer.Send() <- serialize.NewSharedMessage(ev)
	require.Eventually(t, func() bool {
		return len(conn.written(websocket.TextMessage)) == 1
	}, testTimeout, time.Millisecond)
	require.Equal(t, want, conn.written(websocket.TextMessage)[0])
	requireCloses(t, peer)
}

// TestRawSocketWritesSharedMessage checks the same for a rawsocket peer.
func TestRawSocketWritesSharedMessage(t *testing.T) {
	verifyNoLeaks(t)
	client, peer := newPipedRawSocket(t, 0)
	ev := &wamp.Event{Subscription: 1, Publication: 2, Details: wamp.Dict{}, Arguments: wamp.List{"x"}}
	want, err := (&serialize.JSONSerializer{}).Serialize(ev)
	require.NoError(t, err)

	peer.Send() <- serialize.NewSharedMessage(ev)
	frameType, body, err := readFrame(client)
	require.NoError(t, err)
	require.Equal(t, byte(frameMessage), frameType)
	require.Equal(t, want, body)
}

// countWritesConn is a WebsocketConnection that discards what is written to
// it, and counts each message written on done.
type countWritesConn struct {
	done   *sync.WaitGroup
	closed chan struct{}
	once   sync.Once
}

func (c *countWritesConn) WriteMessage(int, []byte) error {
	c.done.Done()
	return nil
}

func (c *countWritesConn) ReadMessage() (int, []byte, error) {
	<-c.closed
	return 0, nil, io.EOF
}

func (c *countWritesConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *countWritesConn) WriteControl(int, []byte, time.Time) error { return nil }
func (c *countWritesConn) SetPongHandler(func(string) error)         {}
func (c *countWritesConn) SetPingHandler(func(string) error)         {}
func (c *countWritesConn) Subprotocol() string                       { return "" }

// BenchmarkFanout measures sending one EVENT to 100 websocket peers, as the
// broker does for the subscribers of a topic, either as the same EVENT, which
// each peer serializes, or as a SharedMessage, which is serialized once. The
// connections discard what is written, so the benchmark measures the work of
// the peers without the cost of writing to a network.
func BenchmarkFanout(b *testing.B) {
	const peers = 100
	serializers := []struct {
		name        string
		s           serialize.Serializer
		payloadType int
	}{
		{"json", &serialize.JSONSerializer{}, websocket.TextMessage},
		{"msgpack", &serialize.MessagePackSerializer{}, websocket.BinaryMessage},
		{"cbor", &serialize.CBORSerializer{}, websocket.BinaryMessage},
	}
	for _, size := range []int{100, 1 << 10, 16 << 10} {
		ev := &wamp.Event{
			Subscription: wamp.GlobalID(),
			Publication:  wamp.GlobalID(),
			Details:      wamp.Dict{"topic": "com.example.sensor.temperature"},
			Arguments:    wamp.List{strings.Repeat("x", size), 42, true},
			ArgumentsKw:  wamp.Dict{"unit": "C", "seq": 12345},
		}
		for _, ser := range serializers {
			for _, shared := range []bool{false, true} {
				mode := "per-peer"
				if shared {
					mode = "shared"
				}
				name := fmt.Sprintf("%dB/%s/%s", size, ser.name, mode)
				b.Run(name, func(b *testing.B) {
					var done sync.WaitGroup
					ps := make([]wamp.Peer, peers)
					for i := range ps {
						conn := &countWritesConn{done: &done, closed: make(chan struct{})}
						ps[i] = transport.NewWebsocketPeer(conn, ser.s, ser.payloadType,
							log.New(io.Discard, "", 0), 0, 16)
					}
					defer func() {
						for _, p := range ps {
							p.Close()
						}
					}()
					b.ReportAllocs()
					for b.Loop() {
						var msg wamp.Message = ev
						if shared {
							msg = serialize.NewSharedMessage(ev)
						}
						done.Add(peers)
						for _, p := range ps {
							p.Send() <- msg
						}
						done.Wait()
					}
				})
			}
		}
	}
}
