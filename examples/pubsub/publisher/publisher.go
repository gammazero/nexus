package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const exampleTopic = "example.hello"

func newClient(clientType string, logger *log.Logger) (*client.Client, error) {
	cfg := client.ClientConfig{
		Realm: "nexus.examples",
	}

	switch clientType {
	case "websocket":
		return client.NewWebsocketClient(
			"ws://localhost:8000/", client.JSON, nil, nil, cfg, logger)
	case "rawtcp":
		return client.NewRawSocketClient(
			"tcp", "127.0.0.1:8001", client.MSGPACK, cfg, logger, 0)
	case "rawunix":
		return client.NewRawSocketClient(
			"unix", "/tmp/exmpl_nexus_sock", client.MSGPACK, cfg, logger, 0)
	default:
		return nil, errors.New(
			"invalid type, must one of: websocket, rawtcp, rawunix")
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-type=websocket -type=rawtcp -type=rawunix]\n", os.Args[0])
}

func main() {
	var clientType string
	fs := flag.NewFlagSet("subscriber", flag.ExitOnError)
	fs.StringVar(&clientType, "type", "websocket", "Transport type, one of: websocket, rawtcp, rawunix")
	fs.Usage = usage
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	logger := log.New(os.Stdout, "PUBLISHER> ", log.LstdFlags)
	// Connect publisher session.
	publisher, err := newClient(clientType, logger)
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
}
