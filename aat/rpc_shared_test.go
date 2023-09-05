package aat_test

import (
	"context"
	"testing"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

func TestRPCSharedRoundRobin(t *testing.T) {
	const procName = "shared.test.procedure"
	options := wamp.SetOption(nil, "invoke", "roundrobin")

	// Connect callee1 and register test procedure.
	callee1 := connectClient(t)

	// Check for feature support in router.
	has := callee1.HasFeature(wamp.RoleDealer, wamp.FeatureSharedReg)
	require.Truef(t, has, "Dealer does not support %s", wamp.FeatureSharedReg)

	testProc1 := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Args: wamp.List{1}}
	}
	err := callee1.Register(procName, testProc1, options)
	require.NoError(t, err)

	// Connect callee2 and register test procedure.
	callee2 := connectClient(t)
	testProc2 := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Args: wamp.List{2}}
	}
	err = callee2.Register(procName, testProc2, options)
	require.NoError(t, err)

	// Connect callee3 and register test procedure.
	callee3 := connectClient(t)
	testProc3 := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Args: wamp.List{3}}
	}
	err = callee3.Register(procName, testProc3, options)
	require.NoError(t, err)

	// Connect caller session.
	caller := connectClient(t)

	expect := int64(1)
	var result *wamp.Result
	for i := 0; i < 5; i++ {
		// Test calling the procedure - expect callee1-3
		ctx := context.Background()
		result, err = caller.Call(ctx, procName, options, nil, nil, nil)
		require.NoError(t, err)
		num, _ := wamp.AsInt64(result.Arguments[0])
		require.Equal(t, expect, num)
		if expect == 3 {
			expect = 1
		} else {
			expect++
		}
	}

	// Unregister callee2 and make sure round robin still works.
	err = callee2.Unregister(procName)
	require.NoError(t, err)

	expect = int64(1)
	for i := 0; i < 5; i++ {
		// Test calling the procedure - expect callee1-3
		ctx := context.Background()
		result, err = caller.Call(ctx, procName, options, nil, nil, nil)
		require.NoError(t, err)
		num, _ := wamp.AsInt64(result.Arguments[0])
		require.Equal(t, expect, num)
		if expect == 1 {
			expect = 3
		} else {
			expect = 1
		}
	}

	// Test unregister.
	require.NoError(t, callee1.Unregister(procName))
	require.NoError(t, callee3.Unregister(procName))
}

func TestRPCSharedRandom(t *testing.T) {
	const procName = "shared.test.procedure"
	options := wamp.SetOption(nil, "invoke", "random")

	// Connect callee1 and register test procedure.
	callee1 := connectClient(t)
	testProc1 := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Args: wamp.List{1}}
	}
	err := callee1.Register(procName, testProc1, options)
	require.NoError(t, err)

	// Connect callee2 and register test procedure.
	callee2 := connectClient(t)
	testProc2 := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Args: wamp.List{2}}
	}
	err = callee2.Register(procName, testProc2, options)
	require.NoError(t, err)

	// Connect callee3 and register test procedure.
	callee3 := connectClient(t)
	testProc3 := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Args: wamp.List{3}}
	}
	err = callee3.Register(procName, testProc3, options)
	require.NoError(t, err)

	// Connect caller session.
	caller := connectClient(t)

	var called1, called2, called3 int
	var i int
	var result *wamp.Result
	for i = 0; i < 20; i++ {
		// Test calling the procedure - expect callee1-3
		ctx := context.Background()
		result, err = caller.Call(ctx, procName, options, nil, nil, nil)
		require.NoError(t, err)
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
		panic("only called one callee with random distribution")
	}

	// Test unregister.
	require.NoError(t, callee1.Unregister(procName))
	require.NoError(t, callee2.Unregister(procName))
	require.NoError(t, callee3.Unregister(procName))
}
