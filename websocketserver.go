package nexus

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gorilla/websocket"
)

const (
	jsonWebsocketProtocol    = "wamp.2.json"
	msgpackWebsocketProtocol = "wamp.2.msgpack"
)

type protocol struct {
	payloadType int
	serializer  serialize.Serializer
}

// WebsocketServer handles websocket connections.
type WebsocketServer struct {
	Upgrader *websocket.Upgrader

	// Serializer for text frames.  Defaults to JSONSerializer.
	TextSerializer serialize.Serializer
	// Serializer for binary frames.  Defaults to MessagePackSerializer.
	BinarySerializer serialize.Serializer

	router    Router
	listener  net.Listener
	addr      net.Addr
	url       string
	protocols map[string]protocol
	log       stdlog.StdLog
}

// NewWebsocketServer takes a router instance and creates a new websocket
// server.
func NewWebsocketServer(r Router, address string) (*WebsocketServer, error) {
	s := &WebsocketServer{
		router:    r,
		protocols: map[string]protocol{},
		log:       r.Logger(),
	}

	s.Upgrader = &websocket.Upgrader{}
	s.addProtocol(jsonWebsocketProtocol, websocket.TextMessage,
		&serialize.JSONSerializer{})
	s.addProtocol(msgpackWebsocketProtocol, websocket.BinaryMessage,
		&serialize.MessagePackSerializer{})

	l, err := net.Listen("tcp", address)
	if err != nil {
		s.log.Print(err)
		return nil, err
	}
	s.listener = l
	s.addr = l.(*net.TCPListener).Addr()
	s.url = fmt.Sprintf("ws://%s/", s.addr)
	return s, nil
}

func (s *WebsocketServer) Addr() net.Addr { return s.addr }

func (s *WebsocketServer) URL() string { return s.url }

func (s *WebsocketServer) Serve() error {
	// Run service on configured port.
	server := &http.Server{
		Handler: s,
		Addr:    s.addr.String(),
	}
	return server.Serve(s.listener)
}

func (s *WebsocketServer) ServeTLS(tlsConfig *tls.Config, certFile, keyFile string) error {
	// Run service on configured port.
	server := &http.Server{
		Handler:   s,
		Addr:      s.addr.String(),
		TLSConfig: tlsConfig,
	}

	// With Go 1.9 everything below can be replaces with this:
	//server.ServeTLS(s.listener, certFile, keyFile)

	configHasCert := len(tlsConfig.Certificates) > 0 || tlsConfig.GetCertificate != nil
	if !configHasCert || certFile != "" || keyFile != "" {
		var err error
		tlsConfig.Certificates = make([]tls.Certificate, 1)
		tlsConfig.Certificates[0], err = tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return err
		}
	}
	return server.Serve(s.listener)
}

func (s *WebsocketServer) Close() {
	s.listener.Close()
}

// ServeHTTP handles HTTP connections.
func (s *WebsocketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Println("Error upgrading to websocket connection:", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.handleWebsocket(conn)
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

func (s *WebsocketServer) handleWebsocket(conn *websocket.Conn) {
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
		default:
			conn.Close()
			return
		}
	}

	// Create a websocket peer from the websocket connection and attach the
	// peer to the router.
	peer := transport.NewWebsocketPeer(conn, serializer, payloadType, s.log)
	if err := s.router.Attach(peer); err != nil {
		s.log.Println("Error attaching to router:", err)
	}
}
