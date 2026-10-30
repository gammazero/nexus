package router //nolint:testpackage

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"maps"
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

// TestWSSubprotocolClientPreference pins gammazero/nexus#282.
//
// Per RFC 6455 §4.2.2, the server "MUST select one of the values from the
// [client's] Sec-WebSocket-Protocol field". The client lists protocols in
// preference order; the server must walk the client's list and pick the
// first one it supports — not walk its own list and pick the first the
// client also offered.
//
// Concretely, nexus's WebsocketServer registers subprotocols in
// addProtocol() order: json, msgpack, cbor. A client that offers
// [cbor, json] should get cbor, but pre-fix it gets json because the
// underlying gorilla.Upgrader iterates the server's list outermost.
func TestWSSubprotocolClientPreference(t *testing.T) {
	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	closer, err := NewWebsocketServer(r).ListenAndServe(wsAddr)
	require.NoError(t, err)
	defer closer.Close()

	// Cases: each row pairs the client's preference-ordered list with the
	// subprotocol the server should select per RFC 6455.
	cases := []struct {
		name           string
		clientOffers   []string
		wantNegotiated string
	}{
		{
			name:           "cbor preferred over json",
			clientOffers:   []string{cborWebsocketProtocol, jsonWebsocketProtocol},
			wantNegotiated: cborWebsocketProtocol,
		},
		{
			name:           "msgpack preferred over json",
			clientOffers:   []string{msgpackWebsocketProtocol, jsonWebsocketProtocol},
			wantNegotiated: msgpackWebsocketProtocol,
		},
		{
			name:           "json preferred over cbor",
			clientOffers:   []string{jsonWebsocketProtocol, cborWebsocketProtocol},
			wantNegotiated: jsonWebsocketProtocol,
		},
		{
			name: "first supported wins when unsupported precedes supported",
			clientOffers: []string{
				"wamp.2.unknown", cborWebsocketProtocol, jsonWebsocketProtocol,
			},
			wantNegotiated: cborWebsocketProtocol,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dialer := websocket.Dialer{Subprotocols: tc.clientOffers}
			conn, rsp, err := dialer.DialContext(
				context.Background(), fmt.Sprintf("ws://%s/", wsAddr), nil)
			require.NoError(t, err)
			defer conn.Close()
			defer rsp.Body.Close()

			require.Equal(t, tc.wantNegotiated, conn.Subprotocol(),
				"server picked a server-preferred protocol instead of a client-preferred one")
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

// TestWSFailedUpgradeRepliesOnce pins that a rejected handshake produces
// exactly one HTTP error response, and that the response does not include a
// negotiated subprotocol. gorilla's Upgrade already replies before handing the
// error back, so replying a second time in ServeHTTP produced a "superfluous
// response.WriteHeader call" from net/http and appended a second error body.
func TestWSFailedUpgradeRepliesOnce(t *testing.T) {
	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	// handshake returns the headers of a valid websocket handshake that offers a
	// supported subprotocol.
	handshake := func() http.Header {
		return http.Header{
			"Connection":             {"Upgrade"},
			"Upgrade":                {"websocket"},
			"Sec-Websocket-Version":  {"13"},
			"Sec-Websocket-Key":      {"dGhlIHNhbXBsZSBub25jZQ=="},
			"Sec-Websocket-Protocol": {jsonWebsocketProtocol},
		}
	}

	cases := []struct {
		name       string
		method     string
		header     http.Header
		rejectAll  bool
		upgradeErr func(http.ResponseWriter, *http.Request, int, error)
		wantStatus int
		wantBody   string
	}{
		{
			name:       "not a websocket handshake",
			method:     http.MethodGet,
			header:     http.Header{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "method not GET",
			method:     http.MethodPost,
			header:     handshake(),
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:   "unsupported version",
			method: http.MethodGet,
			header: func() http.Header {
				h := handshake()
				h.Set("Sec-Websocket-Version", "8")
				return h
			}(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "origin not allowed",
			method:     http.MethodGet,
			header:     handshake(),
			rejectAll:  true,
			wantStatus: http.StatusForbidden,
		},
		{
			name:      "custom Upgrader.Error",
			method:    http.MethodGet,
			header:    handshake(),
			rejectAll: true,
			upgradeErr: func(w http.ResponseWriter, _ *http.Request, _ int, _ error) {
				http.Error(w, "custom rejection", http.StatusTeapot)
			},
			wantStatus: http.StatusTeapot,
			wantBody:   "custom rejection\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewWebsocketServer(r)
			if tc.rejectAll {
				s.Upgrader.CheckOrigin = func(*http.Request) bool { return false }
			}
			s.Upgrader.Error = tc.upgradeErr

			req := httptest.NewRequest(tc.method, "/ws", nil)
			req.Header = tc.header
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code)
			wantBody := tc.wantBody
			if wantBody == "" {
				// What gorilla writes by default. Anything more means that ServeHTTP answered
				// a request that was already answered.
				wantBody = http.StatusText(tc.wantStatus) + "\n"
			}
			require.Equal(t, wantBody, rec.Body.String())
			require.Empty(t, rec.Header().Get("Sec-Websocket-Protocol"),
				"failed upgrade must not include a subprotocol")
		})
	}
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

// logLine returns the line of the log that contains substr, or "" if there is
// none.
func logLine(logBuf *lockedBuffer, substr string) string {
	for line := range strings.Lines(logBuf.String()) {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
}

// requireLogLine waits for a line containing substr to be logged, and returns
// it.
func requireLogLine(t *testing.T, logBuf *lockedBuffer, substr string) string {
	t.Helper()
	require.Eventually(t, func() bool { return logLine(logBuf, substr) != "" },
		2*time.Second, 10*time.Millisecond, "no log line containing %q", substr)
	return logLine(logBuf, substr)
}

// A HELLO for a realm the router does not have is ABORTed, and the log line
// names the peer so the source of a reconnect loop can be traced. This works
// without EnableRequestCapture, since that is off by default.
func TestWSAttachFailureLogsPeer(t *testing.T) {
	cases := []struct {
		name         string
		forwardedFor []string
		wantPeer     string
	}{
		{
			name:     "direct",
			wantPeer: `\(peer 127\.0\.0\.1:\d+\)`,
		},
		{
			name:         "behind proxy",
			forwardedFor: []string{"203.0.113.9, 10.1.1.1"},
			wantPeer: `\(peer 127\.0\.0\.1:\d+, ` +
				`X-Forwarded-For "203\.0\.113\.9, 10\.1\.1\.1"\)`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logBuf := &lockedBuffer{}
			r, err := NewRouter(routerConfig, log.New(logBuf, "", 0))
			require.NoError(t, err)
			defer r.Close()

			closer, err := NewWebsocketServer(r).ListenAndServe(wsAddr)
			require.NoError(t, err)
			defer closer.Close()

			dialer := websocket.Dialer{Subprotocols: []string{jsonWebsocketProtocol}}
			header := http.Header{"X-Forwarded-For": tc.forwardedFor}
			conn, rsp, err := dialer.DialContext(context.Background(),
				fmt.Sprintf("ws://%s/", wsAddr), header)
			require.NoError(t, err)
			defer conn.Close()
			defer rsp.Body.Close()

			hello := []byte(`[1,"realm-1-1-dev",{}]`)
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, hello))
			_, msg, err := conn.ReadMessage()
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(string(msg), "[3,"), "expected ABORT, got %s", msg)

			line := requireLogLine(t, logBuf, "Client cannot attach to router")
			require.Contains(t, line, `no realm "realm-1-1-dev" exists`)
			require.Regexp(t, tc.wantPeer, line)
		})
	}
}

func TestPeerDescription(t *testing.T) {
	require.Equal(t, "unknown", peerDescription("", nil))
	require.Equal(t, "10.0.0.7:4321", peerDescription("10.0.0.7:4321", nil))
	require.Equal(t, `10.0.0.7:4321, X-Forwarded-For "203.0.113.9, 10.1.1.1"`,
		peerDescription("10.0.0.7:4321", []string{"203.0.113.9", "10.1.1.1"}))

	// The header is supplied by the client, so it is quoted, which keeps it from
	// passing as router output, and truncated.
	got := peerDescription("10.0.0.7:4321", []string{`1.2.3.4) (peer "x"`})
	require.Equal(t, `10.0.0.7:4321, X-Forwarded-For "1.2.3.4) (peer \"x\""`, got)

	got = peerDescription("10.0.0.7:4321", []string{strings.Repeat("a", 10000)})
	require.Equal(t, `10.0.0.7:4321, X-Forwarded-For "`+
		strings.Repeat("a", maxForwardedForLog)+`..."`, got)
}

func TestCheckOrigin(t *testing.T) {
	newRequest := func(origin string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://router.example.com/ws", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	exacts := []string{"exact.example.org"}
	globs := []string{"*.glob.example.net"}

	// No Origin header, as from a client that is not a browser.
	require.True(t, checkOrigin(nil, nil, newRequest("")))

	// The Origin host is the request host, compared without case.
	require.True(t, checkOrigin(nil, nil, newRequest("https://Router.Example.com")))

	// An Origin that is not a URL is denied.
	require.False(t, checkOrigin(exacts, globs, newRequest("http://[::1")))
	require.False(t, checkOrigin(exacts, globs, newRequest("%zz")))

	// The Origin host matches an exact host or a glob, without case.
	require.True(t, checkOrigin(exacts, globs, newRequest("http://EXACT.example.org")))
	require.True(t, checkOrigin(exacts, globs, newRequest("http://a.Glob.example.net")))

	// Any other host is denied, including with no allowed hosts.
	require.False(t, checkOrigin(exacts, globs, newRequest("http://other.example.org")))
	require.False(t, checkOrigin(exacts, globs, newRequest("http://glob.example.net")))
	require.False(t, checkOrigin(nil, nil, newRequest("http://other.example.org")))
}

func TestAddProtocol(t *testing.T) {
	s := NewWebsocketServer(nil)

	err := s.addProtocol("custom.proto", websocket.CloseMessage, &serialize.JSONSerializer{})
	require.EqualError(t, err, fmt.Sprintf("invalid payload type: %d", websocket.CloseMessage))

	err = s.addProtocol(jsonWebsocketProtocol, websocket.TextMessage, &serialize.JSONSerializer{})
	require.EqualError(t, err, "protocol already registered: "+jsonWebsocketProtocol)

	// A new protocol is registered with its payload type and serializer.
	ser := &serialize.CBORSerializer{}
	require.NoError(t, s.addProtocol("custom.proto", websocket.BinaryMessage, ser))
	require.Equal(t, protocol{websocket.BinaryMessage, ser}, s.protocols["custom.proto"])
	err = s.addProtocol("custom.proto", websocket.TextMessage, &serialize.JSONSerializer{})
	require.EqualError(t, err, "protocol already registered: custom.proto")
}

// TestAddProtocolNegotiated checks that a client can connect with a protocol
// added by addProtocol.
func TestAddProtocolNegotiated(t *testing.T) {
	r, err := NewRouter(routerConfig, logger)
	require.NoError(t, err)
	defer r.Close()

	s := NewWebsocketServer(r)
	require.NoError(t, s.addProtocol("custom.json", websocket.TextMessage,
		&serialize.JSONSerializer{}))
	server := httptest.NewServer(s)
	defer server.Close()

	dialer := websocket.Dialer{Subprotocols: []string{"custom.json"}}
	conn, rsp, err := dialer.DialContext(context.Background(),
		"ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.Close()
	defer rsp.Body.Close()
	require.Equal(t, "custom.json", conn.Subprotocol())

	hello, err := (&serialize.JSONSerializer{}).Serialize(
		&wamp.Hello{Realm: testRealm, Details: maps.Clone(clientRoles)})
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, hello))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(msg), "[2,"), "expected WELCOME, got %s", msg)
}

