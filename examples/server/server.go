package main

import (
	"log"
	"net/http"

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

	// Run server.
	s := nexus.NewWebsocketServer(nxr)
	server := &http.Server{
		Handler: s,
		Addr:    ":8000",
	}
	log.Println("Server listening on port 8000")
	log.Fatal(server.ListenAndServe())
}
