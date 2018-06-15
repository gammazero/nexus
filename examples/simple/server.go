/*
Simple example nexus WAMP router that handles websockets.

*/
package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/wamp"
)

const address = "127.0.0.1:8080"

func main() {
	// Create router instance.
	routerConfig := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			&router.RealmConfig{
				URI:           wamp.URI("nexus.realm1"),
				AnonymousAuth: true,
			},
		},
	}
	nxr, err := router.NewRouter(routerConfig, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer nxr.Close()

	// Create and run server.
	closer, err := router.NewWebsocketServer(nxr).ListenAndServe(address)
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
