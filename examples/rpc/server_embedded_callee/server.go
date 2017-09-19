package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/gammazero/nexus"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const wsAddr = "127.0.0.1:8000"

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

	logger := log.New(os.Stdout, "CALLEE> ", log.LstdFlags)
	cfg := client.ClientConfig{
		Realm: "nexus.examples",
	}
	// Create local callee client.
	callee, err := client.NewLocalClient(nxr, cfg, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer callee.Close()

	// Register procedure "sum"
	procName := "sum"
	if err = callee.Register(procName, sum, nil); err != nil {
		logger.Fatal("Failed to register procedure:", err)
	}
	logger.Println("Registered procedure", procName, "with router")

	// Create and run websocket server.
	closer, err := nexus.NewWebsocketServer(nxr).ListenAndServe(wsAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Websocket server listening on ws://%s/", wsAddr)

	// Wait for SIGINT (CTRL-c), then close server and exit.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	<-shutdown
	closer.Close()
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
