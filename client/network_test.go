package client_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// listenRawSocket starts a rawsocket server for r, with TLS if tlscfg is not
// nil, and returns its address.
func listenRawSocket(t *testing.T, r router.Router, network, address string, tlscfg *tls.Config) string {
	t.Helper()
	s := router.NewRawSocketServer(r)
	var closer io.Closer
	var err error
	if tlscfg == nil {
		closer, err = s.ListenAndServe(network, address)
	} else {
		closer, err = s.ListenAndServeTLS(network, address, tlscfg, "", "")
	}
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })
	return closer.(net.Listener).Addr().String()
}

// shortSockDir returns a directory for Unix sockets with a path short enough
// for the 104-byte sun_path limit on macOS, which t.TempDir() can exceed.
func shortSockDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "nexus-test-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// connectContext returns a context that ends connecting if it hangs, as a
// rawsocket handshake does when only one side uses TLS.
func connectContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestConnectNet(t *testing.T) {
	r := getTestRouter(t, newTestRealmConfig(testRealm))

	// Websocket servers that record the subprotocols each client requests.
	wsServer := router.NewWebsocketServer(r)
	wsServer.EnableTrackingCookie = true
	subprotocols := make(chan string, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		subprotocols <- req.Header.Get("Sec-WebSocket-Protocol")
		wsServer.ServeHTTP(w, req)
	})
	ws := httptest.NewServer(handler)
	t.Cleanup(ws.Close)
	wss := httptest.NewTLSServer(handler)
	t.Cleanup(wss.Close)
	wsAddr := ws.Listener.Addr().String()
	wssAddr := wss.Listener.Addr().String()

	// Rawsocket servers, using the same certificate as the websocket server.
	serverTLS := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: wss.TLS.Certificates,
	}
	tcpAddr := listenRawSocket(t, r, "tcp", "127.0.0.1:0", nil)
	tcpsAddr := listenRawSocket(t, r, "tcp", "127.0.0.1:0", serverTLS)
	sockDir := shortSockDir(t)
	require.NoError(t, os.Mkdir(filepath.Join(sockDir, "sub"), 0o700))
	sockPath := filepath.Join(sockDir, "sub", "wamp.sock")
	listenRawSocket(t, r, "unix", sockPath, nil)

	roots := x509.NewCertPool()
	roots.AddCert(wss.Certificate())
	clientTLS := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}

	testCases := []struct {
		name          string
		url           string
		serialization serialize.Serialization
		tls           bool
		// subprotocol is the websocket subprotocol the client requests, or
		// empty if it does not connect with websocket.
		subprotocol string
		// chdir is the directory to connect from, for a relative path.
		chdir string
	}{
		{name: "ws", url: "ws://" + wsAddr + "/", serialization: client.JSON,
			subprotocol: "wamp.2.json"},
		{name: "http", url: "http://" + wsAddr + "/", serialization: client.MSGPACK,
			subprotocol: "wamp.2.msgpack"},
		{name: "wss", url: "wss://" + wssAddr + "/", serialization: client.CBOR,
			tls: true, subprotocol: "wamp.2.cbor"},
		{name: "https", url: "https://" + wssAddr + "/", serialization: client.JSON,
			tls: true, subprotocol: "wamp.2.json"},
		{name: "tcp", url: "tcp://" + tcpAddr, serialization: client.MSGPACK},
		{name: "tcp4", url: "tcp4://" + tcpAddr, serialization: client.CBOR},
		{name: "tcp with TLS", url: "tcp://" + tcpsAddr, serialization: client.JSON,
			tls: true},
		{name: "tcps", url: "tcps://" + tcpsAddr, serialization: client.MSGPACK,
			tls: true},
		{name: "tcp4s", url: "tcp4s://" + tcpsAddr, serialization: client.CBOR,
			tls: true},
		{name: "unix", url: "unix://" + sockPath, serialization: client.JSON},
		{name: "unix relative", url: "unix://sub/wamp.sock",
			serialization: client.MSGPACK, chdir: sockDir},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.chdir != "" {
				t.Chdir(tc.chdir)
			}
			jar, err := cookiejar.New(nil)
			require.NoError(t, err)
			cfg := client.Config{
				Realm:           testRealm,
				Serialization:   tc.serialization,
				ResponseTimeout: time.Second,
				Logger:          logger,
			}
			cfg.WsCfg.Jar = jar
			if tc.tls {
				cfg.TlsCfg = clientTLS
			}
			cl, err := client.ConnectNet(connectContext(t), tc.url, cfg)
			require.NoError(t, err)
			defer cl.Close()

			result, err := cl.Call(t.Context(),
				string(wamp.MetaProcSessionCount), nil, nil, nil, nil)
			require.NoError(t, err)
			count, _ := wamp.AsInt64(result.Arguments[0])
			require.GreaterOrEqual(t, count, int64(1))

			if tc.subprotocol == "" {
				return
			}
			require.Equal(t, tc.subprotocol, <-subprotocols)
			// The websocket config is used: the jar has the router's cookie.
			u, err := client.CookieURL(tc.url)
			require.NoError(t, err)
			cookies := jar.Cookies(u)
			require.Len(t, cookies, 1)
			require.Equal(t, "nexus-wamp-cookie", cookies[0].Name)
		})
	}

	// Without a tls.Config, tcps uses TLS with the system roots, which do not
	// have the test certificate.
	_, err := client.ConnectNet(connectContext(t), "tcps://"+tcpsAddr,
		client.Config{Realm: testRealm, Logger: logger})
	var verifyErr *tls.CertificateVerificationError
	require.ErrorAs(t, err, &verifyErr)

	_, err = client.ConnectNet(t.Context(), "unix://"+sockPath,
		client.Config{Realm: testRealm, Logger: logger, TlsCfg: clientTLS})
	require.EqualError(t, err, "tls not supported for unix")

	_, err = client.ConnectNet(t.Context(), "ftp://"+tcpAddr,
		client.Config{Realm: testRealm, Logger: logger})
	require.EqualError(t, err, "invalid url: ftp://"+tcpAddr)

	_, err = client.ConnectNet(t.Context(), "%zz", client.Config{Realm: testRealm})
	var urlErr *url.Error
	require.ErrorAs(t, err, &urlErr)
}

