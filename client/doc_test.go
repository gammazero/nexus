package client

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/gammazero/nexus/wamp"
)

func ExampleCall() {
	// Configure and connect caller client.
	logger := log.New(os.Stdout, "", 0)
	cfg := Config{
		Realm:  "nexus.realm1",
		Logger: logger,
	}
	caller, err := ConnectNet("ws://localhost:8080/", cfg)
	if err != nil {
		logger.Fatal(err)
	}
	defer caller.Close()

	// Create a context to cancel the call after 5 seconds if no response.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test calling the "sum" procedure with args 1..10.
	callArgs := wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	result, err := caller.Call(ctx, "sum", nil, callArgs, nil, "")
	if err != nil {
		logger.Fatal(err)
	}

	// Print the result.
	sum, _ := wamp.AsInt64(result.Arguments[0])
	logger.Println("The sum is:", sum)
}

func ExampleCallProgress() {
	// Configure and connect caller client.
	logger := log.New(os.Stdout, "", 0)
	cfg := Config{
		Realm:  "nexus.realm1",
		Logger: logger,
	}
	caller, err := ConnectNet("ws://localhost:8080/", cfg)
	if err != nil {
		logger.Fatal(err)
	}
	defer caller.Close()

	// The progress handler prints test output as it arrives.
	progHandler := func(result *wamp.Result) {
		// Received more progress ingo from callee.
		percentDone, _ := wamp.AsInt64(result.Arguments[0])
		logger.Printf("Test is %d%% done", percentDone)
	}

	// Create a context to cancel the call in one minute if not finished.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// Call the example procedure "run.test".  The argument specifies how
	// frequently to send progress updates.
	result, err := caller.CallProgress(
		ctx, "run.test", nil, wamp.List{time.Second * 2}, nil, "", progHandler)
	if err != nil {
		logger.Fatal("Failed to call procedure:", err)
	}

	// As a final result, in this example, the callee returns the boolean
	// result of the test.
	passFail, _ := result.Arguments[0].(bool)
	if passFail {
		logger.Println("Test passed")
	} else {
		logger.Println("Test failed")
	}
}

func ExampleRegister() {
	// Configure and connect callee client.
	logger := log.New(os.Stdout, "", 0)
	cfg := Config{
		Realm:  "nexus.realm1",
		Logger: logger,
	}
	callee, err := ConnectNet("tcp://localhost:8080/", cfg)
	if err != nil {
		logger.Fatal(err)
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
		logger.Fatal("Failed to register procedure:", err)
	}

	// Keep handling remote procedure calls until router exits.
	<-callee.Done()
}

func ExampleRegister_progressive() {
	// Configure and connect callee client.
	logger := log.New(os.Stdout, "", 0)
	cfg := Config{
		Realm:  "nexus.realm1",
		Logger: logger,
	}
	callee, err := ConnectNet("tcp://localhost:8080/", cfg)
	if err != nil {
		logger.Fatal(err)
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
			err := callee.SendProgress(ctx, wamp.List{percentDone}, nil)
			if err != nil {
				// If send failed, return error saying the call is canceled.
				return nil
			}
		}
		// Send true as final result.
		return &InvokeResult{Args: wamp.List{true}}
	}

	// Register example procedure.
	if err = callee.Register("system_test", sendData, nil); err != nil {
		logger.Fatal("Failed to register procedure:", err)
	}

	// Keep handling remote procedure calls until router exits.
	<-callee.Done()
}
