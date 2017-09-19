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

const address = "127.0.0.1:8000"

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

	// Create and run server.
	closer, err := nexus.NewWebsocketServer(nxr).ListenAndServe(address)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Websocket server listening on ws://%s/", address)

	// Wait for SIGINT (CTRL-c), then close server and exit.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	<-shutdown
	closer.Close()
}
