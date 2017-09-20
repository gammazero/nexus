/*
Example nexus WAMP router that handles websockets, TCP rawsockets, and unix
rawsockets.

*/
package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/gammazero/nexus"
	"github.com/gammazero/nexus/wamp"
)

const (
	wsAddr   = "127.0.0.1:8000"
	tcpAddr  = "127.0.0.1:8001"
	unixAddr = "/tmp/exmpl_nexus_sock"
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

	// Run websocket server.
	wsCloser, err := wss.ListenAndServe(wsAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Websocket server listening on ws://%s/", wsAddr)

	// Run TCP rawsocket server.
	tcpCloser, err := rss.ListenAndServe("tcp", tcpAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("RawSocet TCP server listening on", tcpAddr)

	// Run unix rawsocket server.
	unixCloser, err := rss.ListenAndServe("unix", unixAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("RawSocket unix server listening on", unixAddr)

	// Wait for SIGINT (CTRL-c), then close servers and exit.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	<-shutdown
	wsCloser.Close()
	tcpCloser.Close()
	unixCloser.Close()
}
