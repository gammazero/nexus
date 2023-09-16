package router

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
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
	checkGoLeaks(t)

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	s := NewWebsocketServer(r)
	s.Upgrader.EnableCompression = true
	closer, err := s.ListenAndServe(wsAddr)
	require.NoError(t, err)
	defer closer.Close()

	wsCfg := transport.WebsocketConfig{
		EnableCompression: true,
	}
	client, err := transport.ConnectWebsocketPeer(
		context.Background(), fmt.Sprintf("ws://%s/", wsAddr), serialize.JSON, nil, r.Logger(), &wsCfg)
	require.NoError(t, err)
	defer client.Close()

	client.Send() <- &wamp.Hello{Realm: testRealm, Details: clientRoles}
	msg, ok := <-client.Recv()
	require.True(t, ok, "recv chan closed")

	_, ok = msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME")
}

func TestWSHandshakeMsgpack(t *testing.T) {
	checkGoLeaks(t)

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	closer, err := NewWebsocketServer(r).ListenAndServe(wsAddr)
	require.NoError(t, err)
	defer closer.Close()

	client, err := transport.ConnectWebsocketPeer(
		context.Background(), fmt.Sprintf("ws://%s/", wsAddr), serialize.MSGPACK, nil, r.Logger(), nil)
	require.NoError(t, err)
	defer client.Close()

	client.Send() <- &wamp.Hello{Realm: testRealm, Details: clientRoles}
	msg, ok := <-client.Recv()
	require.True(t, ok, "Receive buffer closed")

	_, ok = msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME")
}

func TestAllowOrigins(t *testing.T) {
	s := &WebsocketServer{
		Upgrader: &websocket.Upgrader{},
	}

	err := s.AllowOrigins([]string{"*foo.bAr.CoM", "*.bar.net",
		"Hello.世界", "Hello.世界.*.com", "Sevastopol.Seegson.com"})
	require.NoError(t, err)
	err = s.AllowOrigins([]string{"foo.bar.co["})
	require.Error(t, err)

	// Get the function that AllowOrigins configured the server with.
	check := s.Upgrader.CheckOrigin
	require.NotNil(t, check, "Upgrader.CheckOrigin was not set")

	r, err := http.NewRequest("GET", "http://nowhere.net", nil)
	require.NoError(t, err)
	for _, allowed := range []string{"http://foo.bar.com",
		"http://snafoo.bar.com", "https://a.b.c.baz.bar.net",
		"http://hello.世界", "http://hello.世界.X.com",
		"https://sevastopol.seegson.com", "http://nowhere.net/whatever"} {
		r.Header.Set("Origin", allowed)
		require.Truef(t, check(r), "Should have allowed: %s", allowed)
	}

	for _, denied := range []string{"http://cat.bar.com",
		"https://a.bar.net.com", "http://hello.世界.X.nex"} {
		r.Header.Set("Origin", denied)
		require.Falsef(t, check(r), "Should have denied: %s", denied)
	}

	// Check allow all.
	err = s.AllowOrigins([]string{"*"})
	require.NoError(t, err)
	check = s.Upgrader.CheckOrigin

	for _, allowed := range []string{"http://foo.bar.com",
		"https://o.fortuna.imperatrix.mundi", "http://a.???.bb.??.net"} {
		require.Truef(t, check(r), "Should have allowed: %s", allowed)
	}
}

func TestAllowOriginsWithPorts(t *testing.T) {
	s := &WebsocketServer{
		Upgrader: &websocket.Upgrader{},
	}

	r, err := http.NewRequest("GET", "http://nowhere.net:", nil)
	require.NoError(t, err)

	// Test single port
	err = s.AllowOrigins([]string{"*.somewhere.com:8080"})
	require.NoError(t, err)
	// Get the function that AllowOrigins configured the server with.
	check := s.Upgrader.CheckOrigin

	allowed := "http://happy.somewhere.com:8080"
	r.Header.Set("Origin", allowed)
	require.Truef(t, check(r), "Should have allowed: %s", allowed)

	denied := "http://happy.somewhere.com:8081"
	r.Header.Set("Origin", denied)

	require.Falsef(t, check(r), "Should have denied: %s", denied)

	// Test multiple ports
	err = s.AllowOrigins([]string{
		"*.somewhere.com:8080",
		"*.somewhere.com:8905",
		"*.somewhere.com:8908",
	})
	require.NoError(t, err)
	check = s.Upgrader.CheckOrigin

	for _, allowed := range []string{"http://larry.somewhere.com:8080",
		"http://moe.somewhere.com:8905", "http://curley.somewhere.com:8908"} {
		r.Header.Set("Origin", allowed)
		require.Truef(t, check(r), "Should have allowed: %s", allowed)
	}
	for _, denied := range []string{"http://larry.somewhere.com:9080",
		"http://moe.somewhere.com:8906", "http://curley.somewhere.com:8708"} {
		r.Header.Set("Origin", denied)
		require.Falsef(t, check(r), "Should have denied: %s", denied)
	}

	// Test any port
	err = s.AllowOrigins([]string{"*.somewhere.com:*"})
	require.NoError(t, err)
	check = s.Upgrader.CheckOrigin

	allowed = "http://happy.somewhere.com:1313"
	r.Header.Set("Origin", allowed)
	require.Truef(t, check(r), "Should have allowed: %s", allowed)
}
