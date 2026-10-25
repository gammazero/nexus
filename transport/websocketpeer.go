package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// DialFunc is an alternate Dial function for the websocket dialer.
type DialFunc func(network, addr string) (net.Conn, error)

// WebsocketConfig is used to configure client websocket settings.
type WebsocketConfig struct {
	// Supplies alternate Dial function for the websocket dialer. See
	// https://godoc.org/github.com/gorilla/websocket#Dialer
	Dial DialFunc

	// Request per message write compression, if allowed by server.
	EnableCompression bool `json:"enable_compression"`

	// If provided when configuring websocket client, cookies from server are
	// put in here. This allows cookies to be stored and then sent back to the
	// server in subsequent websocket connections. Cookies may be used to
	// identify returning clients, and can be used to authenticate clients.
	Jar http.CookieJar

	// ProxyURL is an optional URL of the proxy to use for websocket requests.
	// If not defined, the proxy defined by the environment is used if defined.
	ProxyURL string

	// KeepAlive configures a websocket "ping/pong" heartbeat when set to a
	// non-zero value. KeepAlive is the interval between websocket "pings". If
	// a "pong" response is not received after 2 intervals have elapsed then
	// the websocket connection is closed.
	KeepAlive time.Duration
}

// WebsocketConnection is the interface that a websocket connection must implement.
type WebsocketConnection interface {
	Close() error
	WriteControl(messageType int, data []byte, deadline time.Time) error
	WriteMessage(messageType int, data []byte) error
	ReadMessage() (messageType int, p []byte, err error)
	SetPongHandler(h func(appData string) error)
	SetPingHandler(h func(appData string) error)
	Subprotocol() string
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

// Unwrap returns the error that caused the failure to connect.
func (e *WebsocketError) Unwrap() error { return e.Err }

// websocketPeer implements the Peer interface, connecting the Send and Recv
// methods to a websocket.
type websocketPeer struct {
	conn        WebsocketConnection
	serializer  serialize.Serializer
	payloadType int

	// keepAlive is the interval between pings. If non-zero, the connection is
	// closed when no message or pong arrives for 2*keepAlive.
	keepAlive time.Duration

	// writeTimeout is how long a write may take before it fails.
	writeTimeout time.Duration

	// Deadline support of conn, or nil if conn does not support deadlines.
	readDeadliner  readDeadliner
	writeDeadliner writeDeadliner

	// Pings from the remote peer, to be answered by the sender.
	pings chan string
	// Number of pings sent without any response from the remote peer.
	pendingPongs atomic.Int32

	// How long to wait for the sender to exit before closing conn.
	stopTimeout time.Duration

	// Used to signal the websocket is closed explicitly.
	closed chan struct{}

	// Channels communicate with router.
	rd chan wamp.Message
	wr chan wamp.Message

	cancelSender context.CancelFunc
	ctxSender    context.Context

	recvDone   chan struct{}
	writerDone chan struct{}
	closeOnce  sync.Once

	log stdlog.StdLog
}

// readDeadliner and writeDeadliner are implemented by gorilla's
// websocket.Conn. They are optional for a WebsocketConnection, so that adding
// deadline support does not change the WebsocketConnection interface. With a
// connection that does not support deadlines, a keepalive only closes the
// connection after missed pongs.
type readDeadliner interface {
	SetReadDeadline(t time.Time) error
}

type writeDeadliner interface {
	SetWriteDeadline(t time.Time) error
}

const (
	// WAMP uses the following websocket subprotocol identifiers for unbatched
	// modes:
	jsonWebsocketProtocol    = "wamp.2.json"
	msgpackWebsocketProtocol = "wamp.2.msgpack"
	cborWebsocketProtocol    = "wamp.2.cbor"

	ctrlTimeout = 5 * time.Second
)

// senderStopTimeout is how long to wait for the sender to exit before closing
// the connection to unblock a write that is stuck because the remote peer is
// not reading.
var senderStopTimeout = 5 * time.Second

// defaultWriteTimeout is how long a write may take before it fails, when no
// keepalive is configured. With a keepalive, the write timeout is 2 keepalive
// intervals.
const defaultWriteTimeout = 30 * time.Second

// ConnectWebsocketPeer creates a new websocket client with the specified
// config, connects the client to the websocket server at the specified URL,
// and returns the connected websocket peer.
//
// The provided Context must be non-nil. If the context expires before the
// connection is complete, an error is returned. Once successfully connected,
// any expiration of the context will not affect the connection.
func ConnectWebsocketPeer(ctx context.Context, routerURL string, serialization serialize.Serialization, tlsConfig *tls.Config, logger stdlog.StdLog, wsCfg *WebsocketConfig) (wamp.Peer, error) { //nolint:lll
	var (
		protocols   []string
		payloadType int
		serializer  serialize.Serializer
	)

	switch serialization {
	case serialize.AUTO:
		protocols = []string{jsonWebsocketProtocol, cborWebsocketProtocol, msgpackWebsocketProtocol}
	case serialize.JSON:
		protocols = []string{jsonWebsocketProtocol}
	case serialize.MSGPACK:
		protocols = []string{msgpackWebsocketProtocol}
	case serialize.CBOR:
		protocols = []string{cborWebsocketProtocol}
	default:
		return nil, fmt.Errorf("unsupported serialization: %v", serialization)
	}

	dialer := websocket.Dialer{
		Subprotocols:    protocols,
		TLSClientConfig: tlsConfig,
		Proxy:           http.ProxyFromEnvironment,
	}

	var keepAlive time.Duration = 0

	netDial := (&net.Dialer{}).DialContext
	if wsCfg != nil {
		if wsCfg.Dial != nil {
			netDial = func(_ context.Context, network, addr string) (net.Conn, error) {
				return wsCfg.Dial(network, addr)
			}
		}
		if wsCfg.ProxyURL != "" {
			proxyURL, err := url.Parse(wsCfg.ProxyURL)
			if err != nil {
				return nil, err
			}
			dialer.Proxy = http.ProxyURL(proxyURL)
		}
		dialer.Jar = wsCfg.Jar
		dialer.EnableCompression = wsCfg.EnableCompression
		keepAlive = wsCfg.KeepAlive
	}

	// The dialer does not stop the opening handshake when ctx is canceled, so
	// close the connection if ctx is done before the handshake completes. Once
	// connected, ctx no longer affects the connection.
	var connMu sync.Mutex
	var netConn net.Conn
	dialer.NetDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := netDial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		connMu.Lock()
		defer connMu.Unlock()
		if ctx.Err() != nil {
			// ctx was done before the connection could be recorded.
			_ = c.Close()
			return nil, ctx.Err()
		}
		netConn = c
		return c, nil
	}
	stop := context.AfterFunc(ctx, func() {
		connMu.Lock()
		defer connMu.Unlock()
		if netConn != nil {
			_ = netConn.Close()
		}
	})

	conn, rsp, err := dialer.DialContext(ctx, routerURL, nil)
	if !stop() {
		// ctx was done before the handshake completed, so the connection was closed,
		// or is being closed.
		if err == nil {
			_ = conn.Close()
			return nil, &WebsocketError{Err: ctx.Err(), Response: rsp}
		}
		return nil, &WebsocketError{Err: fmt.Errorf("%w: %w", ctx.Err(), err), Response: rsp}
	}
	if err != nil {
		return nil, &WebsocketError{
			Err:      err,
			Response: rsp,
		}
	}

	switch conn.Subprotocol() {
	case jsonWebsocketProtocol:
		payloadType = websocket.TextMessage
		serializer = &serialize.JSONSerializer{}
	case cborWebsocketProtocol:
		payloadType = websocket.BinaryMessage
		serializer = &serialize.CBORSerializer{}
	case msgpackWebsocketProtocol:
		payloadType = websocket.BinaryMessage
		serializer = &serialize.MessagePackSerializer{}
	}

	return NewWebsocketPeer(conn, serializer, payloadType, logger, keepAlive, 0), nil
}

