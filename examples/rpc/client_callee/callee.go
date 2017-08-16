package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

func main() {
	logger := log.New(os.Stdout, "CALLEE> ", log.LstdFlags)
	callee, err := client.NewWebsocketClient(
		"ws://localhost:8000/", client.JSON, nil, nil, 0, logger)
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
	fmt.Println("Registered procedure", procName, "with router")

	// Wait for CTRL-c while handling remote procedure calls.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan

	if err = callee.Unregister(procName); err != nil {
		logger.Println("Failed to unregister procedure:", err)
	}
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
