package test_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
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
	for range 5 {
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
	for range 5 {
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

// TestSpecRPCSharedFirst verifies invoke=first: every call lands on the
// first-registered callee until it leaves (spec §14.3.9).
func TestSpecRPCSharedFirst(t *testing.T) {
	checkGoLeaks(t)
	const procName = "shared.spec.first"
	options := wamp.SetOption(nil, wamp.OptInvoke, wamp.InvokeFirst)

	callees := make([]*client.Client, 4)
	for i := range callees {
		callees[i] = connectClient(t)
		idx := int64(i)
		require.NoError(t, callees[i].Register(procName, func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
			return client.InvokeResult{Args: wamp.List{idx}}
		}, options))
	}

	caller := connectClient(t)
	for range 10 {
		res, err := caller.Call(context.Background(), procName, options, nil, nil, nil)
		require.NoError(t, err)
		got, _ := wamp.AsInt64(res.Arguments[0])
		require.Equal(t, int64(0), got, "invoke=first must always hit callee[0]")
	}

	// Failover: drop callee[0]; subsequent calls must land on callee[1].
	require.NoError(t, callees[0].Unregister(procName))
	for range 5 {
		res, err := caller.Call(context.Background(), procName, options, nil, nil, nil)
		require.NoError(t, err)
		got, _ := wamp.AsInt64(res.Arguments[0])
		require.Equal(t, int64(1), got, "after callee[0] removed, invoke=first must hit callee[1]")
	}

	for _, c := range callees[1:] {
		require.NoError(t, c.Unregister(procName))
	}
}

// TestSpecRPCSharedLast verifies invoke=last: every call lands on the
// most-recently-registered callee.
func TestSpecRPCSharedLast(t *testing.T) {
	checkGoLeaks(t)
	const procName = "shared.spec.last"
	options := wamp.SetOption(nil, wamp.OptInvoke, wamp.InvokeLast)

	const n = 4
	callees := make([]*client.Client, n)
	for i := range callees {
		callees[i] = connectClient(t)
		idx := int64(i)
		require.NoError(t, callees[i].Register(procName, func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
			return client.InvokeResult{Args: wamp.List{idx}}
		}, options))
	}

	caller := connectClient(t)
	for range 10 {
		res, err := caller.Call(context.Background(), procName, options, nil, nil, nil)
		require.NoError(t, err)
		got, _ := wamp.AsInt64(res.Arguments[0])
		require.Equal(t, int64(n-1), got, "invoke=last must always hit the most recent callee")
	}

	// Failover: drop the last callee; calls must land on the new last (n-2).
	require.NoError(t, callees[n-1].Unregister(procName))
	for range 5 {
		res, err := caller.Call(context.Background(), procName, options, nil, nil, nil)
		require.NoError(t, err)
		got, _ := wamp.AsInt64(res.Arguments[0])
		require.Equal(t, int64(n-2), got)
	}

	for _, c := range callees[:n-1] {
		require.NoError(t, c.Unregister(procName))
	}
}

// TestSpecRPCSharedSingleConflict verifies the default (no shared) behavior:
// a second REGISTER on the same procedure is rejected, regardless of whether
// the second one specifies an invoke option.
func TestSpecRPCSharedSingleConflict(t *testing.T) {
	checkGoLeaks(t)
	const procName = "shared.spec.single"

	callee1 := connectClient(t)
	require.NoError(t, callee1.Register(procName, func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{}
	}, nil))

	// Second register with shared option must fail because callee1 used single.
	callee2 := connectClient(t)
	options := wamp.SetOption(nil, wamp.OptInvoke, wamp.InvokeRoundRobin)
	err := callee2.Register(procName, func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{}
	}, options)
	require.Error(t, err, "registering shared on top of single must fail")

	require.NoError(t, callee1.Unregister(procName))
}

// TestSpecRPCSharedPolicyMismatch verifies registering with a different
// invoke policy than the existing registration is rejected.
func TestSpecRPCSharedPolicyMismatch(t *testing.T) {
	checkGoLeaks(t)
	const procName = "shared.spec.mismatch"

	rrOpts := wamp.SetOption(nil, wamp.OptInvoke, wamp.InvokeRoundRobin)
	callee1 := connectClient(t)
	require.NoError(t, callee1.Register(procName, func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{}
	}, rrOpts))

	// Try to register with a conflicting policy.
	firstOpts := wamp.SetOption(nil, wamp.OptInvoke, wamp.InvokeFirst)
	callee2 := connectClient(t)
	err := callee2.Register(procName, func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{}
	}, firstOpts)
	require.Error(t, err, "mismatched invoke policy must fail")

	require.NoError(t, callee1.Unregister(procName))
}