// NewWebsocketPeer creates a websocket peer from an existing websocket
// connection. This is used by clients connecting to the WAMP router, and by
// servers to handle connections from clients.
//
// A non-zero keepAlive value configures a websocket "ping/pong" heartbeat,
// sending websocket "pings" every keepAlive interval. If a "pong" response is
// not received after 2 intervals have elapsed then the websocket is closed. If
// conn supports read deadlines, as gorilla's websocket.Conn does, then the
// websocket is also closed when nothing is received from the remote peer for 2
// intervals.
//
// If conn supports write deadlines, as gorilla's websocket.Conn does, then a
// write that does not complete within 2 keepAlive intervals, or 30 seconds if
// keepAlive is zero, fails and the peer stops sending.
func NewWebsocketPeer(conn WebsocketConnection, serializer serialize.Serializer, payloadType int, logger stdlog.StdLog, keepAlive time.Duration, outQueueSize int) wamp.Peer { //nolint:lll
	w := &websocketPeer{
		conn:        conn,
		serializer:  serializer,
		payloadType: payloadType,
		keepAlive:   keepAlive,
		closed:      make(chan struct{}),
		recvDone:    make(chan struct{}),
		writerDone:  make(chan struct{}),

		// The router will read from this channel and immediately dispatch the
		// message to the broker or dealer. Therefore this channel can be
		// unbuffered.
		rd: make(chan wamp.Message),

		// The channel for messages being written to the websocket should be
		// large enough to prevent blocking while waiting for a slow websocket
		// to send messages. For this reason it may be necessary for these
		// messages to be put into an outbound queue that can grow.
		wr: make(chan wamp.Message, outQueueSize),

		pings:        make(chan string, 1), // capacity must be >= 1
		stopTimeout:  senderStopTimeout,
		writeTimeout: defaultWriteTimeout,

		log: logger,
	}
	w.ctxSender, w.cancelSender = context.WithCancel(context.Background())
	w.writeDeadliner, _ = conn.(writeDeadliner)

	// Set up reading before recvHandler starts, since gorilla does not allow
	// the handlers or read deadline to be set concurrently with ReadMessage.
	w.conn.SetPingHandler(w.handlePing)
	if keepAlive != 0 {
		if keepAlive < time.Second {
			w.log.Println("Warning: very short keepalive (< 1 second)")
		}
		w.writeTimeout = 2 * keepAlive
		w.readDeadliner, _ = conn.(readDeadliner)
		w.conn.SetPongHandler(w.handlePong)
		w.extendReadDeadline()
	}

	// Sending to and receiving from websocket is handled concurrently.
	go w.recvHandler()
	if keepAlive != 0 {
		go w.sendHandlerKeepAlive()
	} else {
		go w.sendHandler()
	}

	return w
}

