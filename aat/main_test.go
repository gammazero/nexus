package aat

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/router/auth"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
)

const (
	testRealm     = "nexus.test.realm"
	testAuthRealm = "nexus.test.auth"

	tcpAddr  = "127.0.0.1:8282"
	unixAddr = "/tmp/nexustest_sock"

	certFile = "cert.pem"
	keyFile  = "rsakey.pem"
)

var (
	nxr       router.Router
	cliLogger stdlog.StdLog
	rtrLogger stdlog.StdLog

	err error

	// scheme determines the transport and use of TLS.  Value must be one of
	// the following: "ws", "wss", "tcp", "tcps", "unix", "".
	// Empty indicates direct (in proc) connection to router.  TLS is not
	// available for "" or "unix".
	scheme string

	// serType is set to "json" or "msgpack".  Ignored if sockType is "".
	serType string
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
		"-scheme=[ws, wss, tcp, tcps, unix] or none for local (in-process)")
	flag.StringVar(&serType, "serialize", "",
		"-serialize[json, msgpack] default is json")
	flag.Parse()

	if serType != "" && serType != "json" && serType != "msgpack" {
		fmt.Fprintln(os.Stderr, "invalid serialize value")
		flag.Usage()
		os.Exit(1)
	}

	var certPath, keyPath string
	if scheme == "wss" || scheme == "tcps" {
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

	crAuth, err := auth.NewCRAuthenticator(&testCRAuthenticator{})
	if err != nil {
		panic(err)
	}

	// Create router instance.
	routerConfig := &router.RouterConfig{
		RealmConfigs: []*router.RealmConfig{
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
	case "ws":
		s := router.NewWebsocketServer(nxr)
		closer, err = s.ListenAndServe(tcpAddr)
		sockDesc = "WEBSOCKETS"
	case "wss":
		s := router.NewWebsocketServer(nxr)
		closer, err = s.ListenAndServeTLS(tcpAddr, nil, certPath, keyPath)
		sockDesc = "WEBSOCKETS + TLS"
	case "tcp":
		s := router.NewRawSocketServer(nxr, 0, 0)
		closer, err = s.ListenAndServe(scheme, tcpAddr)
		sockDesc = "TCP RAWSOCKETS"
	case "tcps":
		s := router.NewRawSocketServer(nxr, 0, 0)
		closer, err = s.ListenAndServeTLS("tcp", tcpAddr, nil, certPath, keyPath)
		sockDesc = "TCP RAWSOCKETS + TLS"
	case "unix":
		s := router.NewRawSocketServer(nxr, 0, 0)
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
		rtrLogger.Printf("Server listening on %s://%s", scheme, addr)
	}
	if serType != "" {
		sockDesc = fmt.Sprint(sockDesc, " with ", serType, " serialization")
	}
	fmt.Println("===== CLIENT USING", sockDesc, "=====")

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

	switch scheme {
	case "ws", "tcp":
		addr := fmt.Sprintf("%s://%s/", scheme, tcpAddr)
		cli, err = client.ConnectNet(addr, cfg)
	case "wss", "tcps":
		// If TLS requested, set up TLS configuration to skip verification.
		cfg.TlsCfg = &tls.Config{
			InsecureSkipVerify: true,
		}
		addr := fmt.Sprintf("%s://%s/", scheme, tcpAddr)
		cli, err = client.ConnectNet(addr, cfg)
	case "unix":
		addr := fmt.Sprintf("%s://%s/", scheme, unixAddr)
		cli, err = client.ConnectNet(addr, cfg)
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
