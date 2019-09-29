package router

import (
	"context"
	"testing"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

const tcpAddr = "127.0.0.1:8181"

func TestRSHandshakeJSON(t *testing.T) {
	defer leaktest.Check(t)()

	r, err := NewRouter(routerConfig, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	clsr, err := NewRawSocketServer(r).ListenAndServe("tcp", tcpAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer clsr.Close()

	client, err := transport.ConnectRawSocketPeer(context.Background(), "tcp",
		tcpAddr, serialize.JSON, r.Logger(), 0)
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

func TestRSHandshakeMsgpack(t *testing.T) {
	defer leaktest.Check(t)()

	r, err := NewRouter(routerConfig, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	clsr, err := NewRawSocketServer(r).ListenAndServe("tcp", tcpAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer clsr.Close()

	client, err := transport.ConnectRawSocketPeer(context.Background(), "tcp",
		tcpAddr, serialize.MSGPACK, r.Logger(), 0)
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