// handlePing queues a pong reply to a ping from the remote peer. It runs on
// the recvHandler goroutine.
func (w *websocketPeer) handlePing(m string) error {
	w.peerAlive()
	select {
	case w.pings <- m:
	default:
	}
	return nil
}

// handlePong runs on the recvHandler goroutine when a pong arrives.
func (w *websocketPeer) handlePong(string) error {
	w.peerAlive()
	return nil
}

// peerAlive records that something was received from the remote peer. It
// runs on the recvHandler goroutine.
func (w *websocketPeer) peerAlive() {
	if w.keepAlive == 0 {
		return
	}
	w.pendingPongs.Store(0)
	w.extendReadDeadline()
}

// extendReadDeadline gives the remote peer 2 keepalive intervals to send
// something. It must not be called concurrently with ReadMessage.
func (w *websocketPeer) extendReadDeadline() {
	if w.readDeadliner != nil {
		_ = w.readDeadliner.SetReadDeadline(time.Now().Add(2 * w.keepAlive))
	}
}

// writeMessage writes a message to conn. It fails if the write does not
// complete within writeTimeout.
func (w *websocketPeer) writeMessage(messageType int, data []byte) error {
	if w.writeDeadliner != nil {
		err := w.writeDeadliner.SetWriteDeadline(time.Now().Add(w.writeTimeout))
		if err != nil {
			return err
		}
	}
	return w.conn.WriteMessage(messageType, data)
}

// stopSender tells the sender to exit and waits for it. If the sender does not
// exit within stopTimeout, because it is stuck writing to a remote peer that
// is not reading, then conn is closed to unblock the write.
func (w *websocketPeer) stopSender() {
	w.cancelSender()
	timer := time.NewTimer(w.stopTimeout)
	defer timer.Stop()
	select {
	case <-w.writerDone:
	case <-timer.C:
		w.log.Print("websocket write not completing, closing websocket")
		_ = w.conn.Close()
		<-w.writerDone
	}
}

// abort sends the remote peer an ABORT for the message that could not be
// deserialized. recvHandler closes the websocket when it returns. abort stops
// the sender first, since gorilla does not allow concurrent writes, and gives
// up on the ABORT if it is not written within abortTimeout.
func (w *websocketPeer) abort(deserializeErr error) {
	select {
	case <-w.closed:
		// Peer was closed explicitly, and the sender has already exited.
		return
	default:
	}
	w.stopSender()

	b, err := w.serializer.Serialize(deserializeAbort(deserializeErr))
	if err != nil {
		return
	}
	if w.writeDeadliner != nil {
		_ = w.writeDeadliner.SetWriteDeadline(time.Now().Add(abortTimeout))
	}
	if err = w.conn.WriteMessage(w.payloadType, b); err != nil {
		w.log.Println("Error writing ABORT:", err)
	}
}

func (w *websocketPeer) Recv() <-chan wamp.Message { return w.rd }

