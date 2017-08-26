package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

func main() {
	logger := log.New(os.Stderr, "CALLER> ", log.LstdFlags)
	cfg := client.ClientConfig{
		Realm: "nexus.examples",
	}
	// Connect caller session.
	caller, err := client.NewWebsocketClient(
		"ws://localhost:8000/", client.JSON, nil, nil, cfg, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer caller.Close()

	// Register procedure "sum"
	procName := "sum"

	// Test calling the procedure.
	callArgs := wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ctx := context.Background()
	fmt.Println("Call remote procedure to sum numbers 1-10")
	result, err := caller.Call(ctx, procName, nil, callArgs, nil, "")
	if err != nil {
		logger.Println("failed to call procedure:", err)
	}
	sum, _ := wamp.AsInt64(result.Arguments[0])
	fmt.Println("Result is:", sum)
}
