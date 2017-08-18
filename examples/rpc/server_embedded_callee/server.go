package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gammazero/nexus"
	"github.com/gammazero/nexus/client"
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

	// Create local client.
	logger := log.New(os.Stdout, "CALLEE> ", log.LstdFlags)
	callee, err := client.NewLocalClient(nxr, 0, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer callee.Close()

	// Connect callee session.
	_, err = callee.JoinRealm("nexus.examples", nil, nil)
	if err != nil {
		logger.Fatal(err)
	}

	// Register procedure "sum"
	procName := "sum"
	if err = callee.Register(procName, sum, nil); err != nil {
		logger.Fatal("Failed to register procedure:", err)
	}
	logger.Println("Registered procedure", procName, "with router")

	// Run server.
	s := nexus.NewWebsocketServer(nxr)
	server := &http.Server{
		Handler: s,
		Addr:    ":8000",
	}
	log.Println("Server listening on port 8000")
	log.Fatal(server.ListenAndServe())
}

func sum(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
	fmt.Print("Calculating sum")
	var sum int64
	for i := range args {
		n, ok := wamp.AsInt64(args[i])
		if ok {
			sum += n
		}
	}
	return &client.InvokeResult{Args: wamp.List{sum}}
}
