package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const exampleTopic = "example.hello"

func main() {
	logger := log.New(os.Stdout, "SUBSCRIBER> ", log.LstdFlags)
	subscriber, err := client.NewWebsocketClient(
		"ws://localhost:8000/", client.JSON, nil, nil, 0, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer subscriber.Close()

	// Connect subscriber session.
	_, err = subscriber.JoinRealm("nexus.examples", nil, nil)
	if err != nil {
		logger.Fatal(err)
	}

	// Define function to handle events received.
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		fmt.Println("Received", exampleTopic, "event.")
		if len(args) != 0 {
			fmt.Println("  Event Message:", args[0])
		}
	}

	// Subscribe to topic.
	err = subscriber.Subscribe(exampleTopic, evtHandler, nil)
	if err != nil {
		logger.Fatal("subscribe error:", err)
	}

	// Wait for CTRL-c while handling remote procedure calls.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan

	// Unsubscribe from topic.
	if err = subscriber.Unsubscribe(exampleTopic); err != nil {
		logger.Println("Failed to unsubscribe:", err)
	}
}