// fakeWSConn is a transport.WebsocketConnection that gives ReadMessage the
// messages sent on in, and sends what is written to out.
type fakeWSConn struct {
	proto     string
	readType  int
	in        chan []byte
	out       chan fakeWSMessage
	closed    chan struct{}
	closeOnce sync.Once
}

type fakeWSMessage struct {
	messageType int
	data        []byte
}

func newFakeWSConn(proto string, readType int) *fakeWSConn {
	return &fakeWSConn{
		proto:    proto,
		readType: readType,
		in:       make(chan []byte, 1),
		out:      make(chan fakeWSMessage, 4),
		closed:   make(chan struct{}),
	}
}

func (c *fakeWSConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *fakeWSConn) WriteControl(int, []byte, time.Time) error { return nil }

func (c *fakeWSConn) WriteMessage(messageType int, data []byte) error {
	select {
	case c.out <- fakeWSMessage{messageType, bytes.Clone(data)}:
		return nil
	case <-c.closed:
		return websocket.ErrCloseSent
	}
}

func (c *fakeWSConn) ReadMessage() (int, []byte, error) {
	select {
	case data := <-c.in:
		return c.readType, data, nil
	case <-c.closed:
		return 0, nil, &websocket.CloseError{Code: websocket.CloseNormalClosure}
	}
}

