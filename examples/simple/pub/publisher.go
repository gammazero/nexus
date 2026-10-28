package main

import (
	"context"
	"fmt"
	"log"
	"os"

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
	if err := run(logger); err != nil {
		logger.Fatal(err)
	}
}

func run(logger *log.Logger) error {
	cfg := client.Config{
		Realm:  realm,
		Logger: logger,
	}

	// Connect publisher session.
	publisher, err := client.ConnectNet(context.Background(), addr, cfg)
	if err != nil {
		return err
	}
	defer publisher.Close()

	// Publish to topic.
	err = publisher.Publish(exampleTopic, nil, wamp.List{"hello world"}, nil)
	if err != nil {
		return fmt.Errorf("publish error: %w", err)
	}
	logger.Println("Published", exampleTopic, "event")
	return nil
}
