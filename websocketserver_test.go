package nexus

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
)

var (
	routerConfig = &RouterConfig{
		RealmConfigs: []*RealmConfig{
			&RealmConfig{
				URI:           testRealm,
				StrictURI:     false,
				AnonymousAuth: true,
				AllowDisclose: true,
			},
		},
		Debug: true,
	}
)

func newTestWebsocketServer(t *testing.T) (int, io.Closer, stdlog.StdLog) {
	r, err := NewRouter(routerConfig, nil)
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
	return l.Addr().(*net.TCPAddr).Port, l, r.Logger()
}

func TestWSHandshakeJSON(t *testing.T) {
	port, closer, logger := newTestWebsocketServer(t)
	defer closer.Close()

	client, err := transport.ConnectWebsocketPeer(
		fmt.Sprintf("ws://localhost:%d/", port), serialize.JSON, nil, nil, logger)
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
	port, closer, logger := newTestWebsocketServer(t)
	defer closer.Close()

	client, err := transport.ConnectWebsocketPeer(
		fmt.Sprintf("ws://localhost:%d/", port), serialize.MSGPACK, nil, nil, logger)
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