func (c *fakeWSConn) SetPongHandler(func(string) error) {}
func (c *fakeWSConn) SetPingHandler(func(string) error) {}
func (c *fakeWSConn) Subprotocol() string               { return c.proto }

// TestHandleWebsocketDefaultProtocols checks that handleWebsocket uses the
// serializer and payload type of each WAMP protocol even when the server does
// not have it registered, since a websocket implementation other than gorilla
// may not reject an unregistered protocol, and closes a connection with any
// other protocol.
func TestHandleWebsocketDefaultProtocols(t *testing.T) {
	for _, tc := range []struct {
		proto       string
		payloadType int
		serializer  serialize.Serializer
	}{
		{jsonWebsocketProtocol, websocket.TextMessage, &serialize.JSONSerializer{}},
		{msgpackWebsocketProtocol, websocket.BinaryMessage, &serialize.MessagePackSerializer{}},
		{cborWebsocketProtocol, websocket.BinaryMessage, &serialize.CBORSerializer{}},
	} {
		t.Run(tc.proto, func(t *testing.T) {
			r := newTestRouter(t)
			s := NewWebsocketServer(r)
			s.protocols = map[string]protocol{} // none registered

			conn := newFakeWSConn(tc.proto, tc.payloadType)
			defer conn.Close()
			hello, err := tc.serializer.Serialize(
				&wamp.Hello{Realm: testRealm, Details: maps.Clone(clientRoles)})
			require.NoError(t, err)
			conn.in <- hello

			s.handleWebsocket(conn, nil, "test peer")

			select {
			case out := <-conn.out:
				require.Equal(t, tc.payloadType, out.messageType)
				msg, err := tc.serializer.Deserialize(out.data)
				require.NoError(t, err)
				require.IsType(t, &wamp.Welcome{}, msg)
			case <-time.After(time.Second):
				require.FailNow(t, "no WELCOME")
			}
		})
	}

	t.Run("unknown", func(t *testing.T) {
		r := newTestRouter(t)
		s := NewWebsocketServer(r)
		conn := newFakeWSConn("wamp.2.unknown", websocket.TextMessage)
		s.handleWebsocket(conn, nil, "test peer")
		select {
		case <-conn.closed:
		default:
			require.FailNow(t, "connection not closed")
		}
		require.Empty(t, conn.out, "message sent on connection")
	})
}
