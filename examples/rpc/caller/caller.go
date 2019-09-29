package main

import (
	"context"
	"log"
	"os"

	"github.com/gammazero/nexus/v3/examples/newclient"
	"github.com/gammazero/nexus/v3/wamp"
)

func main() {
	logger := log.New(os.Stderr, "CALLER> ", 0)

	// Connect caller client with requested socket type and serialization.
	caller, err := newclient.NewClient(logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer caller.Close()

	// Test calling the embedded "worldtime" procedure.
	callArgs := wamp.List{"America/New_York", "America/Los_Angeles", "Asia/Shanghai"}
	ctx := context.Background()
	logger.Println("Call remote procedure to time in specified timezones")
	result, err := caller.Call(ctx, "worldtime", nil, callArgs, nil, nil)
	if err != nil {
		logger.Fatal(err)
	}
	for i := range result.Arguments {
		locTime, _ := wamp.AsString(result.Arguments[i])
		logger.Println(locTime)
	}

	// Test calling the "sum" procedure with args 1..10.  Requires
	// external rpc client to be running.
	callArgs = wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	logger.Println("Call remote procedure to sum numbers 1-10")
	result, err = caller.Call(ctx, "sum", nil, callArgs, nil, nil)
	if err != nil {
		logger.Fatal(err)
	}
	sum, _ := wamp.AsInt64(result.Arguments[0])
	logger.Println("Result is:", sum)

}
