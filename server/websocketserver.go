package server

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/gammazero/nexus/router"
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
	router.Router
	Upgrader *websocket.Upgrader

	protocols map[string]protocol

	// The serializer to use for text frames. Defaults to JSONSerializer.
	TextSerializer serialize.Serializer
	// The serializer to use for binary frames. Defaults to JSONSerializer.
	BinarySerializer serialize.Serializer
}

// NewWebsocketServer creates
func NewWebsocketServer(r router.Router) (*WebsocketServer, error) {
	s := &WebsocketServer{
		Router:    r,
		protocols: map[string]protocol{},
	}

	s.Upgrader = &websocket.Upgrader{}
	s.RegisterProtocol(jsonWebsocketProtocol, websocket.TextMessage,
		&serialize.JSONSerializer{})
	s.RegisterProtocol(msgpackWebsocketProtocol, websocket.BinaryMessage,
		&serialize.MessagePackSerializer{})
	return s, nil
}

// RegisterProtocol registers a serializer that should be used for a given protocol string and payload type.
func (s *WebsocketServer) RegisterProtocol(proto string, payloadType int, serializer serialize.Serializer) error {
	log.Println("RegisterProtocol:", proto)
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

// ServeHTTP handles a new HTTP connection.
func (s *WebsocketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println("WebsocketServer.ServeHTTP", r.Method, r.RequestURI)
	conn, err := s.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("error upgrading to websocket connection:", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.handleWebsocket(conn)
}

func (s *WebsocketServer) handleWebsocket(conn *websocket.Conn) {
	var serializer serialize.Serializer
	var payloadType int
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

	peer := transport.NewWebsocketPeer(conn, serializer, payloadType, 16)
	if err := s.Router.Attach(peer); err != nil {
		log.Println(err)
	}
}
