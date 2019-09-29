package router

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gorilla/websocket"
)

const (
	jsonWebsocketProtocol    = "wamp.2.json"
	msgpackWebsocketProtocol = "wamp.2.msgpack"
	cborWebsocketProtocol    = "wamp.2.cbor"

	defaultOutQueueSize = 16
)

type protocol struct {
	payloadType int
	serializer  serialize.Serializer
}

// WebsocketServer handles websocket connections.
//
// Origin Considerations
//
// Web browsers allow Javascript applications to open a WebSocket connection to
// any host.  It is up to the server to enforce an origin policy using the
// Origin request header sent by the browser.  To specify origins allowed by
// the server, call AllowOrigins() to allow origins matching glob patterns, or
// assign a custom function to the server's Upgrader.CheckOrigin.  See
// AllowOrigins() for details.
type WebsocketServer struct {
	// Upgrader specifies parameters for upgrading an HTTP connection to a
	// websocket connection.  See:
	// https://godoc.org/github.com/gorilla/websocket#Upgrader
	Upgrader *websocket.Upgrader

	// Serializer for text frames.  Defaults to JSONSerializer.
	TextSerializer serialize.Serializer
	// Serializer for binary frames.  Defaults to MessagePackSerializer.
	BinarySerializer serialize.Serializer

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
	EnableTrackingCookie bool
	// EnableRequestCapture tells the server to include the upgrade HTTP
	// request in the HELLO and session details.  It is stored in
	// Details.transport.auth.request|*http.Request making it available to
	// authenticator and authorizer logic.
	EnableRequestCapture bool

	// KeepAlive configures a websocket "ping/pong" heartbeat when set to a
	// non-zero value.  KeepAlive is the interval between websocket "pings".
	// If a "pong" response is not received after 2 intervals have elapsed then
	// the websocket connection is closed.
	KeepAlive time.Duration

	// OutQueueSize is the maximum number of pending outbound messages, per
	// client.  The default is defaultOutQueueSize.
	OutQueueSize int

	router    Router
	protocols map[string]protocol
}

// NewWebsocketServer takes a router instance and creates a new websocket
// server.
//
// Optional websocket server configuration can be set, after creating the
// server instance, by setting WebsocketServer.Upgrader and other
// WebsocketServer members directly.  Use then WebsocketServer.AllowOrigins()
// function to specify what origins to allow for CORS support.
//
// To run the websocket server, call one of the server's
// ListenAndServe methods:
//
//     s := NewWebsocketServer(r)
//     closer, err := s.ListenAndServe(address)
//
// Or, use the various ListenAndServe functions provided by net/http.  This
// works because WebsocketServer implements the http.Handler interface:
//
//     s := NewWebsocketServer(r)
//     server := &http.Server{
//         Handler: s,
//         Addr:    address,
//     }
//     server.ListenAndServe()
func NewWebsocketServer(r Router) *WebsocketServer {
	s := &WebsocketServer{
		router:    r,
		protocols: map[string]protocol{},
	}
	s.Upgrader = &websocket.Upgrader{}
	s.addProtocol(jsonWebsocketProtocol, websocket.TextMessage,
		&serialize.JSONSerializer{})
	s.addProtocol(msgpackWebsocketProtocol, websocket.BinaryMessage,
		&serialize.MessagePackSerializer{})
	s.addProtocol(cborWebsocketProtocol, websocket.BinaryMessage,
		&serialize.CBORSerializer{})

	return s
}

