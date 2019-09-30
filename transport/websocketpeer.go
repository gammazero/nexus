package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gorilla/websocket"
)

// DialFunc is an alternate Dial function for the websocket dialer.
type DialFunc func(network, addr string) (net.Conn, error)

// WebsocketConfig is used to configure client websocket settings.
type WebsocketConfig struct {
	// Supplies alternate Dial function for the websocket dialer.
	// See https://godoc.org/github.com/gorilla/websocket#Dialer
	Dial DialFunc

	// Request per message write compression, if allowed by server.
	EnableCompression bool `json:"enable_compression"`

	// If provided when configuring websocket client, cookies from server are
	// put in here.  This allows cookies to be stored and then sent back to the
	// server in subsequent websocket connections.  Cookies may be used to
	// identify returning clients, and can be used to authenticate clients.
	Jar http.CookieJar

	// ProxyURL is an optional URL of the proxy to use for websocket requests.
	// If not defined, the proxy defined by the environment is used if defined.
	ProxyURL string
}

// WebsocketError is returned on failure to connect to a websocket, and
// contains the http response if one is available.
type WebsocketError struct {
	Err      error
	Response *http.Response
}

// Error returns a string describing the failure to connect to a websocket.
func (e *WebsocketError) Error() string {
	if e.Response == nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %s", e.Err, e.Response.Status)
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

	cancelSender context.CancelFunc
	ctxSender    context.Context

	writerDone chan struct{}

	log stdlog.StdLog
}

const (
	// WAMP uses the following websocket subprotocol identifiers for unbatched
	// modes:
	jsonWebsocketProtocol    = "wamp.2.json"
	msgpackWebsocketProtocol = "wamp.2.msgpack"
	cborWebsocketProtocol    = "wamp.2.cbor"

	ctrlTimeout = 5 * time.Second
)

// ConnectWebsocketPeer creates a new websocket client with the specified
// config, connects the client to the websocket server at the specified URL,
// and returns the connected websocket peer.
//
// The provided Context must be non-nil.  If the context expires before the
// connection is complete, an error is returned.  Once successfully connected,
// any expiration of the context will not affect the connection.
func ConnectWebsocketPeer(ctx context.Context, routerURL string, serialization serialize.Serialization, tlsConfig *tls.Config, logger stdlog.StdLog, wsCfg *WebsocketConfig) (wamp.Peer, error) {
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
	case serialize.CBOR:
		protocol = cborWebsocketProtocol
		payloadType = websocket.BinaryMessage
		serializer = &serialize.CBORSerializer{}
	default:
		return nil, fmt.Errorf("unsupported serialization: %v", serialization)
	}

	dialer := websocket.Dialer{
		Subprotocols:    []string{protocol},
		TLSClientConfig: tlsConfig,
		Proxy:           http.ProxyFromEnvironment,
	}

	if wsCfg != nil {
		dialer.NetDial = wsCfg.Dial
		if wsCfg.ProxyURL != "" {
			proxyURL, err := url.Parse(wsCfg.ProxyURL)
			if err != nil {
				return nil, err
			}
			dialer.Proxy = http.ProxyURL(proxyURL)
		}
		dialer.Jar = wsCfg.Jar
		dialer.EnableCompression = wsCfg.EnableCompression
	}

	conn, rsp, err := dialer.DialContext(ctx, routerURL, nil)
	if err != nil {
		return nil, &WebsocketError{
			Err:      err,
			Response: rsp,
		}
	}
	return NewWebsocketPeer(conn, serializer, payloadType, logger, 0, 0), nil
}

// NewWebsocketPeer creates a websocket peer from an existing websocket
// connection.  This is used by clients connecting to the WAMP router, and by
// servers to handle connections from clients.
//
// A non-zero keepAlive value configures a websocket "ping/pong" heartbeat,
// sending websocket "pings" every keepAlive interval.  If a "pong" response
// is not received after 2 intervals have elapsed then the websocket is closed.
func NewWebsocketPeer(conn *websocket.Conn, serializer serialize.Serializer, payloadType int, logger stdlog.StdLog, keepAlive time.Duration, outQueueSize int) wamp.Peer {
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
	w.ctxSender, w.cancelSender = context.WithCancel(context.Background())

	// Sending to and receiving from websocket is handled concurrently.
	go w.recvHandler()
	if keepAlive != 0 {
		if keepAlive < time.Second {
			w.log.Println("Warning: very short keepalive (< 1 second)")
		}
		go w.sendHandlerKeepAlive(keepAlive)
	} else {
		go w.sendHandler()
	}

	return w
}

func (w *websocketPeer) Recv() <-chan wamp.Message { return w.rd }

func (w *websocketPeer) TrySend(msg wamp.Message) error {
	return wamp.TrySend(w.wr, msg)
}

func (w *websocketPeer) SendCtx(ctx context.Context, msg wamp.Message) error {
	return wamp.SendCtx(ctx, w.wr, msg)
}

func (w *websocketPeer) Send(msg wamp.Message) error {
	return wamp.SendCtx(w.ctxSender, w.wr, msg)
}

// Close closes the websocket peer.  This closes the local send channel, and
// sends a close control message to the websocket to tell the other side to
// close.
//
// *** Do not call Send after calling Close. ***
func (w *websocketPeer) Close() {
	// Tell sendHandler to exit and discard any queued messages.  Do not close
	// wr channel in case there are incoming messages during close.
	w.cancelSender()
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
	defer w.cancelSender()

sendLoop:
	for {
		select {
		case msg := <-w.wr:
			b, err := w.serializer.Serialize(msg.(wamp.Message))
			if err != nil {
				w.log.Print(err)
				continue sendLoop
			}

			if err = w.conn.WriteMessage(w.payloadType, b); err != nil {
				if !wamp.IsGoodbyeAck(msg) {
					w.log.Print(err)
				}
				return
			}
		case <-w.ctxSender.Done():
			return
		}
	}
}

func (w *websocketPeer) sendHandlerKeepAlive(keepAlive time.Duration) {
	defer close(w.writerDone)
	defer w.cancelSender()

	var pendingPongs int32
	w.conn.SetPongHandler(func(msg string) error {
		// Any response resets counter.
		atomic.StoreInt32(&pendingPongs, 0)
		return nil
	})

	ticker := time.NewTicker(keepAlive)
	defer ticker.Stop()
	pingMsg := []byte("keepalive")

	senderDone := w.ctxSender.Done()
recvLoop:
	for {
		select {
		case msg := <-w.wr:
			b, err := w.serializer.Serialize(msg.(wamp.Message))
			if err != nil {
				w.log.Print(err)
				continue recvLoop
			}

			if err = w.conn.WriteMessage(w.payloadType, b); err != nil {
				if !wamp.IsGoodbyeAck(msg) {
					w.log.Print(err)
				}
				return
			}
		case <-ticker.C:
			// If missed 2 responses, close websocket.
			if atomic.LoadInt32(&pendingPongs) >= 2 {
				w.log.Print("peer not responging to pings, closing websocket")
				w.conn.Close()
				return
			}
			// Send websocket ping.
			err := w.conn.WriteMessage(websocket.PingMessage, pingMsg)
			if err != nil {
				return
			}
			atomic.AddInt32(&pendingPongs, 1)
		case <-senderDone:
			return
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
				// still happening) and discard any queued messages.
				w.cancelSender()
				// Wait for writer to exit before closing websocket.
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
