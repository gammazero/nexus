package main

import (
	"context"
	"log"
	"os"

	"github.com/gammazero/nexus/examples/newclient"
	"github.com/gammazero/nexus/wamp"
)

func main() {
	logger := log.New(os.Stderr, "CALLER> ", 0)

	// Connect caller client with requested socket type and serialization.
	caller, err := newclient.NewClient(logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer caller.Close()

	// Test calling the "sum" procedure with args 1..10.
	callArgs := wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ctx := context.Background()
	logger.Println("Call remote procedure to sum numbers 1-10")
	result, err := caller.Call(ctx, "sum", nil, callArgs, nil, "")
	if err != nil {
		logger.Fatal(err)
	}
	sum, _ := wamp.AsInt64(result.Arguments[0])
	logger.Println("Result is:", sum)
}