// AllowOrigins configures the server to allow connections when the Origin host
// matches a specified glob pattern.
//
// If the origin request header in the websocket upgrade request is present,
// then the connection is allowed if the Origin host is equal to the Host
// request header or Origin matches one of the allowed patterns.  A pattern is
// in the form of a shell glob as described here:
// https://golang.org/pkg/path/filepath/#Match Glob matching is
// case-insensitive.
//
// For example:
//   s := NewWebsocketServer(r)
//   s.AllowOrigins([]string{"*.domain.com", "*.domain.net"})
//
// To allow all origins, specify a wildcard only:
//   s.AllowOrigins([]string{"*"})
//
// Origins with Ports
//
// To allow origins that have a port number, specify the port as part of the
// origin pattern, or use a wildcard to match the ports.
//
// Allow a specific port, by specifying the expected port number:
//   s.AllowOrigins([]string{"*.somewhere.com:8080"})
//
// Allow individual ports, by specifying each port:
//  err := s.AllowOrigins([]string{
//      "*.somewhere.com:8080",
//      "*.somewhere.com:8905",
//      "*.somewhere.com:8908",
//  })
//
// Allow any port, by specifying a wildcard. Be sure to include the colon ":"
// so that the pattern does not match a longer origin.  Without the ":" this
// example would also match "x.somewhere.comics.net"
//   err := s.AllowOrigins([]string{"*.somewhere.com:*"})
//
// Custom Origin Checks
//
// Alternatively, a custom function my be configured to supplied any origin
// checking logic your application needs.  The WebsocketServer calls the
// function specified in the WebsocketServer.Upgrader.CheckOrigin field to
// check the origin.  If the CheckOrigin function returns false, then the
// WebSocket handshake fails with HTTP status 403.  To supply a CheckOrigin
// function:
//
//     s := NewWebsocketServer(r)
//     s.Upgrader.CheckOrigin = func(r *http.Request) bool { ... }
//
// Default Behavior
//
// If AllowOrigins() is not called, and Upgrader.CheckOrigin is nil, then a
// safe default is used: fail the handshake if the Origin request header is
// present and the Origin host is not equal to the Host request header.
func (s *WebsocketServer) AllowOrigins(origins []string) error {
	if len(origins) == 0 {
		return nil
	}
	var exacts, globs []string
	for _, o := range origins {
		// If allowing any origins, then return simple "true" function.
		if o == "*" {
			s.Upgrader.CheckOrigin = func(r *http.Request) bool { return true }
			return nil
		}

		// Do exact matching whenever possible, since it is more efficient.
		if strings.ContainsAny(o, "*?[]^") {
			if _, err := filepath.Match(o, o); err != nil {
				return fmt.Errorf("error allowing origin, %s: %s", err, o)
			}
			globs = append(globs, strings.ToLower(o))
		} else {
			exacts = append(exacts, o)
		}
	}
	s.Upgrader.CheckOrigin = func(r *http.Request) bool {
		return checkOrigin(exacts, globs, r)
	}
	return nil
}

// ListenAndServe listens on the specified TCP address and starts a goroutine
// that accepts new client connections until the returned io.closer is closed.
func (s *WebsocketServer) ListenAndServe(address string) (io.Closer, error) {
	// Call Listen separate from Serve to check for error listening.
	l, err := net.Listen("tcp", address)
	if err != nil {
		s.router.Logger().Print(err)
		return nil, err
	}

	// Run service on configured port.
	server := &http.Server{
		Handler: s,
		Addr:    l.Addr().String(),
	}
	go server.Serve(l)
	return l, nil
}

// ListenAndServeTLS listens on the specified TCP address and starts a
// goroutine that accepts new TLS client connections until the returned
// io.closer is closed.  If tls.Config does not already contain a certificate,
// then certFile and keyFile, if specified, are used to load an X509
// certificate.
func (s *WebsocketServer) ListenAndServeTLS(address string, tlscfg *tls.Config, certFile, keyFile string) (io.Closer, error) {
	// Load certificate here, instead of in ServeTLS, to check for error before
	// serving.
	var hasCert bool
	if tlscfg == nil {
		tlscfg = &tls.Config{}
	} else if len(tlscfg.Certificates) > 0 || tlscfg.GetCertificate != nil {
		hasCert = true
	}

	if !hasCert || certFile != "" || keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("error loading X509 key pair: %s", err)
		}
		tlscfg.Certificates = append(tlscfg.Certificates, cert)
	}

	// Call Listen separate from Serve to check for error listening.
	l, err := net.Listen("tcp", address)
	if err != nil {
		s.router.Logger().Print(err)
		return nil, err
	}

	// Run service on configured port.
	server := &http.Server{
		Handler:   s,
		Addr:      l.Addr().String(),
		TLSConfig: tlscfg,
	}
	go server.ServeTLS(l, "", "")
	return l, nil
}

