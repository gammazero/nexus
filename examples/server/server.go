package main

import (
	"log"
	"net/http"

	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/server"
	"github.com/gammazero/nexus/wamp"
)

func main() {
	// Create router instance.
	routerConfig := &router.RouterConfig{
		RealmConfigs: []*router.RealmConfig{
			&router.RealmConfig{
				URI:           wamp.URI("nexus.examples"),
				AnonymousAuth: true,
				AllowDisclose: true,
			},
		},
	}
	nxr, err := router.NewRouter(routerConfig)
	if err != nil {
		log.Fatal(err)
	}
	defer nxr.Close()

	// Run server.
	s := server.NewWebsocketServer(nxr)
	server := &http.Server{
		Handler: s,
		Addr:    ":8000",
	}
	log.Println("Server listening on port 8000")
	log.Fatal(server.ListenAndServe())
}
