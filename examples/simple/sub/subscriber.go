package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

const (
	addr  = "ws://localhost:8080/ws"
	realm = "realm1"

	exampleTopic = "example.hello"
)

func main() {
	logger := log.New(os.Stdout, "", 0)
	cfg := client.Config{
		Realm:  realm,
		Logger: logger,
	}

	// Connect subscriber session.
	subscriber, err := client.ConnectNet(context.Background(), addr, cfg)
	if err != nil {
		logger.Fatal(err)
	}
	defer subscriber.Close()

	// Define function to handle events received.
	eventHandler := func(event *wamp.Event) {
		logger.Println("Received", exampleTopic, "event")
		if len(event.Arguments) != 0 {
			logger.Println("  Event Message:", event.Arguments[0])
		}
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
		logger.Println("Failed to unsubscribe:", err)
	}
}
