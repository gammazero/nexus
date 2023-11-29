package aat_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

func TestRPCRegisterAndCall(t *testing.T) {
	checkGoLeaks(t)
	// Connect callee session.
	callee := connectClient(t)

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
	err := callee.Register(procName, handler, nil)
	require.NoError(t, err)

	// Connect caller session.
	caller := connectClient(t)
	// Connect second caller session.
	caller2 := connectClient(t)

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
		require.NoErrorf(t, errs[i], "Caller %d failed to call procedure", i)
		sum, ok := wamp.AsInt64(results[i].Arguments[0])
		require.Truef(t, ok, "Could not convert result %d to int64", i)
		require.Equal(t, int64(55), sum)
	}

	// Test unregister.
	err = callee.Unregister(procName)
	require.NoError(t, err)
}

func TestRPCCallUnregistered(t *testing.T) {
	checkGoLeaks(t)
	// Connect caller session.
	caller := connectClient(t)

	// Test calling unregistered procedure.
	callArgs := wamp.List{555}
	ctx := context.Background()
	result, err := caller.Call(ctx, "NotRegistered", nil, callArgs, nil, nil)
	require.Error(t, err, "expected error calling unregistered procedure")
	require.Nil(t, result)
}

func TestRPCUnregisterUnregistered(t *testing.T) {
	checkGoLeaks(t)
	// Connect caller session.
	callee := connectClient(t)
	// Test unregister unregistered procedure.
	err := callee.Unregister("NotHere")
	require.Error(t, err)
}

func TestRPCCancelCall(t *testing.T) {
	checkGoLeaks(t)
	// Connect callee session.
	callee := connectClient(t)

	// Check for feature support in router.
	has := callee.HasFeature(wamp.RoleDealer, wamp.FeatureCallCanceling)
	require.Truef(t, has, "Dealer does not support %s", wamp.FeatureCallCanceling)

	invkCanceled := make(chan struct{}, 1)
	// Register procedure that waits.
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		invkCanceled <- struct{}{}
		return client.InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	err := callee.Register(procName, handler, nil)
	require.NoError(t, err)

	// Connect caller session.
	caller := connectClient(t)

	err = caller.SetCallCancelMode(wamp.CancelModeKillNoWait)
	require.NoError(t, err)

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
	case <-errChan:
		require.FailNow(t, "call should have been blocked")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()

	// Make sure the call is canceled on caller side.
	select {
	case err = <-errChan:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "call should have been canceled")
	}

	// Make sure the invocation is canceled on callee side.
	select {
	case <-invkCanceled:
	case <-time.After(time.Second):
		require.FailNow(t, "invocation should have been canceled")
	}

	err = callee.Unregister(procName)
	require.NoError(t, err)
}

func TestRPCTimeoutCall(t *testing.T) {
	checkGoLeaks(t)
	// Connect callee session.
	callee := connectClient(t)

	// Check for feature support in router.
	has := callee.HasFeature(wamp.RoleDealer, wamp.FeatureCallTimeout)
	require.Truef(t, has, "Dealer does not support %s", wamp.FeatureCallTimeout)

	invkCanceled := make(chan struct{}, 1)
	// Register procedure that waits.
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		invkCanceled <- struct{}{}
		return client.InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	err := callee.Register(procName, handler, nil)
	require.NoError(t, err)

	// Connect caller session.
	caller := connectClient(t)

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
	case <-errChan:
		require.FailNow(t, "call should have been blocked")
	case <-time.After(200 * time.Millisecond):
	}

	// Make sure the call is canceled on caller side.
	select {
	case err = <-errChan:
		require.Error(t, err)
		var rpcErr client.RPCError
		require.ErrorAs(t, err, &rpcErr)
		require.Equal(t, wamp.ErrTimeout, rpcErr.Err.Error)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "call should have been canceled")
	}

	// Make sure the invocation is canceled on callee side.
	select {
	case <-invkCanceled:
	case <-time.After(time.Second):
		require.FailNow(t, "invocation should have been canceled")
	}

	var rpcError client.RPCError
	require.ErrorAs(t, err, &rpcError)
	require.Equal(t, wamp.ErrTimeout, rpcError.Err.Error)

	err = callee.Unregister(procName)
	require.NoError(t, err)
}

func TestRPCResponseRouting(t *testing.T) {
	checkGoLeaks(t)
	// Connect callee session.
	callee := connectClient(t)

	respond := make(chan struct{})
	ready := sync.WaitGroup{}
	ready.Add(2)

	// Register procedure "hello"
	hello := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		ready.Done()
		<-respond
		return client.InvokeResult{Args: wamp.List{"HELLO"}}
	}
	err := callee.Register("hello", hello, nil)
	require.NoError(t, err)

	// Register procedure "world"
	world := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		ready.Done()
		<-respond
		return client.InvokeResult{Args: wamp.List{"WORLD"}}
	}
	// Register procedure "hello"
	err = callee.Register("world", world, nil)
	require.NoError(t, err)

	// Connect hello caller session.
	callerHello := connectClient(t)
	// Connect world caller session.
	callerWorld := connectClient(t)

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
			require.Equal(t, "HELLO", result.Arguments[0])
		case result = <-rc2:
			require.Equal(t, "WORLD", result.Arguments[0])
		case err = <-ec1:
			require.NoError(t, err, "Error calling 'hello'")
		case err = <-ec2:
			require.NoError(t, err, "Error calling 'world'")
		}
	}
}