func (w *websocketPeer) Send() chan<- wamp.Message { return w.wr }

func (w *websocketPeer) IsLocal() bool { return false }

// Close closes the websocket peer. This closes the local send channel, and
// sends a close control message to the websocket to tell the other side to
// close.
//
// Calling Close more than once, including concurrently, is safe. Calls after
// the first wait for it to finish, and then do nothing.
//
// *** Do not call Send after calling Close. ***
func (w *websocketPeer) Close() {
	w.closeOnce.Do(w.close)
}

// close does the work of Close. It must be called only once.
func (w *websocketPeer) close() {
	// Tell sendHandler to exit and discard any queued messages. Do not close
	// wr channel in case there are incoming messages during close.
	w.stopSender()
drainLoop:
	for {
		select {
		case <-w.wr:
		default:
			break drainLoop
		}
	}

	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "goodbye")

	// Tell recvHandler to close.
	close(w.closed)

	// Ignore errors since websocket may have been closed by other side first
	// in response to a goodbye message.
	_ = w.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(ctrlTimeout))
	_ = w.conn.Close()

	// Wait for the recvHandler goroutine to exit.
	<-w.recvDone
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
			b, err := w.serializer.Serialize(msg)
			if err != nil {
				w.log.Print(err)
				continue sendLoop
			}

			if err = w.writeMessage(w.payloadType, b); err != nil {
				if !wamp.IsGoodbyeAck(msg) {
					w.log.Print(err)
				}
				return
			}
		case m := <-w.pings:
			err := w.writeMessage(websocket.PongMessage, []byte(m))
			if err != nil {
				w.log.Print(err)
			}
		case <-w.ctxSender.Done():
			return
		}
	}
}

func (w *websocketPeer) sendHandlerKeepAlive() {
	defer close(w.writerDone)
	defer w.cancelSender()

	ticker := time.NewTicker(w.keepAlive)
	defer ticker.Stop()
	pingMsg := []byte("keepalive")

	senderDone := w.ctxSender.Done()
recvLoop:
	for {
		select {
		case msg := <-w.wr:
			b, err := w.serializer.Serialize(msg)
			if err != nil {
				w.log.Print(err)
				continue recvLoop
			}

			if err = w.writeMessage(w.payloadType, b); err != nil {
				if !wamp.IsGoodbyeAck(msg) {
					w.log.Print(err)
				}
				return
			}
		case <-ticker.C:
			// If missed 2 responses, close websocket.
			if w.pendingPongs.Load() >= 2 {
				w.log.Print("peer not responging to pings, closing websocket")
				_ = w.conn.Close()
				return
			}
			// Send websocket ping.
			err := w.writeMessage(websocket.PingMessage, pingMsg)
			if err != nil {
				return
			}
			w.pendingPongs.Add(1)
		case m := <-w.pings:
			err := w.writeMessage(websocket.PongMessage, []byte(m))
			if err != nil {
				w.log.Print(err)
			}
		case <-senderDone:
			return
		}
	}
}

// recvHandler pulls messages from the websocket and pushes them to the read
// channel.
func (w *websocketPeer) recvHandler() {
	defer close(w.recvDone)
	// When done, close read channel to cause router to remove session if not
	// already removed.
	defer close(w.rd)
	defer w.conn.Close() //nolint:errcheck
	for {
		msgType, b, err := w.conn.ReadMessage()
		if err != nil {
			select {
			case <-w.closed:
				// Peer was closed explicitly. sendHandler should have already
				// been told to exit.
			default:
				// Peer received control message to close. Cause sendHandler to
				// exit without closing the write channel (in case writes still
				// happening) and discard any queued messages. Wait for writer
				// to exit before closing websocket, but do not wait forever
				// for a write that is stuck. See gammazero/nexus#242.
				w.stopSender()
			}
			// The error is only one of these errors. It is generally not
			// helpful to log this, so keeping this commented out. websocket:
			// close sent websocket: close 1000 (normal): goodbye read tcp
			// addr:port->addr:port: use of closed network connection
			//w.log.Print(err)
			return
		}

		if msgType == websocket.CloseMessage {
			return
		}
		w.peerAlive()

		msg, err := w.serializer.Deserialize(b)
		if err != nil {
			// WAMP treats a message that cannot be deserialized as a protocol
			// violation, so abort instead of skipping it.
			w.log.Println("Cannot deserialize peer message:", err)
			w.abort(err)
			return
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
			timer := time.NewTimer(time.Second)
			select {
			case w.rd <- msg:
			case <-timer.C:
			}
			timer.Stop()
			return
		}
	}
}
