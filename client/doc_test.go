package client

import (
	"context"
	"log"
	"time"

	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/wamp"
)

func ExampleConnectLocal() {
	// Create router that local client attaches to.
	routerConfig := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			{
				URI:           wamp.URI("nexus.realm1"),
				AnonymousAuth: true,
			},
		},
	}
	r, err := router.NewRouter(routerConfig, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Configure and connect local client.
	c, err := ConnectLocal(r, Config{Realm: "nexus.realm1"})
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
}

func ExampleConnectNet() {
	// Configure and connect client.
	c, err := ConnectNet("unix:///tmp/app.sock", Config{Realm: "nexus.realm1"})
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
}

func ExampleClient_Call() {
	// Configure and connect caller client.
	cfg := Config{Realm: "nexus.realm1"}
	caller, err := ConnectNet("ws://localhost:8080/", cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer caller.Close()

	// Create a context to cancel the call after 5 seconds if no response.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test calling the "sum" procedure with args 1..10.
	callArgs := wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	result, err := caller.Call(ctx, "sum", nil, callArgs, nil, "")
	if err != nil {
		log.Fatal(err)
	}

	// Print the result.
	sum, _ := wamp.AsInt64(result.Arguments[0])
	log.Println("The sum is:", sum)
}

func ExampleClient_CallProgress() {
	// Configure and connect caller client.
	cfg := Config{Realm: "nexus.realm1"}
	caller, err := ConnectNet("ws://localhost:8080/", cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer caller.Close()

	// The progress handler prints test output as it arrives.
	progHandler := func(result *wamp.Result) {
		// Received more progress ingo from callee.
		percentDone, _ := wamp.AsInt64(result.Arguments[0])
		log.Printf("Test is %d%% done", percentDone)
	}

	// Create a context to cancel the call in one minute if not finished.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// Call the example procedure "run.test".  The argument specifies how
	// frequently to send progress updates.
	result, err := caller.CallProgress(
		ctx, "run.test", nil, wamp.List{time.Second * 2}, nil, "", progHandler)
	if err != nil {
		log.Fatal("Failed to call procedure:", err)
	}

	// As a final result, in this example, the callee returns the boolean
	// result of the test.
	passFail, _ := result.Arguments[0].(bool)
	if passFail {
		log.Println("Test passed")
	} else {
		log.Println("Test failed")
	}
}

func ExampleClient_Register() {
	// Configure and connect callee client.
	cfg := Config{Realm: "nexus.realm1"}
	callee, err := ConnectNet("tcp://localhost:8080/", cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer callee.Close()

	// Define function that is called to perform remote procedure.
	sum := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *InvokeResult {
		var sum int64
		for i := range args {
			n, _ := wamp.AsInt64(args[i])
			sum += n
		}
		return &InvokeResult{Args: wamp.List{sum}}
	}

	// Register procedure "sum"
	if err = callee.Register("sum", sum, nil); err != nil {
		log.Fatal("Failed to register procedure:", err)
	}

	// Keep handling remote procedure calls until router exits.
	<-callee.Done()
}

func ExampleClient_Register_progressive() {
	// Configure and connect callee client.
	cfg := Config{Realm: "nexus.realm1"}
	callee, err := ConnectNet("tcp://localhost:8080/", cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer callee.Close()

	// Define function that is called to perform remote procedure.
	sendData := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *InvokeResult {
		// Get update interval from caller.
		interval, _ := wamp.AsInt64(args[0])

		// Send simulated progress every interval.
		ticker := time.NewTicker(time.Duration(interval))
		defer ticker.Stop()
		for percentDone := 0; percentDone < 100; {
			<-ticker.C
			percentDone += 20
			if e := callee.SendProgress(ctx, wamp.List{percentDone}, nil); e != nil {
				// If send failed, return error saying the call is canceled.
				return nil
			}
		}
		// Send true as final result.
		return &InvokeResult{Args: wamp.List{true}}
	}

	// Register example procedure.
	if err = callee.Register("system_test", sendData, nil); err != nil {
		log.Fatal("Failed to register procedure:", err)
	}

	// Keep handling remote procedure calls until router exits.
	<-callee.Done()
}

func ExampleClient_SendProgress() {
	// Configure and connect callee client.
	cfg := Config{Realm: "nexus.realm1"}
	callee, err := ConnectNet("ws://localhost:8080/", cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer callee.Close()

	// Define function that is called to perform remote procedure.
	sendData := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *InvokeResult {
		// Get update interval from caller.
		interval, _ := wamp.AsInt64(args[0])

		// Send simulated progress every interval.
		ticker := time.NewTicker(time.Duration(interval))
		defer ticker.Stop()
		for percentDone := 0; percentDone < 100; {
			<-ticker.C
			percentDone += 20
			if e := callee.SendProgress(ctx, wamp.List{percentDone}, nil); e != nil {
				// If send failed, return error saying the call is canceled.
				return nil
			}
		}
		// Send true as final result.
		return &InvokeResult{Args: wamp.List{true}}
	}

	// Register example procedure.
	if err = callee.Register("system_test", sendData, nil); err != nil {
		log.Fatal("Failed to register procedure:", err)
	}

	// Keep handling remote procedure calls until router exits.
	<-callee.Done()
}

func ExampleClient_Subscribe() {
	const (
		site1DbErrors  = "site1.db.errors"
		site2DbAny     = "site2.db."     // prefix match
		site1AnyAlerts = "site1..alerts" // wildcard match
	)

	// Configure and connect subscriber client.
	cfg := Config{Realm: "nexus.realm1"}
	subscriber, err := ConnectNet("ws://localhost:8080/", cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer subscriber.Close()

	// Define specific event handler.
	handler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		eventData, _ := wamp.AsString(args[0])
		log.Printf("Event data for topic %s: %s", site1DbErrors, eventData)
	}

	// Subscribe to event.
	err = subscriber.Subscribe(site1DbErrors, handler, nil)
	if err != nil {
		log.Fatal("subscribe error:", err)
	}

	// Define pattern-based event handler.
	patHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		// Events received for pattern-based (prefix and wildcard)
		// subscriptions contain the matched topic in the details.
		eventTopic, _ := wamp.AsURI(details["topic"])
		eventData, _ := wamp.AsString(args[0])
		log.Printf("Event data for topic %s: %s", eventTopic, eventData)
	}

	// Subscribe to event with prefix match.
	options := wamp.Dict{"match": "prefix"}
	err = subscriber.Subscribe(site2DbAny, patHandler, options)
	if err != nil {
		log.Fatal("subscribe error:", err)
	}

	// Subscribe to event with wildcard match.
	options["match"] = "wildcard"
	err = subscriber.Subscribe(site1AnyAlerts, patHandler, options)
	if err != nil {
		log.Fatal("subscribe error:", err)
	}

	// Keep handling events until router exits.
	<-subscriber.Done()
}
