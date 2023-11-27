/*
Go WAMP client: publisher and caller

Publishes events and makes RPC calls to the python-autobahn client in this
directory to demonstrate interoperability.
*/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/dtegapp/nexus/v3/client"
	"github.com/dtegapp/nexus/v3/wamp"
)

const (
	addr  = "ws://localhost:8080/ws"
	realm = "realm1"

	topic = "example.hello"

	procedure  = "example.add2"
	procedure2 = "example.longop"
)

func main() {
	cfg := client.Config{
		Realm:         realm,
		Logger:        log.New(os.Stderr, "go-client> ", 0),
		Serialization: client.MSGPACK,
	}
	cli, err := client.ConnectNet(context.Background(), addr, cfg)
	if err != nil {
		fmt.Println("connot connect to router", err)
		return
	}
	defer cli.Close()

	publishEvents(cli)
	callRpc(cli)
	callRpcProgressiveResults(cli)
}

func publishEvents(publisher *client.Client) {
	// Publish events to topic
	for i := 0; i < 5; i++ {
		msg := fmt.Sprintf("Testing %d", i)
		err := publisher.Publish(topic, nil, wamp.List{msg}, nil)
		if err != nil {
			fmt.Printf("publish error: %s", err)
			return
		}
		fmt.Printf("Published %q\n", msg)
		time.Sleep(time.Second)
	}
}

func callRpc(caller *client.Client) {
	// Call procedure to sum arguments
	ctx := context.Background()
	result, err := caller.Call(ctx, procedure, nil, wamp.List{2, 3}, nil, nil)
	if err != nil {
		fmt.Println("Failed to call procedure:", err)
		return
	}
	val, _ := wamp.AsInt64(result.Arguments[0])
	fmt.Println("Call result:", val)
}

func callRpcProgressiveResults(caller *client.Client) {
	progHandler := func(result *wamp.Result) {
		progResult := result.Arguments[0].(string)
		fmt.Println("Received chunk:", progResult)
	}

	// Call procedure to get progressive results
	ctx := context.Background()
	result, err := caller.Call(
		ctx, procedure2, nil, wamp.List{"a"}, nil, progHandler)
	if err != nil {
		fmt.Println("Failed to call procedure:", err)
		return
	}

	fmt.Println("Final result:", result.Arguments[0].(string))
}
