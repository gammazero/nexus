package router

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gorilla/websocket"
)

var (
	routerConfig = &Config{
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
		context.Background(), fmt.Sprintf("ws://%s/", wsAddr), serialize.JSON, nil, r.Logger(), &wsCfg)
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
		context.Background(), fmt.Sprintf("ws://%s/", wsAddr), serialize.MSGPACK, nil, r.Logger(), nil)
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

func TestAllowOrigins(t *testing.T) {
	s := &WebsocketServer{
		Upgrader: &websocket.Upgrader{},
	}

	err := s.AllowOrigins([]string{"*foo.bAr.CoM", "*.bar.net",
		"Hello.世界", "Hello.世界.*.com", "Sevastopol.Seegson.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AllowOrigins([]string{"foo.bar.co["}); err == nil {
		t.Fatal("Expected error")
	}

	// Get the function that AllowOrigins configured the server with.
	check := s.Upgrader.CheckOrigin
	if check == nil {
		t.Fatal("Upgrader.CheckOrigin was not set")
	}

	r, err := http.NewRequest("GET", "http://nowhere.net", nil)
	if err != nil {
		t.Fatal("Failed to create request:", err)
	}
	for _, allowed := range []string{"http://foo.bar.com",
		"http://snafoo.bar.com", "https://a.b.c.baz.bar.net",
		"http://hello.世界", "http://hello.世界.X.com",
		"https://sevastopol.seegson.com", "http://nowhere.net/whatever"} {
		r.Header.Set("Origin", allowed)
		if !check(r) {
			t.Error("Should have allowed:", allowed)
		}
	}

	for _, denied := range []string{"http://cat.bar.com",
		"https://a.bar.net.com", "http://hello.世界.X.nex"} {
		r.Header.Set("Origin", denied)
		if check(r) {
			t.Error("Should have denied:", denied)
		}
	}

	// Check allow all.
	err = s.AllowOrigins([]string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	check = s.Upgrader.CheckOrigin

	for _, allowed := range []string{"http://foo.bar.com",
		"https://o.fortuna.imperatrix.mundi", "http://a.???.bb.??.net"} {
		if !check(r) {
			t.Error("Should have allowed:", allowed)
		}
	}
}

func TestAllowOriginsWithPorts(t *testing.T) {
	s := &WebsocketServer{
		Upgrader: &websocket.Upgrader{},
	}

	r, err := http.NewRequest("GET", "http://nowhere.net:", nil)
	if err != nil {
		t.Fatal("Failed to create request:", err)
	}

	// Test single port
	err = s.AllowOrigins([]string{"*.somewhere.com:8080"})
	if err != nil {
		t.Error(err)
	}
	// Get the function that AllowOrigins configured the server with.
	check := s.Upgrader.CheckOrigin

	allowed := "http://happy.somewhere.com:8080"
	r.Header.Set("Origin", allowed)
	if !check(r) {
		t.Error("Should have allowed:", allowed)
	}

	denied := "http://happy.somewhere.com:8081"
	r.Header.Set("Origin", denied)

	if check(r) {
		t.Error("Should have denied:", denied)
	}

	// Test multiple ports
	err = s.AllowOrigins([]string{
		"*.somewhere.com:8080",
		"*.somewhere.com:8905",
		"*.somewhere.com:8908",
	})
	if err != nil {
		t.Fatal(err)
	}
	check = s.Upgrader.CheckOrigin

	for _, allowed := range []string{"http://larry.somewhere.com:8080",
		"http://moe.somewhere.com:8905", "http://curley.somewhere.com:8908"} {
		r.Header.Set("Origin", allowed)
		if !check(r) {
			t.Error("Should have allowed:", allowed)
		}
	}
	for _, denied := range []string{"http://larry.somewhere.com:9080",
		"http://moe.somewhere.com:8906", "http://curley.somewhere.com:8708"} {
		r.Header.Set("Origin", denied)
		if check(r) {
			t.Error("Should have denied:", denied)
		}
	}

	// Test any port
	err = s.AllowOrigins([]string{"*.somewhere.com:*"})
	if err != nil {
		t.Error(err)
	}
	check = s.Upgrader.CheckOrigin

	allowed = "http://happy.somewhere.com:1313"
	r.Header.Set("Origin", allowed)
	if !check(r) {
		t.Error("Should have allowed:", allowed)
	}
}
