package main

import (
	"context"
	"github.com/dtegapp/nexus/v3/client"
	"github.com/dtegapp/nexus/v3/transport/serialize"
	"log"
	"os"
	"os/signal"

	"github.com/dtegapp/nexus/v3/wamp"
)

const (
	exampleTopic     = "example.hello"
	realm            = "realm1"
	routerUrl        = "ws://127.0.0.1:8080"
	serializationStr = "cbor"
)

func main() {
	logger := log.New(os.Stdout, "SUBSCRIBER> ", 0)

	var serialization serialize.Serialization
	var subscriber *client.Client
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

	// Connect subscriber client with requested socket type and serialization.
	subscriber, err = client.ConnectNet(context.Background(), routerUrl, cfg)
	if err != nil {
		logger.Fatalf("Cannot connect to server error: %s", err)
		return
	}
	defer subscriber.Close()

	logger.Println("Connected to", routerUrl, "using", serializationStr, "serialization")

	// Define function to handle events received.
	eventHandler := func(event *wamp.Event) {
		logger.Println("Received", exampleTopic, "event")
		logger.Println("  args payload:", event.Arguments)
		logger.Println("  kwargs payload:", event.ArgumentsKw)
	}

	// Subscribe to topic.
	err = subscriber.Subscribe(exampleTopic, eventHandler, nil)
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
		logger.Fatal("Failed to unsubscribe:", err)
	}
}
