package aat

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

func TestRPCRegisterAndCall(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect callee session.
	callee, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Test registering a valid procedure.
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		var sum int64
		for i := range args {
			n, ok := wamp.AsInt64(args[i])
			if ok {
				sum += n
			}
		}
		return &client.InvokeResult{Args: wamp.List{sum}}
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

	// Connect third caller session.
	caller3, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Test calling the procedure.
	callArgs := wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	var result1, result2, result3 *wamp.Result
	var err1, err2, err3 error
	var ready, allDone sync.WaitGroup
	release := make(chan struct{})
	ready.Add(3)
	allDone.Add(3)
	go func() {
		defer allDone.Done()
		ready.Done()
		<-release
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result1, err1 = caller.Call(ctx, procName, nil, callArgs, nil, "")
	}()
	go func() {
		defer allDone.Done()
		// Call it with caller2.
		ready.Done()
		<-release
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result2, err2 = caller2.Call(ctx, procName, nil, callArgs, nil, "")
	}()
	go func() {
		// Call it with caller3.
		defer allDone.Done()
		ready.Done()
		<-release
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result3, err3 = caller3.Call(ctx, procName, nil, callArgs, nil, "")
	}()

	ready.Wait()
	close(release)
	allDone.Wait()

	errs := []error{err1, err2, err3}
	results := []*wamp.Result{result1, result2, result3}
	for i := 0; i < 3; i++ {
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

	err = caller3.Close()
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
	result, err := caller.Call(ctx, "NotRegistered", nil, callArgs, nil, "")
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

	// Check for feature support in router.
	const featureCallCanceling = "call_canceling"
	if !callee.HasFeature("dealer", featureCallCanceling) {
		t.Error("Dealer does not have", featureCallCanceling, "feature")
	}

	invkCanceled := make(chan struct{}, 1)
	// Register procedure that waits.
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		invkCanceled <- struct{}{}
		return &client.InvokeResult{Err: wamp.ErrCanceled}
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
	ctx, cancel := context.WithCancel(context.Background())
	// Calling the procedure, should block.
	go func() {
		callArgs := wamp.List{73}
		_, e := caller.Call(ctx, procName, nil, callArgs, nil, "killnowait")
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

func TestRPCTimeoutCall(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect callee session.
	callee, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Check for feature support in router.
	const featureCallTimeout = "call_timeout"
	if !callee.HasFeature("dealer", featureCallTimeout) {
		t.Error("Dealer does not have", featureCallTimeout, "feature")
	}

	invkCanceled := make(chan struct{}, 1)
	// Register procedure that waits.
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		invkCanceled <- struct{}{}
		return &client.InvokeResult{Err: wamp.ErrCanceled}
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
		_, e := caller.Call(ctx, procName, opts, callArgs, nil, "killnowait")
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
