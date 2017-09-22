package main

import (
	"log"
	"os"

	"github.com/gammazero/nexus/examples/newclient"
	"github.com/gammazero/nexus/wamp"
)

const exampleTopic = "example.hello"

func main() {
	logger := log.New(os.Stdout, "PUBLISHER> ", 0)
	// Connect publisher client with requested socket type and serialization.
	publisher, err := newclient.NewClient(logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer publisher.Close()

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

	// Publish events only to sessions 42, 1138, 1701.
	args = wamp.List{"for your eyes only"}
	opts := wamp.Dict{wamp.WhitelistKey: wamp.List{42, 1138, 1701}}
	err = publisher.Publish(exampleTopic, opts, args, nil)
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

	logger.Println("Published messages to", exampleTopic)
}
