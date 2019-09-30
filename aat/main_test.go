package aat

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http/cookiejar"
	"os"
	"path"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

const (
	testRealm     = "nexus.test.realm"
	testAuthRealm = "nexus.test.auth"

	tcpAddr  = "127.0.0.1:8282"
	unixAddr = "/tmp/nexustest_sock"

	certFile = "cert.pem"
	keyFile  = "rsakey.pem"

	keepAliveInterval = time.Second * 5
)

var (
	nxr       router.Router
	cliLogger stdlog.StdLog
	rtrLogger stdlog.StdLog

	err error

	// scheme determines the transport and use of TLS.  Value must be one of
	// the following: "http", "https", "ws", "wss", "tcp", "tcps", "unix", "".
	// Empty indicates direct (in proc) connection to router.  TLS is not
	// available for "" or "unix".
	scheme string

	// serType is set to "json" or "msgpack".  Ignored if sockType is "".
	serType string

	// compress enables compression on both client and server config
	compress bool

	// size of server's per-client outbound message queue
	outQueueSize int
)

type testAuthz struct{}

func (a *testAuthz) Authorize(sess *wamp.Session, msg wamp.Message) (bool, error) {
	m, ok := msg.(*wamp.Subscribe)
	if !ok {
		if callMsg, ok := msg.(*wamp.Call); ok {
			if callMsg.Procedure == wamp.URI("need.ldap.auth") {
				return false, errors.New("Cannot contact LDAP server")
			}
		}
		return true, nil
	}
	if m.Topic == "nexus.interceptor" {
		m.Topic = "nexus.interceptor.foobar.baz"
	}
	wamp.SetOption(sess.Details, "foobar", "baz")
	return true, nil
}

func TestMain(m *testing.M) {
	// ----- Setup environment -----
	flag.StringVar(&scheme, "scheme", "",
		"-scheme=[http, https, ws, wss, tcp, tcps, unix] or none for local (in-process)")
	flag.StringVar(&serType, "serialize", "",
		"-serialize[json, msgpack, cbor] default is json")
	flag.BoolVar(&compress, "compress", false, "enable compression")
	flag.IntVar(&outQueueSize, "qsize", 0, "server's per-client outbound queue size")
	flag.Parse()

	if serType != "" && serType != "json" && serType != "msgpack" && serType != "cbor" {
		fmt.Fprintln(os.Stderr, "invalid serialize value")
		flag.Usage()
		os.Exit(1)
	}

	var certPath, keyPath string
	if scheme == "https" || scheme == "wss" || scheme == "tcps" {
		if _, err = os.Stat(certFile); os.IsNotExist(err) {
			certPath = path.Join("aat", certFile)
			keyPath = path.Join("aat", keyFile)
		} else {
			certPath = certFile
			keyPath = keyFile
		}
	}

	// Create separate logger for client and router.
	cliLogger = log.New(os.Stdout, "CLIENT> ", log.LstdFlags)
	rtrLogger = log.New(os.Stdout, "ROUTER> ", log.LstdFlags)

	sks := &serverKeyStore{
		provider: "UserDB",
	}
	crAuth := auth.NewCRAuthenticator(sks, time.Second)

	// Create router instance.
	routerConfig := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			{
				URI:           wamp.URI(testRealm),
				StrictURI:     false,
				AnonymousAuth: true,
				AllowDisclose: false,

				EnableMetaKill:   true,
				EnableMetaModify: true,
			},
			{
				URI:               wamp.URI(testAuthRealm),
				StrictURI:         false,
				AnonymousAuth:     true,
				AllowDisclose:     false,
				Authenticators:    []auth.Authenticator{crAuth},
				Authorizer:        &testAuthz{},
				RequireLocalAuth:  true,
				RequireLocalAuthz: true,

				MetaStrict:                true,
				MetaIncludeSessionDetails: []string{"foobar"},

				EnableMetaKill:   true,
				EnableMetaModify: true,
			},
		},
		//Debug: true,
	}
	nxr, err = router.NewRouter(routerConfig, rtrLogger)
	if err != nil {
		panic(err)
	}
	defer nxr.Close()

	var closer io.Closer
	var sockDesc string
	addr := tcpAddr
	switch scheme {
	case "":
		serType = ""
		sockDesc = "LOCAL CONNECTIONS"
	case "http", "ws":
		s := router.NewWebsocketServer(nxr)
		sockDesc = "WEBSOCKETS"
		// Set optional websocket config.
		if compress {
			s.Upgrader.EnableCompression = true
			sockDesc += " + compression"
		}
		s.EnableTrackingCookie = true
		s.EnableRequestCapture = true
		s.KeepAlive = keepAliveInterval
		s.OutQueueSize = outQueueSize
		closer, err = s.ListenAndServe(tcpAddr)
	case "https", "wss":
		s := router.NewWebsocketServer(nxr)
		sockDesc = "WEBSOCKETS + TLS"
		if compress {
			s.Upgrader.EnableCompression = true
			sockDesc += " + compression"
		}
		s.EnableTrackingCookie = true
		s.EnableRequestCapture = true
		s.KeepAlive = keepAliveInterval
		s.OutQueueSize = outQueueSize
		closer, err = s.ListenAndServeTLS(tcpAddr, nil, certPath, keyPath)
	case "tcp":
		s := router.NewRawSocketServer(nxr)
		s.KeepAlive = keepAliveInterval
		closer, err = s.ListenAndServe(scheme, tcpAddr)
		sockDesc = "TCP RAWSOCKETS"
	case "tcps":
		s := router.NewRawSocketServer(nxr)
		s.KeepAlive = keepAliveInterval
		s.OutQueueSize = outQueueSize
		closer, err = s.ListenAndServeTLS("tcp", tcpAddr, nil, certPath, keyPath)
		sockDesc = "TCP RAWSOCKETS + TLS"
	case "unix":
		os.Remove(unixAddr)
		s := router.NewRawSocketServer(nxr)
		s.KeepAlive = keepAliveInterval
		s.OutQueueSize = outQueueSize
		closer, err = s.ListenAndServe(scheme, unixAddr)
		addr = unixAddr
		sockDesc = "UNIX RAWSOCKETS"
	default:
		fmt.Fprintln(os.Stderr, "invalid scheme:", scheme)
		flag.Usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to start websocket server:", err)
		os.Exit(1)
	}
	if closer != nil {
		rtrLogger.Printf("Server listening on %s://%s compression=%t", scheme, addr, compress)
	}
	if serType != "" {
		sockDesc = fmt.Sprint(sockDesc, " with ", serType, " serialization")
	}
	fmt.Println("===== CLIENT USING", sockDesc, "=====")
	if compress {
		fmt.Println("Compression enabled")
	}
	// Connect and disconnect so that router is started before running tests.
	// Otherwise, goroutine leak detection will think the router goroutines
	// have leaked if that are not already running.
	cli, err := connectClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to connect client:", err)
		os.Exit(1)
	}
	err = cli.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to disconnect client:", err)
		os.Exit(1)
	}
	cfg := client.Config{
		Realm:           testAuthRealm,
		ResponseTimeout: time.Second,
	}
	cli, err = connectClientCfg(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to connect client:", err)
		os.Exit(1)
	}
	err = cli.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to disconnect client:", err)
		os.Exit(1)
	}

	// Run tests.
	rc := m.Run()

	// Shutdown router and clienup environment.
	if closer != nil {
		closer.Close()
	}
	os.Exit(rc)
}

