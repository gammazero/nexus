package nexus

import (
	"testing"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
)

func newTestRawSocketServer(t *testing.T) (*RawSocketServer, stdlog.StdLog) {
	r, err := NewRouter(routerConfig, nil)
	if err != nil {
		t.Fatal(err)
	}

	s, err := NewRawSocketServer(r, "tcp", ":8181", 0)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		s.Serve(false)
		r.Close()
	}()

	return s, r.Logger()
}

func TestRSHandshakeJSON(t *testing.T) {
	defer leaktest.Check(t)()
	s, lgr := newTestRawSocketServer(t)
	defer s.Close()

	client, err := transport.ConnectRawSocketPeer(s.Addr().Network(),
		s.Addr().String(), serialize.JSON, lgr, 0)
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
	s, lgr := newTestRawSocketServer(t)
	defer s.Close()

	client, err := transport.ConnectRawSocketPeer(s.Addr().Network(),
		s.Addr().String(), serialize.MSGPACK, lgr, 0)
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
