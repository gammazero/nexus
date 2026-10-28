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

	// Publish an event with each ppt scheme and payload serializer.
	events := []struct {
		scheme     string
		serializer string
		property1  int
		property2  string
		property3  bool
	}{
		{"x_custom", "cbor", 25, "ppt is working", true},
		{"mqtt", "msgpack", 45, "ppt is working again", false},
		{"mqtt", "", 75, "ppt is working again", true},
	}
	for _, e := range events {
		serializer := e.serializer
		options := wamp.Dict{wamp.OptPPTScheme: e.scheme}
		if serializer != "" {
			options[wamp.OptPPTSerializer] = serializer
		} else {
			serializer = "native"
		}
		args := wamp.List{fmt.Sprintf("Event with %s ppt scheme and %s payload serializer", e.scheme, serializer)}
		kwargs := wamp.Dict{
			"property1": e.property1,
			"property2": e.property2,
			"property3": e.property3,
		}
		if err = publisher.Publish(exampleTopic, options, args, kwargs); err != nil {
			return fmt.Errorf("publish error: %w", err)
		}
		logger.Println("Published message to", exampleTopic)
		logger.Println("  args payload:", args)
		logger.Println("  kwargs payload:", kwargs)
	}
	return nil
}
