package main

import (
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

	logger := log.New(os.Stdout, "SUBSCRIBER> ", log.LstdFlags)
	// Connect subscriber session.
	subscriber, err := newClient(clientType, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer subscriber.Close()

	// Define function to handle events received.
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		fmt.Println("Received", exampleTopic, "event.")
		if len(args) != 0 {
			m, _ := wamp.AsString(args[0])
			fmt.Println("  Event Message:", m)
		}
	}

	// Subscribe to topic.
	err = subscriber.Subscribe(exampleTopic, evtHandler, nil)
	if err != nil {
		logger.Fatal("subscribe error:", err)
	}
	logger.Println("Subscribed to", exampleTopic)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	// Wait for CTRL-c or client close while handling events.
	select {
	case <-sigChan:
	case <-subscriber.Done():
		logger.Print("Router gone, exiting")
		return // router gone, just exit
	}

	// Unsubscribe from topic.
	if err = subscriber.Unsubscribe(exampleTopic); err != nil {
		logger.Println("Failed to unsubscribe:", err)
	}
}
