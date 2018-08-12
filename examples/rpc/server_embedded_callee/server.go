package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/wamp"
)

const wsAddr = "127.0.0.1:8000"

func main() {
	// Create router instance.
	routerConfig := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			&router.RealmConfig{
				URI:           wamp.URI("nexus.examples"),
				AnonymousAuth: true,
				AllowDisclose: true,
			},
		},
	}
	nxr, err := router.NewRouter(routerConfig, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer nxr.Close()

	logger := log.New(os.Stdout, "CALLEE> ", log.LstdFlags)
	cfg := client.Config{
		Realm:  "nexus.examples",
		Logger: logger,
	}
	// Create local callee client.
	callee, err := client.ConnectLocal(nxr, cfg)
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

	procName = "div"
	if err = callee.Register(procName, div, nil); err != nil {
		logger.Fatal("Failed to register procedure:", err)
	}
	logger.Println("Registered procedure", procName, "with router")

	// Create and run websocket server.
	closer, err := router.NewWebsocketServer(nxr).ListenAndServe(wsAddr)
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
	fmt.Println("Calculating sum")
	var sum int64
	for i := range args {
		n, ok := wamp.AsInt64(args[i])
		if ok {
			sum += n
		}
	}
	return &client.InvokeResult{Args: wamp.List{sum}}
}

func div(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
	fmt.Println("Calculating div")
	if len(args)!=2 {
		return &client.InvokeResult{Err:"Not enough arguments"}
	}
	a, ok1 := wamp.AsInt64(args[0])
	b, ok2 := wamp.AsInt64(args[1])
	if ok1 && ok2 {
		if b== 0 {
			return &client.InvokeResult{Err:"Can not divide by 0"}
		}
		return &client.InvokeResult{Args: wamp.List{float64(a)/float64(b)}}
	}

	return &client.InvokeResult{Err:"Invalid argument type"}
}
