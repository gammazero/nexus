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
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
)

const (
	testRealm     = "nexus.test.realm"
	testAuthRealm = "nexus.test.auth"

	tcpAddr  = "127.0.0.1:8282"
	unixAddr = "/tmp/nexustest_sock"

	webURL  = "wss://127.0.0.1:8282"
	tcpURL  = "tcp://127.0.0.1:8282"
	unixURL = "unix:///tmp/nexustest_sock"
)

var (
	nxr       nexus.Router
	cliLogger stdlog.StdLog
	rtrLogger stdlog.StdLog

	err error

	// sockType is set to "web", "tcp", "unix", "".  Empty means use local
	// connection to connect client to router.
	sockType string
	// serType is set to "json" or "msgpack".  Ignored if sockType is "".
	serType string
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
	flag.StringVar(&sockType, "socket", "",
		"-socket=[web, tcp, unix] or none for local (in-process)")
	flag.StringVar(&serType, "serialize", "",
		"-serialize[json, msgpack] or none for socket default")
	flag.Parse()

	if serType != "" && serType != "json" && serType != "msgpack" {
		fmt.Fprintln(os.Stderr, "invalid serialize value")
		flag.Usage()
		os.Exit(1)
	}

	var sockDesc string
	switch sockType {
	case "web":
		sockDesc = "WEBSOCKETS"
		if serType == "" {
			serType = "json"
		}
	case "tcp":
		sockDesc = "TCP RAWSOCKETS"
		if serType == "" {
			serType = "msgpack"
		}
	case "unix":
		sockDesc = "UNIX RAWSOCKETS"
		if serType == "" {
			serType = "msgpack"
		}
	case "":
		sockDesc = "LOCAL CONNECTIONS"
		serType = ""
	default:
		fmt.Fprintln(os.Stderr, "invalid socket value")
		flag.Usage()
		os.Exit(1)
	}
	if serType != "" {
		sockDesc = fmt.Sprint(sockDesc, " with ", serType, " serialization")
	}
	fmt.Println("===== CLIENT USING", sockDesc, "=====")

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

	var closer io.Closer
	switch sockType {
	case "web":
		wss := nexus.NewWebsocketServer(nxr)
		closer, err = wss.ListenAndServe(tcpAddr)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed to start websocket server:", err)
			os.Exit(1)
		}
		rtrLogger.Println("WebSocket server listening on", webURL)
	case "tcp", "unix":
		// Createraw socket server.
		rss := nexus.NewRawSocketServer(nxr, 0, 0)
		var rsAddr string
		if sockType == "unix" {
			closer, err = rss.ListenAndServe(sockType, unixAddr)
		} else {
			closer, err = rss.ListenAndServe(sockType, tcpAddr)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed to start rawsocket server:", err)
			os.Exit(1)
		}
		rtrLogger.Println("RawSocket server listening on", rsAddr)
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
	nxr.Close()
	os.Exit(rc)
}

func connectClientCfg(cfg client.ClientConfig) (*client.Client, error) {
	var cli *client.Client
	var err error

	switch serType {
	case "json":
		cfg.Serialization = serialize.JSON
	case "msgpack":
		cfg.Serialization = serialize.MSGPACK
	}
	cfg.Logger = cliLogger

	switch sockType {
	case "web":
		cli, err = client.ConnectNet(fmt.Sprintf("ws://%s/", tcpAddr), cfg)
	case "tcp":
		cli, err = client.ConnectNet(fmt.Sprintf("tcp://%s/", tcpAddr), cfg)
	case "unix":
		cli, err = client.ConnectNet(fmt.Sprintf("unix://%s/", unixAddr), cfg)
	default:
		cli, err = client.ConnectLocal(nxr, cfg)

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
