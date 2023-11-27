package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/dtegapp/nexus/v3/client"
	"github.com/dtegapp/nexus/v3/examples/newclient"
	"github.com/dtegapp/nexus/v3/wamp"
)

const procedureName = "sum"

func main() {
	logger := log.New(os.Stdout, "CALLEE> ", 0)
	// Connect callee client with requested socket type and serialization.
	callee, err := newclient.NewClient(logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer callee.Close()

	// Register procedure "sum"
	if err = callee.Register(procedureName, sum, nil); err != nil {
		logger.Fatal("Failed to register procedure:", err)
	}
	logger.Println("Registered procedure", procedureName, "with router")

	// Wait for CTRL-c or client close while handling remote procedure calls.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
	case <-callee.Done():
		logger.Print("Router gone, exiting")
		return // router gone, just exit
	}

	if err = callee.Unregister(procedureName); err != nil {
		logger.Println("Failed to unregister procedure:", err)
	}
}

func sum(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	log.Println("Calculating sum")
	var sum int64
	for _, arg := range inv.Arguments {
		n, ok := wamp.AsInt64(arg)
		if ok {
			sum += n
		}
	}
	return client.InvokeResult{Args: wamp.List{sum}}
}
