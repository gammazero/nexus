package main

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"os"

	"github.com/gammazero/nexus/v3/examples/newclient"
	"github.com/gammazero/nexus/v3/wamp"
)

const procedureName = "sum"

func main() {
	logger := log.New(os.Stderr, "CALLER> ", 0)
	if err := run(logger); err != nil {
		logger.Fatal(err)
	}
}

func run(logger *log.Logger) error {
	// Connect caller client with requested socket type and serialization.
	caller, err := newclient.NewClient(logger)
	if err != nil {
		return err
	}
	defer caller.Close()

	// Test calling the "sum" procedure with each ppt scheme and payload
	// serializer. Requires external rpc client to be running.
	calls := []struct {
		scheme     string
		serializer string
	}{
		{"x_custom", "cbor"},
		{"mqtt", "msgpack"},
		{"mqtt", ""},
	}
	ctx := context.Background()
	for _, c := range calls {
		serializer := c.serializer
		options := wamp.Dict{wamp.OptPPTScheme: c.scheme}
		if serializer != "" {
			options[wamp.OptPPTSerializer] = serializer
		} else {
			serializer = "native"
		}
		logger.Printf("Calling remote procedure to sum numbers with %s ppt scheme and %s payload serializer",
			c.scheme, serializer)
		callArgs := wamp.List{rand.Intn(100), rand.Intn(100)}
		result, err := caller.Call(ctx, procedureName, options, callArgs, nil, nil)
		if err != nil {
			return err
		}
		if len(result.Arguments) == 0 {
			return errors.New("result has no arguments")
		}
		sum, _ := wamp.AsInt64(result.Arguments[0])
		logger.Println("Result is:", sum)
	}
	return nil
}