// TestConnectNetRawSocketConfig checks that ConnectNet makes the rawsocket
// handshake with the serialization and receive limit in Config.
func TestConnectNetRawSocketConfig(t *testing.T) {
	sockPath := filepath.Join(shortSockDir(t), "wamp.sock")
	for _, network := range []string{"tcp", "unix"} {
		address := "127.0.0.1:0"
		if network == "unix" {
			address = sockPath
		}
		l, err := net.Listen(network, address)
		require.NoError(t, err)
		defer l.Close()

		// Fake router that reads the client's handshake and closes.
		handshake := make(chan []byte, 1)
		go func() {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			buf := make([]byte, 4)
			_, _ = io.ReadFull(conn, buf)
			handshake <- buf
		}()

		_, err = client.ConnectNet(connectContext(t),
			network+"://"+l.Addr().String(), client.Config{
				Realm:         testRealm,
				Serialization: client.MSGPACK,
				RecvLimit:     1000,
				Logger:        logger,
			})
		require.Error(t, err, network)
		// 0x7f is the rawsocket magic number. 0x12 is the maximum message
		// length 2^(9+1), which is at least 1000, and the msgpack serializer.
		require.Equal(t, []byte{0x7f, 0x12, 0, 0}, <-handshake, network)
	}
}

func TestCookieURL(t *testing.T) {
	testCases := []struct {
		routerURL string
		want      string
	}{
		{"ws://localhost:8080/ws", "http://localhost:8080/ws"},
		{"wss://user@example.com:8443/ws?a=1#f", "https://user@example.com:8443/ws?a=1#f"},
		{"http://localhost:8080/ws", "http://localhost:8080/ws"},
		{"https://example.com/ws", "https://example.com/ws"},
	}
	for _, tc := range testCases {
		u, err := client.CookieURL(tc.routerURL)
		require.NoError(t, err, tc.routerURL)
		require.Equal(t, tc.want, u.String())
	}

	_, err := client.CookieURL("tcp://localhost:8080")
	require.EqualError(t, err, "scheme not valid for websocket: tcp")
	_, err = client.CookieURL("ws://[::1")
	var urlErr *url.Error
	require.ErrorAs(t, err, &urlErr)
	_, err = client.CookieURL("%zz")
	require.ErrorAs(t, err, &urlErr)

	// The URL gets the cookies that a jar has for the router's HTTP URL.
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	httpURL, err := url.Parse("https://example.com/ws")
	require.NoError(t, err)
	jar.SetCookies(httpURL, []*http.Cookie{{Name: "session", Value: "abc"}})
	u, err := client.CookieURL("wss://example.com/ws")
	require.NoError(t, err)
	cookies := jar.Cookies(u)
	require.Len(t, cookies, 1)
	require.Equal(t, "abc", cookies[0].Value)
}
