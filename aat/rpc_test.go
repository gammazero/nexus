package aat

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

func TestRPCRegisterAndCall(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect callee session.
	callee, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	proceed := make(chan struct{})

	// Test registering a valid procedure.
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		wait, _ := wamp.AsBool(inv.ArgumentsKw["wait"])
		if wait {
			<-proceed // wait until other call finishes
		} else {
			defer close(proceed)
		}
		var sum int64
		for _, arg := range inv.Arguments {
			n, ok := wamp.AsInt64(arg)
			if ok {
				sum += n
			}
		}
		return client.InvokeResult{Args: wamp.List{sum}}
	}

	// Register procedure "sum"
	procName := "sum"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("Failed to register procedure:", err)
	}

	// Connect caller session.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Connect second caller session.
	caller2, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Test calling the procedure.
	callArgs := wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	var result1, result2 *wamp.Result
	var err1, err2 error
	var allDone sync.WaitGroup
	allDone.Add(2)
	go func() {
		defer allDone.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		kwArgs := wamp.Dict{
			"wait": true,
		}
		result1, err1 = caller.Call(ctx, procName, nil, callArgs, kwArgs, nil)
	}()
	go func() {
		defer allDone.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result2, err2 = caller2.Call(ctx, procName, nil, callArgs, nil, nil)
	}()

	allDone.Wait()

	errs := []error{err1, err2}
	results := []*wamp.Result{result1, result2}
	for i := 0; i < len(errs); i++ {
		if errs[i] != nil {
			t.Error("Caller", i, "failed to call procedure:", errs[i])
		} else {
			sum, ok := wamp.AsInt64(results[i].Arguments[0])
			if !ok {
				t.Error("Could not convert result", i, "to int64")
			} else if sum != 55 {
				t.Errorf("Wrong result %d: %d", i, sum)
			}
		}
	}

	// Test unregister.
	if err = callee.Unregister(procName); err != nil {
		t.Error("Failed to unregister procedure:", err)
	}

	err = caller.Close()
	if err != nil {
		t.Error("Failed to disconnect client:", err)
	}

	err = caller2.Close()
	if err != nil {
		t.Error("Failed to disconnect client:", err)
	}

	err = callee.Close()
	if err != nil {
		t.Error("Failed to disconnect client:", err)
	}
}

