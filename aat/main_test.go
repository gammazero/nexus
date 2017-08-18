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

	"github.com/gammazero/nexus"
	"github.com/gammazero/nexus/auth"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/wamp"
)

const (
	testRealm = "nexus.test.realm"
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
)

func TestMain(m *testing.M) {
	// ----- Setup environment -----
	flag.BoolVar(&websocketClient, "websocket", false,
		"use websocket to connect clients to router")
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
			&nexus.RealmConfig{
				URI:           wamp.URI(testRealm),
				StrictURI:     false,
				AnonymousAuth: true,
				AllowDisclose: false,
			},
			&nexus.RealmConfig{
				URI:           wamp.URI("nexus.test.auth"),
				StrictURI:     false,
				AnonymousAuth: true,
				AllowDisclose: false,
				Authenticators: map[string]auth.Authenticator{
					"testauth": crAuth,
				},
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

	// Run tests.
	rc := m.Run()

	// Shutdown router and clienup environment.
	if websocketClient {
		listener.Close()
	}
	nxr.Close()
	os.Exit(rc)
}

func connectClientNoJoin() (*client.Client, error) {
	var cli *client.Client
	var err error
	if websocketClient {
		cli, err = client.NewWebsocketClient(
			serverURL, client.JSON, nil, nil, time.Second, cliLogger)
	} else {
		cli, err = client.NewLocalClient(nxr, 200*time.Millisecond, cliLogger)
	}

	if err != nil {
		cliLogger.Println("Failed to create client:", err)
		return nil, err
	}

	//cli.SetDebug(true)

	return cli, nil
}

func connectClient() (*client.Client, error) {
	cli, err := connectClientNoJoin()
	if err != nil {
		return nil, err
	}
	_, err = cli.JoinRealm(testRealm, nil, nil)
	if err != nil {
		return nil, err
	}
	return cli, nil
}

func connectClientDetails(details wamp.Dict) (*client.Client, error) {
	cli, err := connectClientNoJoin()
	if err != nil {
		return nil, err
	}
	_, err = cli.JoinRealm(testRealm, details, nil)
	if err != nil {
		return nil, err
	}
	return cli, nil
}

func TestHandshake(t *testing.T) {
	cli, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	err = cli.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}
