package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/examples/newclient"
	"github.com/gammazero/nexus/v3/wamp"
)

const procedureName = "sum"

var logger = log.New(os.Stdout, "CALLEE> ", 0)

func main() {
	if err := run(); err != nil {
		logger.Fatal(err)
	}
}

func run() error {
	// Connect callee client with requested socket type and serialization.
	callee, err := newclient.NewClient(logger)
	if err != nil {
		return err
	}
	defer callee.Close()

	// Register procedure "sum"
	if err = callee.Register(procedureName, sum, nil); err != nil {
		return fmt.Errorf("failed to register procedure: %w", err)
	}
	logger.Println("Registered procedure", procedureName)

	// Wait for CTRL-c or client close while handling remote procedure calls.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
	case <-callee.Done():
		logger.Print("Router gone, exiting")
		return nil // router gone, just exit
	}

	if err = callee.Unregister(procedureName); err != nil {
		logger.Println("Failed to unregister procedure:", err)
	}
	return nil
}

func sum(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	logger.Println("Received RPC Invocation")
	logger.Println("  args payload:", inv.Arguments)
	logger.Println("  kwargs payload:", inv.ArgumentsKw)
	var sum int64
	for _, arg := range inv.Arguments {
		n, ok := wamp.AsInt64(arg)
		if ok {
			sum += n
		}
	}
	logger.Println("  Calculated sum", sum)

	var options wamp.Dict
	// Make a random choice to demonstrate different serializers.
	switch rand.Intn(3) {
	case 0:
		options = wamp.Dict{
			wamp.OptPPTScheme:     "x_custom",
			wamp.OptPPTSerializer: "cbor",
		}
	case 1:
		options = wamp.Dict{
			wamp.OptPPTScheme:     "mqtt",
			wamp.OptPPTSerializer: "msgpack",
		}
	case 2:
		options = wamp.Dict{
			wamp.OptPPTScheme: "mqtt",
		}
	}
	logger.Println("  Sending back result with", options[wamp.OptPPTScheme], "scheme")

	return client.InvokeResult{Options: options, Args: wamp.List{sum}}
}
