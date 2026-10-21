package router //nolint:testpackage

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
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

func TestWSCookieAttributes(t *testing.T) {
	// http library treats the samesite attribute for strings as if no attribute was set
	sameSiteUnsetValue := http.SameSiteDefaultMode - 1
	tests := []struct {
		name         string
		setSameSite  *http.SameSite
		setSecure    *bool
		wantSameSite http.SameSite
		wantIsSecure bool
	}{
		{
			name:         "default settings",
			wantSameSite: sameSiteUnsetValue,
			wantIsSecure: false,
		},
		{
			name:         "same site strict",
			setSameSite:  new(http.SameSiteStrictMode),
			wantSameSite: http.SameSiteStrictMode,
			wantIsSecure: false,
		},
		{
			name:         "same site none",
			setSameSite:  new(http.SameSiteNoneMode),
			wantSameSite: http.SameSiteNoneMode,
			wantIsSecure: false,
		},
		{
			name:         "secure is true",
			setSecure:    new(true),
			wantSameSite: sameSiteUnsetValue,
			wantIsSecure: true,
		},
		{
			name:         "secure is false",
			setSecure:    new(false),
			wantSameSite: sameSiteUnsetValue,
			wantIsSecure: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRouter(routerConfig, nil)
			require.NoError(t, err)
			defer r.Close()

			wss := NewWebsocketServer(r)
			wss.EnableTrackingCookie = true
			if tt.setSecure != nil {
				wss.TrackingCookieSecureAttribute = *tt.setSecure
			}
			if tt.setSameSite != nil {
				wss.TrackingCookieSameSiteAttribute = *tt.setSameSite
			}
			closer, err := wss.ListenAndServe(wsAddr)
			require.NoError(t, err)
			defer closer.Close()

			dialer := websocket.Dialer{
				Subprotocols:    []string{jsonWebsocketProtocol, cborWebsocketProtocol, msgpackWebsocketProtocol},
				TLSClientConfig: nil,
			}
			conn, rsp, err := dialer.DialContext(context.Background(), fmt.Sprintf("ws://%s/", wsAddr), nil)
			require.NoError(t, err)
			defer conn.Close()

			for _, c := range rsp.Cookies() {
				if c.Name == "nexus-wamp-cookie" {
					require.Equal(t, c.SameSite, tt.wantSameSite)
					require.Equal(t, c.Secure, tt.wantIsSecure)
				}
			}
		})
	}

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

// lockedBuffer is a bytes.Buffer safe for concurrent writes from the router's
// logger goroutines and reads from the test.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A HELLO for a realm the router does not have is ABORTed, and the log line
// names the peer so the source of a reconnect loop can be traced.
func TestWSAttachFailureLogsPeer(t *testing.T) {
	logBuf := &lockedBuffer{}
	r, err := NewRouter(routerConfig, log.New(logBuf, "", 0))
	require.NoError(t, err)
	defer r.Close()

	closer, err := NewWebsocketServer(r).ListenAndServe(wsAddr)
	require.NoError(t, err)
	defer closer.Close()

	client, err := transport.ConnectWebsocketPeer(
		context.Background(), fmt.Sprintf("ws://%s/", wsAddr), serialize.JSON, nil, r.Logger(), nil)
	require.NoError(t, err)
	defer client.Close()

	client.Send() <- &wamp.Hello{Realm: "realm-1-1-dev", Details: clientRoles}
	msg, ok := <-client.Recv()
	require.True(t, ok, "recv chan closed")
	_, ok = msg.(*wamp.Abort)
	require.True(t, ok, "expected ABORT")

	require.Eventually(t, func() bool {
		return strings.Contains(logBuf.String(), "Client cannot attach to router")
	}, 2*time.Second, 10*time.Millisecond)
	line := logBuf.String()
	require.Contains(t, line, `no realm "realm-1-1-dev" exists`)
	require.Regexp(t, `\(peer 127\.0\.0\.1:\d+\)`, line)
}

type addrMockConn struct {
	transport.WebsocketConnection
	addr net.Addr
}

func (c addrMockConn) RemoteAddr() net.Addr { return c.addr }

type addrlessMockConn struct{ transport.WebsocketConnection }

func TestPeerDescription(t *testing.T) {
	tcp := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 7), Port: 4321}

	require.Equal(t, "unknown", peerDescription(addrlessMockConn{}, wamp.Dict{}))
	require.Equal(t, "10.0.0.7:4321", peerDescription(addrMockConn{addr: tcp}, wamp.Dict{"auth": wamp.Dict(nil)}))

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.1.1.1")
	got := peerDescription(addrMockConn{addr: tcp}, wamp.Dict{"auth": wamp.Dict{"request": req}})
	require.Equal(t, "10.0.0.7:4321 forwarded-for 203.0.113.9, 10.1.1.1", got)
}
