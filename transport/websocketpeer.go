package transport

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
	"github.com/gorilla/websocket"
)

type WebsocketConfig struct {
	// Enable per message write compression.
	// When configuring server: allows compression, used if clients request
	// When configuring client: requests compression, used if server allows
	EnableCompression     bool `json:"enable_compression"`
	EnableContextTakeover bool `json:"enable_context_takeover"`
	CompressionLevel      int  `json:"compression_level"`

	// For WebsocketServer configuration only.  These options are configured
	// for the router by passing WebsocketConfig to
	// WebsocketServer.SetConfig().
	//
	// EnableTrackingCookie tells the server to send a random-value cookie to
	// the websocket client.  A returning client may identify itself by sending
	// a previously issued tracking cookie in a websocket request.  If a
	// request header received by the server contains the tracking cookie, then
	// the cookie is included in the HELLO and session details.  The new
	// tracking cookie that gets sent to the client (the cookie to expect for
	// subsequent connections) is also stored in HELLO and session details.
	//
	// The cookie from the request, and the next cookie to expect, are
	// stored in the HELLO and session details, respectively, as:
	//
	//     Details.transport.auth.cookie|*http.Cookie
	//     Details.transport.auth.nextcookie|*http.Cookie
	//
	// This information is available to auth/authz logic, and can be retrieved
	// from details as follows:
	//
	//     req *http.Request
	//     path := []string{"transport", "auth", "request"}
	//     v, err := wamp.DictValue(details, path)
	//     if err == nil {
	//         req = v.(*http.Request)
	//     }
	//
	// The "cookie" and "nextcookie" values are retrieved similarly.
	//
	// EnableRequestCapture tells the server to include the upgrade HTTP
	// request in the HELLO and session details.  It is stored in
	// Details.transport.auth.request|*http.Request and is available to
	// auth/authz logic.
	EnableTrackingCookie bool `json:"enable_tracking_cookie"`
	EnableRequestCapture bool `json:"enable_request_capture"`

	// For websocket client configuration only
	//
	// If provided when configuring websocket client, cookies from server are
	// put in here.  This allows cookies to be stored and then sent back to the
	// server in subsequent websocket connections.  Cookies may be used to
	// identify returning clients, and can be used to authenticate clients.
	Jar http.CookieJar
}

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

	writerDone chan struct{}

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

// ConnectWebsocketPeer creates a new websocketPeer with the specified config,
// and connects it to the websocket server at the specified URL.
func ConnectWebsocketPeer(url string, serialization serialize.Serialization, tlsConfig *tls.Config, dial DialFunc, logger stdlog.StdLog, wsCfg *WebsocketConfig) (wamp.Peer, error) {
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
	if wsCfg != nil {
		dialer.EnableCompression = wsCfg.EnableCompression
		//dialer.EnableContextTakeover = wsCfg.EnableContextTakeover
		//dialer.CompressionLevel = wsCfg.CompressionLevel

		dialer.Jar = wsCfg.Jar
	}

	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	return NewWebsocketPeer(conn, serializer, payloadType, logger), nil
}

// NewWebsocketPeer creates a websocket peer from an existing websocket
// connection.  This is used by clients connecting to the WAMP router, and by
// servers to handle connections from clients.
func NewWebsocketPeer(conn *websocket.Conn, serializer serialize.Serializer, payloadType int, logger stdlog.StdLog) wamp.Peer {
	w := &websocketPeer{
		conn:        conn,
		serializer:  serializer,
		payloadType: payloadType,
		closed:      make(chan struct{}),
		writerDone:  make(chan struct{}),

		// The router will read from this channel and immediately dispatch the
		// message to the broker or dealer.  Therefore this channel can be
		// unbuffered.
		rd: make(chan wamp.Message),

		// The channel for messages being written to the websocket should be
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

func (w *websocketPeer) TrySend(msg wamp.Message) error {
	select {
	case w.wr <- msg:
	default:
		return errors.New("blocked")
	}
	return nil
}

func (w *websocketPeer) Send(msg wamp.Message) error {
	w.wr <- msg
	return nil
}

// Close closes the websocket peer.  This closes the local send channel, and
// sends a close control message to the websocket to tell the other side to
// close.
//
// *** Do not call Send after calling Close. ***
func (w *websocketPeer) Close() {
	// Tell sendHandler to exit, allowing it to finish sending any queued
	// messages.  Do not close wr channel in case there are incoming messages
	// during close.
	w.wr <- nil
	<-w.writerDone

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
	defer close(w.writerDone)
	for msg := range w.wr {
		if msg == nil {
			return
		}
		b, err := w.serializer.Serialize(msg.(wamp.Message))
		if err != nil {
			w.log.Print(err)
		}

		if err = w.conn.WriteMessage(w.payloadType, b); err != nil {
			if !wamp.IsGoodbyeAck(msg) {
				w.log.Print(err)
			}
		}
	}
}

// recvHandler pulls messages from the websocket and pushes them to the read
// channel.
func (w *websocketPeer) recvHandler() {
	// When done, close read channel to cause router to remove session if not
	// already removed.
	defer close(w.rd)
	defer w.conn.Close()
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
				<-w.writerDone
			}
			// The error is only one of these errors.  It is generally not
			// helpful to log this, so keeping this commented out.
			// websocket: close sent
			// websocket: close 1000 (normal): goodbye
			// read tcp addr:port->addr:port: use of closed network connection
			//w.log.Print(err)
			return
		}

		if msgType == websocket.CloseMessage {
			return
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
		//
		// Need to wake up on w.closed so this goroutine can exit in the case
		// that messages are not being read from the peer and prevent this
		// write from completing.
		select {
		case w.rd <- msg:
		case <-w.closed:
			// If closed, try for one second to send the last message and then
			// exit recvHandler.
			select {
			case w.rd <- msg:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
