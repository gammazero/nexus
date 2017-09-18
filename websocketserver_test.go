package nexus

import (
	"fmt"
	"testing"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/stdlog"
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

func newTestWebsocketServer(t *testing.T) (*WebsocketServer, stdlog.StdLog) {
	r, err := NewRouter(routerConfig, nil)
	if err != nil {
		t.Fatal(err)
	}

	s, err := NewWebsocketServer(r, "127.0.0.1:")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		s.Serve()
		r.Close()
	}()
	return s, r.Logger()
}

func TestWSHandshakeJSON(t *testing.T) {
	defer leaktest.Check(t)()
	s, logger := newTestWebsocketServer(t)
	defer s.Close()

	client, err := transport.ConnectWebsocketPeer(
		fmt.Sprintf("ws://%s/", s.Addr()), serialize.JSON, nil, nil, logger)
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
	s, logger := newTestWebsocketServer(t)
	defer s.Close()

	client, err := transport.ConnectWebsocketPeer(
		fmt.Sprintf("ws://%s/", s.Addr()), serialize.MSGPACK, nil, nil, logger)
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
