package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gammazero/nexus/v3/examples/newclient"
	"github.com/gammazero/nexus/v3/wamp"
)

const exampleTopic = "example.hello"

func main() {
	logger := log.New(os.Stdout, "PUBLISHER> ", 0)
	if err := run(logger); err != nil {
		logger.Fatal(err)
	}
}

func run(logger *log.Logger) error {
	// Connect publisher client with requested socket type and serialization.
	publisher, err := newclient.NewClient(logger)
	if err != nil {
		return err
	}
	defer publisher.Close()

	// Publish to topic.
	args := wamp.List{"hello world"}
	if err = publisher.Publish(exampleTopic, nil, args, nil); err != nil {
		return fmt.Errorf("publish error: %w", err)
	}

	// Publish more events to topic.
	args = wamp.List{"how are you today"}
	if err = publisher.Publish(exampleTopic, nil, args, nil); err != nil {
		return fmt.Errorf("publish error: %w", err)
	}

	// Publish events only to sessions 42, 1138, 1701.
	args = wamp.List{"for your eyes only"}
	opts := wamp.Dict{wamp.WhitelistKey: wamp.List{42, 1138, 1701}}
	if err = publisher.Publish(exampleTopic, opts, args, nil); err != nil {
		return fmt.Errorf("publish error: %w", err)
	}

	for _, msg := range []string{"testing 1", "testing 2", "testing 3"} {
		args = wamp.List{msg}
		if err = publisher.Publish(exampleTopic, nil, args, nil); err != nil {
			return fmt.Errorf("publish error: %w", err)
		}
	}

	logger.Println("Published messages to", exampleTopic)
	return nil
}
