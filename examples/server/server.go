/*
Example nexus WAMP router that handles websockets (with/out TLS), TCP
rawsockets (with/out TLS), and unix rawsockets.

*/
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path"
	"time"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/wamp"
)

const (
	certFile = "cert.pem"
	keyFile  = "rsakey.pem"
)

func main() {
	var (
		realm = "realm1"

		netAddr  = "localhost"
		unixAddr = "/tmp/exmpl_nexus_sock"

		wsPort   = 8000
		wssPort  = 8443
		tcpPort  = 8080
		tcpsPort = 8081
	)
	flag.StringVar(&netAddr, "netaddr", netAddr, "network address to listen on")
	flag.StringVar(&unixAddr, "unixaddr", unixAddr, "unix address to listen on")
	flag.IntVar(&wsPort, "ws-port", wsPort, "websocket port")
	flag.IntVar(&wssPort, "wss-port", wssPort, "websocket TLS port")
	flag.IntVar(&tcpPort, "tcp-port", tcpPort, "TCP port")
	flag.IntVar(&tcpsPort, "tcps-port", tcpsPort, "TCP TLS port")
	flag.StringVar(&realm, "realm", realm, "realm name")
	flag.Parse()

	// Create router instance.
	routerConfig := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			&router.RealmConfig{
				URI:           wamp.URI(realm),
				AnonymousAuth: true,
				AllowDisclose: true,
			},
		},
	}
	nxr, err := router.NewRouter(routerConfig, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer nxr.Close()

	// create local (embedded) RPC callee client that provides the time in the
	// requested timezones.
	callee, err := createLocalCallee(nxr, realm)
	if err != nil {
		log.Fatal(err)
	}
	defer callee.Close()

	// Create websocket server.
	wss := router.NewWebsocketServer(nxr)
	// Enable websocket compression, which is used if clients request it.
	wss.Upgrader.EnableCompression = true
	// Configure server to send and look for client tracking cookie.
	wss.EnableTrackingCookie = true
	// Set keep-alive period to 30 seconds.
	wss.KeepAlive = 30 * time.Second

	// Create rawsocket server.
	rss := router.NewRawSocketServer(nxr, 0, 0)

	// ---- Start servers ----

	// Run websocket server.
	wsAddr := fmt.Sprintf("%s:%d", netAddr, wsPort)
	wsCloser, err := wss.ListenAndServe(wsAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer wsCloser.Close()
	log.Printf("Websocket server listening on ws://%s/", wsAddr)

	// Run TCP rawsocket server.
	tcpAddr := fmt.Sprintf("%s:%d", netAddr, tcpPort)
	tcpCloser, err := rss.ListenAndServe("tcp", tcpAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer tcpCloser.Close()
	log.Printf("RawSocket TCP server listening on tcp://%s/", tcpAddr)

	// Run unix rawsocket server.
	unixCloser, err := rss.ListenAndServe("unix", unixAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer unixCloser.Close()
	log.Printf("RawSocket unix server listening on unix://%s", unixAddr)

	// ---- Start TLS servers ----

	var certPath, keyPath string
	if _, err = os.Stat(certFile); os.IsNotExist(err) {
		certPath = path.Join("server", certFile)
		keyPath = path.Join("server", keyFile)
	} else {
		certPath = certFile
		keyPath = keyFile
	}

	// Run TLS websocket server.
	wsAddrTLS := fmt.Sprintf("%s:%d", netAddr, wssPort)
	wsTlsCloser, err := wss.ListenAndServeTLS(wsAddrTLS, nil, certPath, keyPath)
	if err != nil {
		log.Fatal(err)
	}
	defer wsTlsCloser.Close()
	log.Printf("TLS Websocket server listening on wss://%s/", wsAddrTLS)

	// Run TLS TCP rawsocket server.
	tcpAddrTLS := fmt.Sprintf("%s:%d", netAddr, tcpsPort)
	tcpTlsCloser, err := rss.ListenAndServeTLS("tcp", tcpAddrTLS, nil, certPath, keyPath)
	if err != nil {
		log.Fatal(err)
	}
	defer tcpTlsCloser.Close()
	log.Printf("TLS RawSocket TCP server listening on tcps://%s/", tcpAddrTLS)

	// Wait for SIGINT (CTRL-c), then close servers and exit.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	<-shutdown
	// Servers close at exit due to defer calls.
}

// createLocalCallee creates a local callee client that is embedded in this
// server.  This client functions as a trusted client and does not require IPC
// to communicate with the router.
//
// The purpose of this client is to demonstrate how to create a local client
// that is part of the same application running the WAMP router.
func createLocalCallee(nxr router.Router, realm string) (*client.Client, error) {
	logger := log.New(os.Stdout, "CALLEE> ", log.LstdFlags)
	cfg := client.Config{
		Realm:  realm,
		Logger: logger,
	}
	callee, err := client.ConnectLocal(nxr, cfg)
	if err != nil {
		return nil, err
	}

	// Register procedure "time"
	const timeProc = "worldtime"
	if err = callee.Register(timeProc, worldTime, nil); err != nil {
		return nil, fmt.Errorf("Failed to register %q: %s", timeProc, err)
	}
	log.Printf("Registered procedure %q with router", timeProc)

	return callee, nil
}

// worldTime is a RPC function that returns times for the specified timezones.
// This function is registered by the local callee client that is embedded in
// the server.
func worldTime(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
	now := time.Now()
	results := wamp.List{fmt.Sprintf("UTC: %s", now.UTC())}

	for i := range args {
		locName, ok := wamp.AsString(args[i])
		if !ok {
			continue
		}
		loc, err := time.LoadLocation(locName)
		if err != nil {
			results = append(results, fmt.Sprintf("%s: %s", locName, err))
			continue
		}
		results = append(results, fmt.Sprintf("%s: %s", locName, now.In(loc)))
	}

	return &client.InvokeResult{Args: results}
}
