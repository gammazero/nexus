package aat

import (
	"context"
	"testing"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

func TestRPCSharedRoundRobin(t *testing.T) {
	const procName = "shared.test.procedure"
	options := wamp.SetOption(nil, "invoke", "roundrobin")

	// Connect callee1 and register test procedure.
	callee1, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client: ", err)
	}
	testProc1 := func(ctx context.Context, args []interface{}, kwargs, details map[string]interface{}) *client.InvokeResult {
		return &client.InvokeResult{Args: []interface{}{1}}
	}
	if err = callee1.Register(procName, testProc1, options); err != nil {
		t.Fatal("failed to register procedure: ", err)
	}

	// Connect callee2 and register test procedure.
	callee2, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client: ", err)
	}
	testProc2 := func(ctx context.Context, args []interface{}, kwargs, details map[string]interface{}) *client.InvokeResult {
		return &client.InvokeResult{Args: []interface{}{2}}
	}
	if err = callee2.Register(procName, testProc2, options); err != nil {
		t.Fatal("failed to register procedure: ", err)
	}

	// Connect callee3 and register test procedure.
	callee3, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client: ", err)
	}
	testProc3 := func(ctx context.Context, args []interface{}, kwargs, details map[string]interface{}) *client.InvokeResult {
		return &client.InvokeResult{Args: []interface{}{3}}
	}
	if err = callee3.Register(procName, testProc3, options); err != nil {
		t.Fatal("failed to register procedure: ", err)
	}

	// Connect caller session.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client: ", err)
	}

	expect := int64(1)
	for i := 0; i < 5; i++ {
		// Test calling the procedure - expect callee1-3
		ctx := context.Background()
		result, err := caller.Call(ctx, procName, options, nil, nil, "")
		if err != nil {
			t.Fatal("failed to call procedure: ", err)
		}
		num, _ := wamp.AsInt64(result.Arguments[0])
		if num != expect {
			t.Fatal("got result from wrong callee")
		}
		if expect == 3 {
			expect = 1
		} else {
			expect++
		}
	}

	// Unregister callee2 and make sure round robin still works.
	if err = callee2.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure: ", err)
	}

	expect = int64(1)
	for i := 0; i < 5; i++ {
		// Test calling the procedure - expect callee1-3
		ctx := context.Background()
		result, err := caller.Call(ctx, procName, options, nil, nil, "")
		if err != nil {
			t.Fatal("failed to call procedure: ", err)
		}
		num, _ := wamp.AsInt64(result.Arguments[0])
		if num != expect {
			t.Fatal("got result from wrong callee")
		}
		if expect == 1 {
			expect = 3
		} else {
			expect = 1
		}
	}

	// Test unregister.
	if err = callee1.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure: ", err)
	}
	if err = callee3.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure: ", err)
	}

	err = callee1.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client: ", err)
	}
	err = callee2.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client: ", err)
	}
	err = callee3.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client: ", err)
	}

	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client: ", err)
	}
}

func TestRPCSharedRandom(t *testing.T) {
	const procName = "shared.test.procedure"
	options := wamp.SetOption(nil, "invoke", "random")

	// Connect callee1 and register test procedure.
	callee1, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client: ", err)
	}
	testProc1 := func(ctx context.Context, args []interface{}, kwargs, details map[string]interface{}) *client.InvokeResult {
		return &client.InvokeResult{Args: []interface{}{1}}
	}
	if err = callee1.Register(procName, testProc1, options); err != nil {
		t.Fatal("failed to register procedure: ", err)
	}

	// Connect callee2 and register test procedure.
	callee2, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client: ", err)
	}
	testProc2 := func(ctx context.Context, args []interface{}, kwargs, details map[string]interface{}) *client.InvokeResult {
		return &client.InvokeResult{Args: []interface{}{2}}
	}
	if err = callee2.Register(procName, testProc2, options); err != nil {
		t.Fatal("failed to register procedure: ", err)
	}

	// Connect callee3 and register test procedure.
	callee3, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client: ", err)
	}
	testProc3 := func(ctx context.Context, args []interface{}, kwargs, details map[string]interface{}) *client.InvokeResult {
		return &client.InvokeResult{Args: []interface{}{3}}
	}
	if err = callee3.Register(procName, testProc3, options); err != nil {
		t.Fatal("failed to register procedure: ", err)
	}

	// Connect caller session.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client: ", err)
	}

	var called1, called2, called3 int
	var i int
	for i = 0; i < 20; i++ {
		// Test calling the procedure - expect callee1-3
		ctx := context.Background()
		result, err := caller.Call(ctx, procName, options, nil, nil, "")
		if err != nil {
			t.Fatal("failed to call procedure: ", err)
		}
		num, _ := wamp.AsInt64(result.Arguments[0])
		switch num {
		case 1:
			called1++
		case 2:
			called2++
		case 3:
			called3++
		}
	}
	if called1 == i || called2 == i || called3 == i {
		t.Error("only called one callee with random distribution")
	}

	// Test unregister.
	if err = callee1.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure: ", err)
	}
	if err = callee2.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure: ", err)
	}
	if err = callee3.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure: ", err)
	}

	err = callee1.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client: ", err)
	}
	err = callee2.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client: ", err)
	}
	err = callee3.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client: ", err)
	}

	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client: ", err)
	}
}
