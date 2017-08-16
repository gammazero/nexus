package main

import (
	"log"
	"os"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const exampleTopic = "example.hello"

func main() {
	logger := log.New(os.Stdout, "PUBLISHER> ", log.LstdFlags)
	publisher, err := client.NewWebsocketClient(
		"ws://localhost:8000/", client.JSON, nil, nil, 0, logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer publisher.Close()

	// Connect publisher session.
	_, err = publisher.JoinRealm("nexus.examples", nil, nil)
	if err != nil {
		logger.Fatal(err)
	}

	// Publish to topic.
	args := wamp.List{"hello world"}
	err = publisher.Publish(exampleTopic, nil, args, nil)
	if err != nil {
		logger.Fatal("subscribe error:", err)
	}

	// Publish more events to topic.
	args = wamp.List{"how are you today"}
	err = publisher.Publish(exampleTopic, nil, args, nil)
	if err != nil {
		logger.Fatal("subscribe error:", err)
	}

	args = wamp.List{"testing 1"}
	err = publisher.Publish(exampleTopic, nil, args, nil)
	if err != nil {
		logger.Fatal("subscribe error:", err)
	}

	args = wamp.List{"testing 2"}
	err = publisher.Publish(exampleTopic, nil, args, nil)
	if err != nil {
		logger.Fatal("subscribe error:", err)
	}

	args = wamp.List{"testing 3"}
	err = publisher.Publish(exampleTopic, nil, args, nil)
	if err != nil {
		logger.Fatal("subscribe error:", err)
	}
}