// ServeHTTP handles HTTP connections.
func (s *WebsocketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var authDict wamp.Dict
	var nextCookie *http.Cookie

	// If tracking cookie is enabled, then read the tracking cookie from the
	// request header, if it contains the cookie.  Generate a new tracking
	// cookie for next time and put it in the response header.
	if s.EnableTrackingCookie {
		if authDict == nil {
			authDict = wamp.Dict{}
		}
		const cookieName = "nexus-wamp-cookie"
		if reqCk, err := r.Cookie(cookieName); err == nil {
			authDict["cookie"] = reqCk
			//fmt.Println("===> Received tracking cookie: ", reqCk)
		}
		b := make([]byte, 18)
		_, err := rand.Read(b)
		if err == nil {
			// Create new auth cookie with 20 byte random value.
			nextCookie = &http.Cookie{
				Name:  cookieName,
				Value: base64.URLEncoding.EncodeToString(b),
			}
			http.SetCookie(w, nextCookie)
			authDict["nextcookie"] = nextCookie
			//fmt.Println("===> Sent Next tracking cookie:", nextCookie)
			//fmt.Println()
		}
	}

	// If request capture is enabled, then save the HTTP upgrade http.Request
	// in the HELLO and session details as transport.auth details.request.
	if s.EnableRequestCapture {
		if authDict == nil {
			authDict = wamp.Dict{}
		}
		authDict["request"] = r
	}

	conn, err := s.Upgrader.Upgrade(w, r, w.Header())
	if err != nil {
		s.router.Logger().Println("Error upgrading to websocket connection:", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.handleWebsocket(conn, wamp.Dict{"auth": authDict})
}

// addProtocol registers a serializer for protocol and payload type.
func (s *WebsocketServer) addProtocol(proto string, payloadType int, serializer serialize.Serializer) error {
	if payloadType != websocket.TextMessage && payloadType != websocket.BinaryMessage {
		return fmt.Errorf("invalid payload type: %d", payloadType)
	}
	if _, ok := s.protocols[proto]; ok {
		return errors.New("protocol already registered: " + proto)
	}
	s.protocols[proto] = protocol{payloadType, serializer}
	s.Upgrader.Subprotocols = append(s.Upgrader.Subprotocols, proto)
	return nil
}

func (s *WebsocketServer) handleWebsocket(conn *websocket.Conn, transportDetails wamp.Dict) {
	var serializer serialize.Serializer
	var payloadType int
	// Get serializer and payload type for protocol.
	if proto, ok := s.protocols[conn.Subprotocol()]; ok {
		serializer = proto.serializer
		payloadType = proto.payloadType
	} else {
		// Although gorilla rejects connections with unregistered protocols,
		// other websocket implementations may not.
		switch conn.Subprotocol() {
		case jsonWebsocketProtocol:
			serializer = &serialize.JSONSerializer{}
			payloadType = websocket.TextMessage
		case msgpackWebsocketProtocol:
			serializer = &serialize.MessagePackSerializer{}
			payloadType = websocket.BinaryMessage
		case cborWebsocketProtocol:
			serializer = &serialize.CBORSerializer{}
			payloadType = websocket.BinaryMessage
		default:
			conn.Close()
			return
		}
	}

	// Create a websocket peer from the websocket connection and attach the
	// peer to the router.
	qsize := s.OutQueueSize
	if qsize == 0 {
		qsize = defaultOutQueueSize
	}
	peer := transport.NewWebsocketPeer(conn, serializer, payloadType, s.router.Logger(), s.KeepAlive, qsize)
	if err := s.router.AttachClient(peer, transportDetails); err != nil {
		s.router.Logger().Println("Error attaching to router:", err)
	}
}

// checkOrigin returns true if the origin is not set, is equal to the request
// host, or matches one of the allowed patterns.  This function is assigned to
// the server's Upgrader.CheckOrigin when AllowOrigins() is called.
func checkOrigin(exacts, globs []string, r *http.Request) bool {
	origin := r.Header["Origin"]
	if len(origin) == 0 {
		return true
	}
	u, err := url.Parse(origin[0])
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}

	for i := range exacts {
		if strings.EqualFold(u.Host, exacts[i]) {
			return true
		}
	}
	if len(globs) != 0 {
		host := strings.ToLower(u.Host)
		for i := range globs {
			if ok, _ := filepath.Match(globs[i], host); ok {
				return true
			}
		}
	}
	return false
}
