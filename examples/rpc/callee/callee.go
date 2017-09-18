package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const exampleTopic = "example.hello"

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

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-type=websocket -type=rawtcp -type=rawunix]\n", os.Args[0])
}

func main() {
	var clientType string
	fs := flag.NewFlagSet("subscriber", flag.ExitOnError)
	fs.StringVar(&clientType, "type", "websocket", "Transport type, one of: websocket, rawtcp, rawunix")
	fs.Usage = usage
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	logger := log.New(os.Stdout, "CALLEE> ", log.LstdFlags)
	// Connect callee session.
	callee, err := newClient(clientType, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer callee.Close()

	// Register procedure "sum"
	procName := "sum"
	if err = callee.Register(procName, sum, nil); err != nil {
		logger.Fatal("Failed to register procedure:", err)
	}
	fmt.Println("Registered procedure", procName, "with router")

	// Wait for CTRL-c while handling remote procedure calls.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	// Wait for CTRL-c or client close while handling remote procedure calls.
	select {
	case <-sigChan:
	case <-callee.Done():
		logger.Print("Router gone, exiting")
		return // router gone, just exit
	}

	if err = callee.Unregister(procName); err != nil {
		logger.Println("Failed to unregister procedure:", err)
	}
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