func connectClientCfg(cfg client.Config) (*client.Client, error) {
	var cli *client.Client
	var err error

	switch serType {
	case "json":
		cfg.Serialization = serialize.JSON
	case "msgpack":
		cfg.Serialization = serialize.MSGPACK
	case "cbor":
		cfg.Serialization = serialize.CBOR
	}
	cfg.Logger = cliLogger

	if compress {
		cfg.WsCfg.EnableCompression = true
	}

	var addr string
	switch scheme {
	case "http", "ws", "tcp":
		addr = fmt.Sprintf("%s://%s/", scheme, tcpAddr)
		cli, err = client.ConnectNet(context.Background(), addr, cfg)
	case "https", "wss", "tcps":
		// If TLS requested, set up TLS configuration to skip verification.
		cfg.TlsCfg = &tls.Config{
			InsecureSkipVerify: true,
		}
		addr = fmt.Sprintf("%s://%s/", scheme, tcpAddr)
		cli, err = client.ConnectNet(context.Background(), addr, cfg)
	case "unix":
		addr = fmt.Sprintf("%s://%s", scheme, unixAddr)
		cli, err = client.ConnectNet(context.Background(), addr, cfg)
	default:
		cli, err = client.ConnectLocal(nxr, cfg)
	}
	if err != nil {
		cliLogger.Println("Failed to create client:", err)
		return nil, err
	}

	if cfg.WsCfg.Jar != nil {
		switch scheme {
		case "http", "https", "ws", "wss":
			// OK, websocket scheme.
		default:
			// Programming error in test.
			panic("CookieJar provided for non-websocket client")
		}

		cookieURL, err := client.CookieURL(addr)
		if err != nil {
			return nil, err
		}
		cookies := cfg.WsCfg.Jar.Cookies(cookieURL)
		cliLogger.Println("Client received cookies from router:", cookies)
		var found bool
		for i := range cookies {
			if cookies[i].Name == "nexus-wamp-cookie" {
				found = true
				break
			}
		}
		if !found {
			cli.Close()
			err = errors.New("did not get expected cookie from router")
			cliLogger.Println(err)
			return nil, err
		}
	}

	//cli.SetDebug(true)

	return cli, nil
}

func connectClient() (*client.Client, error) {
	cfg := client.Config{
		Realm:           testRealm,
		ResponseTimeout: time.Second,
	}
	cli, err := connectClientCfg(cfg)
	if err != nil {
		return nil, err
	}
	return cli, nil
}

func TestHandshake(t *testing.T) {
	defer leaktest.Check(t)()

	cfg := client.Config{
		Realm:           testRealm,
		ResponseTimeout: time.Second,
	}

	switch scheme {
	case "http", "https", "ws", "wss":
		jar, err := cookiejar.New(nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		cfg.WsCfg.Jar = jar
	}

	cli, err := connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	err = cli.Close()
	if err != nil {
		t.Fatal("Failed to close client:", err)
	}
	err = cli.Close()
	if err == nil {
		t.Fatal("Expected error if client already closed")
	}
}
