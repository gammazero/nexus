package main

import (
	"context"
	"github.com/dtegapp/nexus/v3/transport/serialize"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"time"

	"github.com/dtegapp/nexus/v3/client"
	"github.com/dtegapp/nexus/v3/wamp"
)

const (
	procedureName    = "sum"
	realm            = "realm1"
	routerUrl        = "ws://127.0.0.1:8080"
	serializationStr = "cbor"
)

var logger = log.New(os.Stdout, "CALLEE> ", 0)

func main() {
	rand.Seed(time.Now().UnixNano())

	var serialization serialize.Serialization
	var callee *client.Client
	var err error

	// Get requested serialization.
	switch serializationStr {
	case "json":
		serialization = client.JSON
	case "msgpack":
		serialization = client.MSGPACK
	case "cbor":
		serialization = client.CBOR
	}

	cfg := client.Config{
		Realm:         realm,
		Serialization: serialization,
		Logger:        logger,
	}

	// Connect callee client with requested socket type and serialization.
	callee, err = client.ConnectNet(context.Background(), routerUrl, cfg)
	if err != nil {
		logger.Fatalf("Cannot connect to server error: %s", err)
		return
	}
	defer callee.Close()

	logger.Println("Connected to", routerUrl, "using", serializationStr, "serialization")

	// Register procedure "sum"
	if err = callee.Register(procedureName, sum, nil); err != nil {
		logger.Fatal("Failed to register procedure:", err)
	}
	logger.Println("Registered procedure", procedureName)

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

	options := wamp.Dict{}
	r := rand.Intn(3)
	switch r {
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
