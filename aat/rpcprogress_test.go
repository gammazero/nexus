package aat

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const progProc = "nexus.test.progproc"

func TestRPCProgressiveCallResults(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect callee session.
	callee, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Test registering a valid procedure.
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		err := callee.SendProgress(ctx, wamp.List{"Alpha"}, nil)
		if err != nil {
			fmt.Println("Error sending Alpha progress:", err)
		}
		time.Sleep(1000 * time.Millisecond)

		err = callee.SendProgress(ctx, wamp.List{"Bravo"}, nil)
		if err != nil {
			fmt.Println("Error sending Bravo progress:", err)
		}
		time.Sleep(1000 * time.Millisecond)

		err = callee.SendProgress(ctx, wamp.List{"Charlie"}, nil)
		if err != nil {
			fmt.Println("Error sending Charlie progress:", err)
		}
		time.Sleep(1000 * time.Millisecond)

		var sum int64
		for i := range args {
			n, ok := wamp.AsInt64(args[i])
			if ok {
				sum += n
			}
		}
		return &client.InvokeResult{Args: wamp.List{sum}}
	}

	// Register procedure
	if err = callee.Register(progProc, handler, nil); err != nil {
		t.Fatal("Failed to register procedure:", err)
	}

	// Connect caller session.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	progCount := 0
	progHandler := func(result *wamp.Result) {
		arg := result.Arguments[0].(string)
		if (progCount == 0 && arg != "Alpha") || (progCount == 1 && arg != "Bravo") || (progCount == 2 && arg != "Charlie") {
			return
		}
		progCount++
	}

	// Test calling the procedure.
	callArgs := wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ctx := context.Background()
	fmt.Println("---> Calling", progProc)
	result, err := caller.CallProgress(ctx, progProc, nil, callArgs, nil, "", progHandler)
	fmt.Println("--->", progProc, "returned")
	if err != nil {
		t.Fatal("Failed to call procedure:", err)
	}
	sum, ok := wamp.AsInt64(result.Arguments[0])
	if !ok {
		t.Fatal("Could not convert result to int64")
	}
	if sum != 55 {
		t.Fatal("Wrong result:", sum)
	}

	// Test unregister.
	if err = callee.Unregister(progProc); err != nil {
		t.Fatal("Failed to unregister procedure:", err)
	}

	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = callee.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
	if progCount != 3 {
		t.Fatal("Expected progCount == 3")
	}
}
