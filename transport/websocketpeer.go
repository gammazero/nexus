package transport

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gammazero/nexus/logger"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
	"github.com/gorilla/websocket"
)

// WebsocketPeer implements the Peer interface, connecting the Send and Recv
// methods to a websocket.
type websocketPeer struct {
	conn        *websocket.Conn
	serializer  serialize.Serializer
	payloadType int

	// Used to signal the websocket is closed.
	closed chan struct{}

	// Channels communicate with router.
	rd chan wamp.Message
	wr chan wamp.Message

	// Stop send handler without closing wr channel.
	stopSend chan struct{}

	log logger.Logger
}

const (
	// WAMP uses the following WebSocket subprotocol identifiers for unbatched
	// modes:
	jsonWebsocketProtocol    = "wamp.2.json"
	msgpackWebsocketProtocol = "wamp.2.msgpack"

	defaultOutQueueSize = 160
	ctrlTimeout         = 5 * time.Second
)

type DialFunc func(network, addr string) (net.Conn, error)

// ConnectWebsockerPeer creates a new websockerPeer with the specified config,
// and connects it to the websocket server at the specified URL.
//
// queueSize is the maximum number of messages that can be queue to be written
// to the websocker.  Once the queue has reached this limit, the WAMP router
// will drop messages in order to not block.  A value of < 1 uses the default
// size.
func ConnectWebsocketPeer(url string, serialization serialize.Serialization, tlsConfig *tls.Config, dial DialFunc, outQueueSize int, logger logger.Logger) (wamp.Peer, error) {
	var (
		protocol    string
		payloadType int
		serializer  serialize.Serializer
	)

	switch serialization {
	case serialize.JSON:
		protocol = jsonWebsocketProtocol
		payloadType = websocket.TextMessage
		serializer = &serialize.JSONSerializer{}
	case serialize.MSGPACK:
		protocol = msgpackWebsocketProtocol
		payloadType = websocket.BinaryMessage
		serializer = &serialize.MessagePackSerializer{}
	default:
		return nil, fmt.Errorf("unsupported serialization: %v", serialization)
	}

	dialer := websocket.Dialer{
		Subprotocols:    []string{protocol},
		TLSClientConfig: tlsConfig,
		Proxy:           http.ProxyFromEnvironment,
		NetDial:         dial,
	}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	return NewWebsocketPeer(conn, serializer, payloadType, outQueueSize, logger), nil
}

// NewWebsockerPeer creates a websocket peer from an existing websocket
// connection.  This is used for for hanndling clients connecting to the WAMP
// service.
func NewWebsocketPeer(conn *websocket.Conn, serializer serialize.Serializer, payloadType int, outQueueSize int, logger logger.Logger) wamp.Peer {
	if outQueueSize < 1 {
		outQueueSize = defaultOutQueueSize
	}
	w := &websocketPeer{
		conn:        conn,
		serializer:  serializer,
		payloadType: payloadType,
		closed:      make(chan struct{}),
		stopSend:    make(chan struct{}),

		// Messages read from the websocket can be handled immediately, since
		// they have traveled over the websocket and the read channel does not
		// need to be more than size 1.
		rd: make(chan wamp.Message, 1),

		// The channel for messages being written to the websocket sould be
		// large enough to prevent blocking while waiting for a slow websocket
		// to send messages.  For this reason it may be necessary for these
		// messages to be put into an outbound queue that can grow.
		wr: make(chan wamp.Message, outQueueSize),

		log: logger,
	}
	// Sending to and receiving from websocket is handled concurrently.
	go w.recvHandler()
	go w.sendHandler()

	return w
}

func (w *websocketPeer) Recv() <-chan wamp.Message { return w.rd }

func (w *websocketPeer) Send(msg wamp.Message) {
	select {
	case w.wr <- msg:
	default:
		w.log.Println("WARNING: client blocked router.  Dropped:",
			msg.MessageType())
	}
}

func (w *websocketPeer) Close() {
	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure,
		"goodbye")
	err := w.conn.WriteControl(websocket.CloseMessage, closeMsg,
		time.Now().Add(ctrlTimeout))
	if err != nil {
		w.log.Println("error sending close message:", err)
	}
	close(w.closed)
	if err = w.conn.Close(); err != nil {
		w.log.Println("error closing connection:", err)
	}
}

// sendHandler pulls messages from the write channel, and pushes them to the
// websocket.
func (w *websocketPeer) sendHandler() {
	select {
	case msg, open := <-w.wr:
		if !open {
			return
		}
		b, err := w.serializer.Serialize(msg.(wamp.Message))
		if err != nil {
			w.log.Println(err)
		}

		if err = w.conn.WriteMessage(w.payloadType, b); err != nil {
			w.log.Println(err)
		}
	case <-w.stopSend:
		return
	}

}

// recvHandler pulls messages from the websocket and pushes them to the read
// channel.
func (w *websocketPeer) recvHandler() {
	for {
		msgType, b, err := w.conn.ReadMessage()
		if err != nil {
			select {
			case <-w.closed:
				w.log.Println("peer connection closed")
			default:
				w.log.Println("error reading from peer:", err)
				w.conn.Close()
			}
			break
		}

		if msgType == websocket.CloseMessage {
			w.conn.Close()
			break
		}

		msg, err := w.serializer.Deserialize(b)
		if err != nil {
			// TODO: something more than merely logging?
			w.log.Println("error deserializing peer message:", err)
			continue
		}
		// It is OK for the router to block a client since routing should be
		// very quick compared to the time to transfer a message over
		// websocket, and a blocked client will not block other clients.
		w.rd <- msg
	}
	// Close read channel, cause router to remove session if not already.
	close(w.rd)
	// Stop sendHandler, without closing write channel.
	close(w.stopSend)
}
