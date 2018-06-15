/*
Example nexus WAMP router that handles websockets (with/out TLS), TCP
rawsockets (with/out TLS), and unix rawsockets.

*/
package main

import (
	"log"
	"os"
	"os/signal"
	"path"
	"time"

	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/wamp"
)

const (
	wsAddr     = "127.0.0.1:8000"
	tcpAddr    = "127.0.0.1:8001"
	wsAddrTLS  = "127.0.0.1:8100"
	tcpAddrTLS = "127.0.0.1:8101"
	unixAddr   = "/tmp/exmpl_nexus_sock"

	certFile = "cert.pem"
	keyFile  = "rsakey.pem"
)

func main() {
	// Create router instance.
	routerConfig := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			&router.RealmConfig{
				URI:           wamp.URI("nexus.examples"),
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
	wsCloser, err := wss.ListenAndServe(wsAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer wsCloser.Close()
	log.Printf("Websocket server listening on http://%s/", wsAddr)

	// Run TCP rawsocket server.
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
	wsTlsCloser, err := wss.ListenAndServeTLS(wsAddrTLS, nil, certPath, keyPath)
	if err != nil {
		log.Fatal(err)
	}
	defer wsTlsCloser.Close()
	log.Printf("TLS Websocket server listening on https://%s/", wsAddrTLS)

	// Run TLS TCP rawsocket server.
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
