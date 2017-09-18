/*
Simple example nexus WAMP router that handles websockets.

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
			},
		},
	}
	nxr, err := nexus.NewRouter(routerConfig, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer nxr.Close()

	// Create websocket server.
	wss, err := nexus.NewWebsocketServer(nxr, "127.0.0.1:8000")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Websocket server listening on", wss.URL())

	// Run server in separate goroutine. Serve() returns when server is closed.
	go wss.Serve()

	// Wait for SIGINT (CTRL-c), then close server and exit.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	<-shutdown
	wss.Close()
}