func TestRPCCallUnregistered(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect caller session.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Test calling unregistered procedure.
	callArgs := wamp.List{555}
	ctx := context.Background()
	result, err := caller.Call(ctx, "NotRegistered", nil, callArgs, nil, nil)
	if err == nil {
		t.Fatal("expected error calling unregistered procedure")
	}
	if result != nil {
		t.Fatal("result should be nil on error")
	}

	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestRPCUnregisterUnregistered(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect caller session.
	callee, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Test unregister unregistered procedure.
	if err = callee.Unregister("NotHere"); err == nil {
		t.Fatal("expected error unregistering unregistered procedure")
	}

	err = callee.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestRPCCancelCall(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect callee session.
	callee, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer callee.Close()

	// Check for feature support in router.
	if !callee.HasFeature(wamp.RoleDealer, wamp.FeatureCallCanceling) {
		t.Error("Dealer does not support", wamp.FeatureCallCanceling)
	}

	invkCanceled := make(chan struct{}, 1)
	// Register procedure that waits.
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		invkCanceled <- struct{}{}
		return client.InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure:", err)
	}

	// Connect caller session.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	if err = caller.SetCallCancelMode(wamp.CancelModeKillNoWait); err != nil {
		t.Fatal(err)
	}

	errChan := make(chan error)
	ctx, cancel := context.WithCancel(context.Background())
	// Calling the procedure, should block.
	go func() {
		callArgs := wamp.List{73}
		_, e := caller.Call(ctx, procName, nil, callArgs, nil, nil)
		errChan <- e
	}()

	// Make sure the call is blocked.
	select {
	case err = <-errChan:
		t.Fatal("call should have been blocked")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()

	// Make sure the call is canceled on caller side.
	select {
	case err = <-errChan:
		if err == nil {
			t.Fatal("expected error from canceling call")
		}
		if err != context.Canceled {
			t.Fatal("expected context.Canceled error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call should have been canceled")
	}

	// Make sure the invocation is canceled on callee side.
	select {
	case <-invkCanceled:
	case <-time.After(time.Second):
		t.Fatal("invocation should have been canceled")
	}

	if err = callee.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure:", err)
	}
	err = callee.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestRPCTimeoutCall(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect callee session.
	callee, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Check for feature support in router.
	if !callee.HasFeature(wamp.RoleDealer, wamp.FeatureCallTimeout) {
		t.Error("Dealer does not support", wamp.FeatureCallTimeout)
	}

	invkCanceled := make(chan struct{}, 1)
	// Register procedure that waits.
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		invkCanceled <- struct{}{}
		return client.InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure:", err)
	}

	// Connect caller session.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	errChan := make(chan error)
	ctx := context.Background()
	opts := wamp.Dict{"timeout": 1000}
	// Calling the procedure, should block.
	go func() {
		callArgs := wamp.List{73}
		_, e := caller.Call(ctx, procName, opts, callArgs, nil, nil)
		errChan <- e
	}()

	// Make sure the call is blocked.
	select {
	case err = <-errChan:
		t.Fatal("call should have been blocked")
	case <-time.After(200 * time.Millisecond):
	}

	// Make sure the call is canceled on caller side.
	select {
	case err = <-errChan:
		if err == nil {
			t.Fatal("Expected error from canceling call")
		}
		if err.(client.RPCError).Err.Error != wamp.ErrCanceled {
			t.Fatal("Wrong error for canceled call, got",
				err.(client.RPCError).Err.Error, "want", wamp.ErrCanceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call should have been canceled")
	}

	// Make sure the invocation is canceled on callee side.
	select {
	case <-invkCanceled:
	case <-time.After(time.Second):
		t.Fatal("invocation should have been canceled")
	}

	rpcError, ok := err.(client.RPCError)
	if !ok {
		t.Fatal("expected RPCError type of error")
	}
	if rpcError.Err.Error != wamp.ErrCanceled {
		t.Fatal("expected canceled error, got:", err)
	}
	if err = callee.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure:", err)
	}
	err = callee.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestRPCResponseRouting(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect callee session.
	callee, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	respond := make(chan struct{})
	ready := sync.WaitGroup{}
	ready.Add(2)

	// Register procedure "hello"
	hello := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		ready.Done()
		<-respond
		return client.InvokeResult{Args: wamp.List{"HELLO"}}
	}
	if err = callee.Register("hello", hello, nil); err != nil {
		t.Fatal("Failed to register procedure:", err)
	}

	// Register procedure "world"
	world := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		ready.Done()
		<-respond
		return client.InvokeResult{Args: wamp.List{"WORLD"}}
	}
	// Register procedure "hello"
	if err = callee.Register("world", world, nil); err != nil {
		t.Fatal("Failed to register procedure:", err)
	}

	// Connect hello caller session.
	callerHello, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Connect world caller session.
	callerWorld, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	rc1 := make(chan *wamp.Result)
	rc2 := make(chan *wamp.Result)
	ec1 := make(chan error)
	ec2 := make(chan error)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go func() {
		result1, err1 := callerHello.Call(ctx, "hello", nil, nil, nil, nil)
		if err1 != nil {
			ec1 <- err1
			return
		}
		rc1 <- result1
	}()
	go func() {
		result2, err2 := callerWorld.Call(ctx, "world", nil, nil, nil, nil)
		if err2 != nil {
			ec2 <- err2
			return
		}
		rc2 <- result2
	}()

	ready.Wait()
	close(respond)

	var result *wamp.Result
	for i := 0; i < 2; i++ {
		select {
		case result = <-rc1:
			if result.Arguments[0] != "HELLO" {
				t.Error("Wrong result for call to 'hello':", result.Arguments[0])
			}
		case result = <-rc2:
			if result.Arguments[0] != "WORLD" {
				t.Error("Wrong result for call to 'world':", result.Arguments[0])
			}
		case err = <-ec1:
			t.Error("Error calling 'hello':", err)
		case err = <-ec2:
			t.Error("Error calling 'world':", err)
		}
	}

	err = callerHello.Close()
	if err != nil {
		t.Error("Failed to disconnect client:", err)
	}

	err = callerWorld.Close()
	if err != nil {
		t.Error("Failed to disconnect client:", err)
	}

	err = callee.Close()
	if err != nil {
		t.Error("Failed to disconnect client:", err)
	}
}
