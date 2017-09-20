package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const exampleTopic = "example.hello"

func main() {
	cfg := client.ClientConfig{
		Realm: "nexus.examples",
	}
	logger := log.New(os.Stdout, "", 0)

	// Connect subscriber session.
	subscriber, err := client.NewWebsocketClient("ws://localhost:8000/",
		client.JSON, nil, nil, cfg, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer subscriber.Close()

	// Define function to handle events received.
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		logger.Println("Received", exampleTopic, "event")
		if len(args) != 0 {
			m, _ := wamp.AsString(args[0])
			logger.Println("  Event Message:", m)
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
