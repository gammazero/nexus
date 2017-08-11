package server

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
)

const (
	testRealm = wamp.URI("test.realm")
)

var (
	routerConfig = &router.RouterConfig{
		RealmConfigs: []*router.RealmConfig{
			&router.RealmConfig{
				URI:           testRealm,
				StrictURI:     false,
				AnonymousAuth: true,
				AllowDisclose: true,
			},
		},
	}
)

func clientRoles() wamp.Dict {
	return wamp.Dict{
		"roles": wamp.Dict{
			"publisher": wamp.Dict{
				"features": wamp.Dict{
					"subscriber_blackwhite_listing": true,
				},
			},
			"subscriber": wamp.Dict{},
			"callee":     wamp.Dict{},
			"caller":     wamp.Dict{},
		},
	}
}

func newTestWebsocketServer(t *testing.T) (int, io.Closer) {
	r, err := router.NewRouter(routerConfig)
	router.DebugEnabled = true
	if err != nil {
		t.Fatal(err)
	}

	s := NewWebsocketServer(r)
	server := &http.Server{
		Handler: s,
	}

	var addr net.TCPAddr
	l, err := net.ListenTCP("tcp", &addr)
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(l)
	return l.Addr().(*net.TCPAddr).Port, l
}

func TestWSHandshakeJSON(t *testing.T) {
	port, closer := newTestWebsocketServer(t)
	defer closer.Close()

	client, err := transport.ConnectWebsocketPeer(
		fmt.Sprintf("ws://localhost:%d/", port), serialize.JSON, nil, nil, router.Logger())
	if err != nil {
		t.Fatal(err)
	}

	client.Send(&wamp.Hello{Realm: testRealm, Details: clientRoles()})
	msg, ok := <-client.Recv()
	if !ok {
		t.Fatal("recv chan closed")
	}

	if _, ok = msg.(*wamp.Welcome); !ok {
		t.Fatal("expected WELCOME, got", msg.MessageType())
	}
	client.Close()
}

func TestWSHandshakeMsgpack(t *testing.T) {
	port, closer := newTestWebsocketServer(t)
	defer closer.Close()

	client, err := transport.ConnectWebsocketPeer(
		fmt.Sprintf("ws://localhost:%d/", port), serialize.MSGPACK, nil, nil, router.Logger())
	if err != nil {
		t.Fatal(err)
	}

	client.Send(&wamp.Hello{Realm: testRealm, Details: clientRoles()})
	msg, ok := <-client.Recv()
	if !ok {
		t.Fatal("Receive buffer closed")
	}

	if _, ok = msg.(*wamp.Welcome); !ok {
		t.Fatalf("expected WELCOME, got %s: %+v", msg.MessageType(), msg)
	}
	client.Close()
}
