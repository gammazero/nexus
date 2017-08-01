package aat

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/logger"
	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/server"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
)

const (
	testRealm = "nexus.test.realm"

	// Creates websocket client if true.  Otherwise, create embedded client
	// that only uses channels to communicate with router.
	websocketClient = true
)

var (
	nxr       router.Router
	cliLogger logger.Logger
	rtrLogger logger.Logger

	serverURL string
	port      int
)

func TestMain(m *testing.M) {
	const (
		autoRealm = false
		strictURI = false

		anonAuth      = true
		allowDisclose = false
	)

	// ----- Setup environment -----

	// Create separate logger for client and router.
	cliLogger = log.New(os.Stdout, "CLIENT> ", log.LstdFlags)
	rtrLogger = log.New(os.Stdout, "ROUTER> ", log.LstdFlags)
	//router.DebugEnabled = true
	router.SetLogger(rtrLogger)

	// Create router instance.
	nxr = router.NewRouter(autoRealm, strictURI)
	nxr.AddRealm(wamp.URI(testRealm), anonAuth, allowDisclose)

	var listener *net.TCPListener
	var err error
	if websocketClient {
		s := server.NewWebsocketServer(nxr)
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
			serverURL, serialize.JSON, nil, nil, time.Second, cliLogger)
		if err != nil {
			cliLogger.Println("Failed to create websocket client:", err)
			return nil, err
		}
	} else {
		cPeer, sPeer := router.LinkedPeers()
		go func() {
			if err := nxr.Attach(sPeer); err != nil {
				cliLogger.Print("Failed to attach client: ", err)
			}
		}()

		cli = client.NewClient(cPeer, 200*time.Millisecond, cliLogger)
	}
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

func TestHandshake(t *testing.T) {
	client, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client: ", err)
	}
	err = client.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client: ", err)
	}
}
