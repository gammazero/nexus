package main

import (
	"log"
	"os"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const exampleTopic = "example.hello"

func main() {
	logger := log.New(os.Stdout, "", 0)
	cfg := client.Config{
		Realm:  "nexus.realm1",
		Logger: logger,
	}

	// Connect publisher session.
	publisher, err := client.ConnectNet("http://localhost:8080/", cfg)
	if err != nil {
		logger.Fatal(err)
	}
	defer publisher.Close()

	// Publish to topic.
	err = publisher.Publish(exampleTopic, nil, wamp.List{"hello world"}, nil)
	if err != nil {
		logger.Fatal("subscribe error:", err)
	}
	logger.Println("Published", exampleTopic, "event")
}
