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

	// Run websocket server.
	wss, err := nexus.NewWebsocketServer(nxr, "127.0.0.1:8000")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Websocket server listening on", wss.URL())

	// Run rawsocket TCP server.
	rssTCP, err := nexus.NewRawSocketServer(nxr, "tcp", "127.0.0.1:8001", 0)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("RawSocet TCP server listening on", rssTCP.Addr())

	// Run rawsocket unix server.
	rssUnix, err := nexus.NewRawSocketServer(nxr, "unix", "/tmp/exmpl_nexus_sock", 0)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("RawSocket unix server listening on", rssUnix.Addr())

	// Create a signal handler that signals the shutdown channel.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)

	// Run the servers in separate goroutines, since each Serve() method does
	// not return until each server is closed.
	go wss.Serve()
	go rssTCP.Serve(true)
	go rssUnix.Serve(false)

	// Wait for SIGINT (CTRL-c), then close servers and exit.
	<-shutdown
	wss.Close()
	rssTCP.Close()
	rssUnix.Close()
}
