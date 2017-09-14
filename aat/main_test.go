package aat

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
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
)

var (
	nxr       nexus.Router
	cliLogger stdlog.StdLog
	rtrLogger stdlog.StdLog

	serverURL string
	port      int

	err error

	// Creates websocket client if true.  Otherwise, create embedded client
	// that only uses channels to communicate with router.
	websocketClient bool

	// Use msgpack serialization with websockets if true.  Otherwise, use JSON.
	msgPack bool
)

type testAuthz struct{}

func (a *testAuthz) Authorize(sess *nexus.Session, msg wamp.Message) (bool, error) {
	m, ok := msg.(*wamp.Subscribe)
	if !ok {
		if callMsg, ok := msg.(*wamp.Call); ok {
			if callMsg.Procedure == wamp.URI("need.ldap.auth") {
				// Cannot contact LDAP server
				return false, nil
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
	flag.BoolVar(&msgPack, "msgpack", false,
		"use msgpack serialization with websockets")
	flag.Parse()
	if websocketClient {
		fmt.Println("===== USING WEBSOCKET CLIENT =====")
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

	var listener *net.TCPListener
	if websocketClient {
		s := nexus.NewWebsocketServer(nxr)
		server := &http.Server{
			Handler: s,
		}

		addr := net.TCPAddr{IP: net.ParseIP("127.0.0.1")}
		listener, err = net.ListenTCP("tcp", &addr)
		if err != nil {
			cliLogger.Println("Server cannot listen:", err)
		}
		go server.Serve(listener)
		port = listener.Addr().(*net.TCPAddr).Port
		serverURL = fmt.Sprintf("ws://127.0.0.1:%d/", port)

		rtrLogger.Println("Server listening on", serverURL)
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
	if websocketClient {
		listener.Close()
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
