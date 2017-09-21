package main

import (
	"errors"
	"flag"
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

func main() {
	var clientType string
	flag.StringVar(&clientType, "type", "websocket", "Socket type, one of: websocket, rawtcp, rawunix")
	flag.Parse()

	logger := log.New(os.Stdout, "SUBSCRIBER> ", 0)
	// Connect subscriber session.
	subscriber, err := newClient(clientType, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer subscriber.Close()

	// Define function to handle events received.
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		logger.Println("Received", exampleTopic, "event")
		if len(args) != 0 {
			logger.Println("  Event Message:", args[0])
		}
	}

	// Subscribe to topic.
	err = subscriber.Subscribe(exampleTopic, evtHandler, nil)
	if err != nil {
		logger.Fatal("subscribe error:", err)
	}
	logger.Println("Subscribed to", exampleTopic)

	// Wait for CTRL-c or client close while handling events.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
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
