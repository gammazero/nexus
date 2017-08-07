package transport

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
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

	// Used to signal the websocket is closed explicitly.
	closed chan struct{}

	// Channels communicate with router.
	rd chan wamp.Message
	wr chan wamp.Message

	wsWriterDone chan struct{}

	log logger.Logger
}

const (
	// WAMP uses the following WebSocket subprotocol identifiers for unbatched
	// modes:
	jsonWebsocketProtocol    = "wamp.2.json"
	msgpackWebsocketProtocol = "wamp.2.msgpack"

	outQueueSize = 16
	ctrlTimeout  = 5 * time.Second
)

type DialFunc func(network, addr string) (net.Conn, error)

// ConnectWebsockerPeer creates a new websockerPeer with the specified config,
// and connects it to the websocket server at the specified URL.
//
// queueSize is the maximum number of messages that can be queue to be written
// to the websocker.  Once the queue has reached this limit, the WAMP router
// will drop messages in order to not block.  A value of < 1 uses the default
// size.
func ConnectWebsocketPeer(url string, serialization serialize.Serialization, tlsConfig *tls.Config, dial DialFunc, logger logger.Logger) (wamp.Peer, error) {
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
	return NewWebsocketPeer(conn, serializer, payloadType, logger), nil
}

// NewWebsockerPeer creates a websocket peer from an existing websocket
// connection.  This is used for for hanndling clients connecting to the WAMP
// service.
func NewWebsocketPeer(conn *websocket.Conn, serializer serialize.Serializer, payloadType int, logger logger.Logger) wamp.Peer {
	w := &websocketPeer{
		conn:         conn,
		serializer:   serializer,
		payloadType:  payloadType,
		closed:       make(chan struct{}),
		wsWriterDone: make(chan struct{}),

		// The router will read from this channen and immediately dispatch the
		// message to the broker or dealer.  Therefore this channel can be
		// unbuffered.
		rd: make(chan wamp.Message),

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

// Close closes the websocker peer.  This closes the local send channel, and
// sends a close control message to the websocket to tell the other side to
// close.
//
// *** Do not call Send after calling Close. ***
func (w *websocketPeer) Close() {
	// Tell sendHandler to exit, allowing it to finish sending any queued
	// messages.  Do not close wr channel in case there are incoming messages
	// during close.
	w.wr <- nil
	<-w.wsWriterDone

	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure,
		"goodbye")

	// Tell recvHandler to close.
	close(w.closed)

	// Ignore errors since websocket may have been closed by other side first
	// in response to a goodbye message.
	w.conn.WriteControl(websocket.CloseMessage, closeMsg,
		time.Now().Add(ctrlTimeout))
	w.conn.Close()
}

// sendHandler pulls messages from the write channel, and pushes them to the
// websocket.
func (w *websocketPeer) sendHandler() {
	defer close(w.wsWriterDone)
	for msg := range w.wr {
		if msg == nil {
			return
		}
		b, err := w.serializer.Serialize(msg.(wamp.Message))
		if err != nil {
			w.log.Print(err)
		}

		if err = w.conn.WriteMessage(w.payloadType, b); err != nil {
			w.log.Print(err)
		}
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
				// Peer was closed explicitly. sendHandler should have already
				// been told to exit.
			default:
				// Peer received control message to close.  Cause sendHandler
				// to exit without closing the write channel (in case writes
				// still happening) and allow it to finish sending any queued
				// messages.
				w.wr <- nil
				<-w.wsWriterDone

				// Close websocket connection.
				w.conn.Close()
			}
			if !strings.HasPrefix(err.Error(), "websocket: close 1000") {
				w.log.Print(err)
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
			w.log.Println("Cannot deserialize peer message:", err)
			continue
		}
		// It is OK for the router to block a client since routing should be
		// very quick compared to the time to transfer a message over
		// websocket, and a blocked client will not block other clients.
		w.rd <- msg
	}
	// Close read channel, cause router to remove session if not already.
	close(w.rd)
}
