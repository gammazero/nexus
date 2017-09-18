package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

func newClient(clientType string, logger *log.Logger) (*client.Client, error) {
	cfg := client.ClientConfig{
		Realm: "nexus.examples",
	}

	switch clientType {
	case "websocket":
		return client.NewWebsocketClient(
			"ws://localhost:8000/", client.JSON, nil, nil, cfg, logger)
	case "rawtcp":
		return client.NewRawSocketClient(
			"tcp", "127.0.0.1:8001", client.MSGPACK, cfg, logger, 0)
	case "rawunix":
		return client.NewRawSocketClient(
			"unix", "/tmp/exmpl_nexus_sock", client.MSGPACK, cfg, logger, 0)
	default:
		return nil, errors.New(
			"invalid type, must one of: websocket, rawtcp, rawunix")
	}
}

func main() {
	var clientType string
	flag.StringVar(&clientType, "type", "websocket", "Socket type, one of: websocket, rawtcp, rawunix")
	flag.Parse()

	logger := log.New(os.Stderr, "CALLER> ", 0)

	// Connect caller session.
	caller, err := newClient(clientType, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer caller.Close()

	// Register procedure "sum"
	procName := "sum"

	// Test calling the procedure.
	callArgs := wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ctx := context.Background()
	logger.Println("Call remote procedure to sum numbers 1-10")
	result, err := caller.Call(ctx, procName, nil, callArgs, nil, "")
	if err != nil {
		logger.Println("failed to call procedure:", err)
	}
	sum, _ := wamp.AsInt64(result.Arguments[0])
	logger.Println("Result is:", sum)
}
