package transport

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"time"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
	"github.com/gorilla/websocket"
)

// websocketPeer implements the Peer interface, connecting the Send and Recv
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
	recvDone     chan struct{}

	log stdlog.StdLog
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
// A positive queueSize value specifies the maximum number of messages that can
// be queued waiting to be written to the websocker.  Once the queue has
// reached this limit, the WAMP router will drop messages in order to not
// block.  A value of < 1 uses the default size.
func ConnectWebsocketPeer(url string, serialization serialize.Serialization, tlsConfig *tls.Config, dial DialFunc, logger stdlog.StdLog) (wamp.Peer, error) {
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
// connection.  This is used for handling clients connecting to the WAMP
// router.
func NewWebsocketPeer(conn *websocket.Conn, serializer serialize.Serializer, payloadType int, logger stdlog.StdLog) wamp.Peer {
	w := &websocketPeer{
		conn:         conn,
		serializer:   serializer,
		payloadType:  payloadType,
		closed:       make(chan struct{}),
		wsWriterDone: make(chan struct{}),
		recvDone:     make(chan struct{}),

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

func (w *websocketPeer) Send(msg wamp.Message) error {
	select {
	case w.wr <- msg:
	default:
		err := fmt.Errorf("client blocked - dropped %s", msg.MessageType())
		w.log.Println("!!!", err)
		return err
	}
	return nil
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
	select {
	case <-w.recvDone:
	case <-time.After(time.Second):
		// Timed out waiting for recvHandler to finish, so it may be waiting to
		// write w.rd and nothing is reading from it - need to close the
		// channel to free it.  So, install recover func in case receiveHandler
		// does close w.rd first, and close w.rd.
		defer func() {
			recover()
		}()
		close(w.rd)
	}
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
	defer func() {
		if r := recover(); r != nil {
			// Only panic if not a runtime error, as caused by channel closed.
			if r, ok := r.(runtime.Error); !ok {
				// Not runtime error, so maybe important panic from websocket.
				panic(r)
			}
		}
		// Close websocket connection.
		w.conn.Close()
	}()
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
			// The error is only one of these erors.  It is generally not
			// helpful to log this, so keeping this commented out.
			// websocket: close sent
			// websocket: close 1000 (normal): goodbye
			// read tcp addr:port->addr:port: use of closed network connection
			//w.log.Print(err)
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
	close(w.recvDone)
}
