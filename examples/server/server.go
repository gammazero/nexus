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

	"github.com/gammazero/nexus"
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
	routerConfig := &nexus.RouterConfig{
		RealmConfigs: []*nexus.RealmConfig{
			&nexus.RealmConfig{
				URI:           wamp.URI("nexus.examples"),
				AnonymousAuth: true,
				AllowDisclose: true,
			},
		},
	}
	nxr, err := nexus.NewRouter(routerConfig, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer nxr.Close()

	// Create websocket and rawsocket servers.
	wss := nexus.NewWebsocketServer(nxr)
	rss := nexus.NewRawSocketServer(nxr, 0, 0)

	// ---- Start servers ----

	// Run websocket server.
	wsCloser, err := wss.ListenAndServe(wsAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer wsCloser.Close()
	log.Printf("Websocket server listening on ws://%s/", wsAddr)

	// Run TCP rawsocket server.
	tcpCloser, err := rss.ListenAndServe("tcp", tcpAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer tcpCloser.Close()
	log.Println("RawSocket TCP server listening on", tcpAddr)

	// Run unix rawsocket server.
	unixCloser, err := rss.ListenAndServe("unix", unixAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer unixCloser.Close()
	log.Println("RawSocket unix server listening on", unixAddr)

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
	log.Printf("TLS Websocket server listening on ws://%s/", wsAddrTLS)

	// Run TLS TCP rawsocket server.
	tcpTlsCloser, err := rss.ListenAndServeTLS("tcp", tcpAddrTLS, nil, certPath, keyPath)
	if err != nil {
		log.Fatal(err)
	}
	defer tcpTlsCloser.Close()
	log.Println("TLS RawSocket TCP server listening on", tcpAddrTLS)

	// Wait for SIGINT (CTRL-c), then close servers and exit.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	<-shutdown
	// Servers close at exit due to defer calls.
}
