package router

import (
	"fmt"
	"testing"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
)

var (
	routerConfig = &RouterConfig{
		RealmConfigs: []*RealmConfig{
			{
				URI:           testRealm,
				StrictURI:     false,
				AnonymousAuth: true,
				AllowDisclose: true,
			},
		},
		Debug: false,
	}
)

const wsAddr = "127.0.0.1:8000"

func TestWSHandshakeJSON(t *testing.T) {
	defer leaktest.Check(t)()

	r, err := NewRouter(routerConfig, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	s := NewWebsocketServer(r)
	s.Upgrader.EnableCompression = true
	closer, err := s.ListenAndServe(wsAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()

	wsCfg := transport.WebsocketConfig{
		EnableCompression: true,
	}
	client, err := transport.ConnectWebsocketPeer(
		fmt.Sprintf("ws://%s/", wsAddr), serialize.JSON, nil, nil, r.Logger(), &wsCfg)
	if err != nil {
		t.Fatal(err)
	}

	client.Send(&wamp.Hello{Realm: testRealm, Details: clientRoles})
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
	defer leaktest.Check(t)()

	r, err := NewRouter(routerConfig, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	closer, err := NewWebsocketServer(r).ListenAndServe(wsAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()

	client, err := transport.ConnectWebsocketPeer(
		fmt.Sprintf("ws://%s/", wsAddr), serialize.MSGPACK, nil, nil, r.Logger(), nil)
	if err != nil {
		t.Fatal(err)
	}

	client.Send(&wamp.Hello{Realm: testRealm, Details: clientRoles})
	msg, ok := <-client.Recv()
	if !ok {
		t.Fatal("Receive buffer closed")
	}

	if _, ok = msg.(*wamp.Welcome); !ok {
		t.Fatalf("expected WELCOME, got %s: %+v", msg.MessageType(), msg)
	}
	client.Close()
}
