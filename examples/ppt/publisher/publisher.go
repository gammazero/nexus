package main

import (
	"context"
	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"log"
	"os"

	"github.com/gammazero/nexus/v3/wamp"
)

const (
	exampleTopic     = "example.hello"
	realm            = "realm1"
	routerUrl        = "ws://127.0.0.1:8080"
	serializationStr = "cbor"
)

func main() {
	logger := log.New(os.Stdout, "PUBLISHER> ", 0)

	var serialization serialize.Serialization
	var publisher *client.Client
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

	// Connect publisher client with requested socket type and serialization.
	publisher, err = client.ConnectNet(context.Background(), routerUrl, cfg)
	if err != nil {
		logger.Fatalf("Cannot connect to server error: %s", err)
		return
	}
	defer publisher.Close()

	logger.Println("Connected to", routerUrl, "using", serializationStr, "serialization")

	// Publish to topic.
	args := wamp.List{"Event with x_custom ppt scheme and cbor payload serializer"}
	kwargs := wamp.Dict{"property1": 25, "property2": "ppt is working", "property3": true}
	options := wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "cbor",
	}
	err = publisher.Publish(exampleTopic, options, args, kwargs)
	if err != nil {
		logger.Fatalf("publish error: %s", err)
	}
	logger.Println("Published message to", exampleTopic)
	logger.Println("  args payload:", args)
	logger.Println("  kwargs payload:", kwargs)

	// Publish more events to topic.
	args = wamp.List{"Event with mqtt ppt scheme and msgpack payload serializer"}
	kwargs = wamp.Dict{"property1": 45, "property2": "ppt is working again", "property3": false}
	options = wamp.Dict{
		wamp.OptPPTScheme:     "mqtt",
		wamp.OptPPTSerializer: "msgpack",
	}
	err = publisher.Publish(exampleTopic, options, args, kwargs)
	if err != nil {
		logger.Fatalf("publish error: %s", err)
	}
	logger.Println("Published message to", exampleTopic)
	logger.Println("  args payload:", args)
	logger.Println("  kwargs payload:", kwargs)

	args = wamp.List{"Event with mqtt ppt scheme and native payload serializer"}
	kwargs = wamp.Dict{"property1": 75, "property2": "ppt is working again", "property3": true}
	options = wamp.Dict{
		wamp.OptPPTScheme: "mqtt",
	}
	err = publisher.Publish(exampleTopic, options, args, kwargs)
	if err != nil {
		logger.Fatalf("publish error: %s", err)
	}
	logger.Println("Published message to", exampleTopic)
	logger.Println("  args payload:", args)
	logger.Println("  kwargs payload:", kwargs)

	publisher.Close()
}
