package main

import (
	"context"
	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/gammazero/nexus/v3/wamp"
)

const (
	procedureName    = "sum"
	realm            = "realm1"
	routerUrl        = "ws://127.0.0.1:8080"
	serializationStr = "cbor"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	logger := log.New(os.Stderr, "CALLER> ", 0)

	var serialization serialize.Serialization
	var caller *client.Client
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

	// Connect caller client with requested socket type and serialization.
	caller, err = client.ConnectNet(context.Background(), routerUrl, cfg)
	if err != nil {
		logger.Fatalf("Cannot connect to server error: %s", err)
		return
	}
	defer caller.Close()

	logger.Println("Connected to", routerUrl, "using", serializationStr, "serialization")

	// Test calling the "sum" procedure with args 1..10.  Requires
	// external rpc client to be running.
	ctx := context.Background()
	logger.Println("Calling remote procedure to sum numbers with x_custom ppt scheme and cbor payload serializer")
	callArgs := wamp.List{rand.Intn(100), rand.Intn(100)}
	options := wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "cbor",
	}
	result, err := caller.Call(ctx, "sum", options, callArgs, nil, nil)
	if err != nil {
		logger.Fatal(err)
	}
	sum, _ := wamp.AsInt64(result.Arguments[0])
	logger.Println("Result is:", sum)

	logger.Println("Calling remote procedure to sum numbers with mqtt ppt scheme and msgpack payload serializer")
	callArgs = wamp.List{rand.Intn(100), rand.Intn(100)}
	options = wamp.Dict{
		wamp.OptPPTScheme:     "mqtt",
		wamp.OptPPTSerializer: "msgpack",
	}
	result, err = caller.Call(ctx, "sum", options, callArgs, nil, nil)
	if err != nil {
		logger.Fatal(err)
	}
	sum, _ = wamp.AsInt64(result.Arguments[0])
	logger.Println("Result is:", sum)

	logger.Println("Calling remote procedure to sum numbers with mqtt ppt scheme and native payload serializer")
	callArgs = wamp.List{rand.Intn(100), rand.Intn(100)}
	options = wamp.Dict{
		wamp.OptPPTScheme: "mqtt",
	}
	result, err = caller.Call(ctx, "sum", options, callArgs, nil, nil)
	if err != nil {
		logger.Fatal(err)
	}
	sum, _ = wamp.AsInt64(result.Arguments[0])
	logger.Println("Result is:", sum)

	caller.Close()
}
