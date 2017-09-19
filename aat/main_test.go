package aat

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus"
	"github.com/gammazero/nexus/auth"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/wamp"
)

const (
	testRealm     = "nexus.test.realm"
	testAuthRealm = "nexus.test.auth"

	tcpAddr  = "127.0.0.1:8282"
	unixAddr = "/tmp/nexustest_sock"
)

var (
	nxr       nexus.Router
	cliLogger stdlog.StdLog
	rtrLogger stdlog.StdLog

	serverURL string
	rsNet     string
	rsAddr    string

	err error

	// Creates websocket or rawsocket client.  If not either of those, create
	// embedded client that only uses channels to communicate with router.
	websocketClient bool
	rawsocketTCP    bool
	rawsocketUnix   bool

	// Use msgpack serialization with websockets if true.  Otherwise, use JSON.
	msgPack bool

	// Use JSON serialization with rawsockets if true.  Otherwise, use msgpack.
	jsonEnc bool
)

type testAuthz struct{}

func (a *testAuthz) Authorize(sess *nexus.Session, msg wamp.Message) (bool, error) {
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
	flag.BoolVar(&websocketClient, "websocket", false,
		"use websocket to connect clients to router")
	flag.BoolVar(&rawsocketUnix, "rawsocketunix", false,
		"use Unix raw socket to connect clients to router")
	flag.BoolVar(&rawsocketTCP, "rawsockettcp", false,
		"use TCP raw socket to connect clients to router")
	flag.BoolVar(&msgPack, "msgpack", false,
		"use msgpack serialization with websockets (otherwise json)")
	flag.BoolVar(&jsonEnc, "json", false,
		"use JSON serialization with rawsockets (otherwise msgpack)")
	flag.Parse()

	if websocketClient {
		fmt.Println("===== USING WEBSOCKET CLIENT =====")
	} else if rawsocketTCP || rawsocketUnix {
		fmt.Println("===== USING RAWSOCKET CLIENT =====")
	} else {
		fmt.Println("===== USING LOCAL CLIENT =====")
	}

	// Create separate logger for client and router.
	cliLogger = log.New(os.Stdout, "CLIENT> ", log.LstdFlags)
	rtrLogger = log.New(os.Stdout, "ROUTER> ", log.LstdFlags)

	crAuth, err := auth.NewCRAuthenticator(&testCRAuthenticator{})
	if err != nil {
		panic(err)
	}

	// Create router instance.
	routerConfig := &nexus.RouterConfig{
		RealmConfigs: []*nexus.RealmConfig{
			{
				URI:           wamp.URI(testRealm),
				StrictURI:     false,
				AnonymousAuth: true,
				AllowDisclose: false,
			},
			{
				URI:           wamp.URI(testAuthRealm),
				StrictURI:     false,
				AnonymousAuth: true,
				AllowDisclose: false,
				Authenticators: map[string]auth.Authenticator{
					"testauth": crAuth,
				},
				Authorizer: &testAuthz{},
			},
		},
		//Debug: true,
	}
	nxr, err = nexus.NewRouter(routerConfig, rtrLogger)
	if err != nil {
		panic(err)
	}

	var wsCloser, tcpCloser, unixCloser io.Closer
	if websocketClient {
		wss := nexus.NewWebsocketServer(nxr)
		wsCloser, err = wss.ListenAndServe(tcpAddr)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed to start websocket server:", err)
			os.Exit(1)
		}
		serverURL = fmt.Sprintf("ws://%s/", tcpAddr)
		rtrLogger.Println("WebSocket server listening on", serverURL)
	} else if rawsocketTCP || rawsocketUnix {
		rss := nexus.NewRawSocketServer(nxr, 0, false)
		if rawsocketUnix {
			// Create Unix raw socket
			unixCloser, err = rss.ListenAndServe("unix", unixAddr)
			rtrLogger.Println("RawSocket server listening on", unixAddr)
			rsNet = "unix"
			rsAddr = unixAddr
		} else {
			// Create TCP raw socket
			tcpCloser, err = rss.ListenAndServe("tcp", tcpAddr)
			rtrLogger.Println("RawSocket server listening on", tcpAddr)
			rsNet = "tcp"
			rsAddr = tcpAddr
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed to start rawsocket server:", err)
			os.Exit(1)
		}
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
	cfg := client.ClientConfig{
		Realm: testAuthRealm,
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
	if wsCloser != nil {
		wsCloser.Close()
	} else if tcpCloser != nil {
		tcpCloser.Close()
	} else if unixCloser != nil {
		unixCloser.Close()
	}

	nxr.Close()
	os.Exit(rc)
}

func connectClientCfg(cfg client.ClientConfig) (*client.Client, error) {
	var cli *client.Client
	var err error
	if websocketClient {
		// Use larger response timeout for very slow test systems.
		cfg.ResponseTimeout = time.Second
		if msgPack {
			cli, err = client.NewWebsocketClient(
				serverURL, client.MSGPACK, nil, nil, cfg, cliLogger)
		} else {
			cli, err = client.NewWebsocketClient(
				serverURL, client.JSON, nil, nil, cfg, cliLogger)
		}
	} else if rawsocketTCP || rawsocketUnix {
		// Use larger response timeout for very slow test systems.
		cfg.ResponseTimeout = time.Second
		if jsonEnc {
			cli, err = client.NewRawSocketClient(rsNet, rsAddr, client.JSON,
				cfg, cliLogger, 0)
		} else {
			cli, err = client.NewRawSocketClient(rsNet, rsAddr, client.MSGPACK,
				cfg, cliLogger, 0)
		}
	} else {
		cli, err = client.NewLocalClient(nxr, cfg, cliLogger)
	}

	if err != nil {
		cliLogger.Println("Failed to create client:", err)
		return nil, err
	}

	//cli.SetDebug(true)

	return cli, nil
}

func connectClient() (*client.Client, error) {
	cfg := client.ClientConfig{
		Realm: testRealm,
	}
	cli, err := connectClientCfg(cfg)
	if err != nil {
		return nil, err
	}
	return cli, nil
}

func TestHandshake(t *testing.T) {
	defer leaktest.Check(t)()
	cli, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	err = cli.Close()
	if err != nil {
		t.Fatal("Failed to close client:", err)
	}
	err = cli.Close()
	if err != nil {
		t.Fatal("Failed to close client 2nd time:", err)
	}
	err = cli.Close()
	if err != nil {
		t.Fatal("Failed to close client 3rd time:", err)
	}
}
