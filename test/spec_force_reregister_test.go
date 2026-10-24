package test_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

// TestSpecForceReregister exercises the WAMP advanced-profile
// force_reregister option end-to-end across whichever transport the
// test matrix is currently running on. The second callee must forcibly
// take over the procedure from the first, and subsequent CALLs must be
// routed to the new callee.
func TestSpecForceReregister(t *testing.T) {
	checkGoLeaks(t)

	const procName = "nexus.test.force_reregister"

	callee1 := connectClient(t)
	callee2 := connectClient(t)
	caller := connectClient(t)

	var hits1, hits2 atomic.Int32
	handler1 := func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
		hits1.Add(1)
		return client.InvokeResult{Args: wamp.List{"callee1"}}
	}
	handler2 := func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
		hits2.Add(1)
		return client.InvokeResult{Args: wamp.List{"callee2"}}
	}

	require.NoError(t, callee1.Register(procName, handler1, nil),
		"callee1 should be able to register the procedure")

	// Second REGISTER without force_reregister must fail with
	// procedure_already_exists.
	err := callee2.Register(procName, handler2, nil)
	require.Error(t, err,
		"second register without force_reregister must fail")

	// Now callee2 force-reregisters; should succeed and evict callee1.
	require.NoError(t,
		callee2.Register(procName, handler2,
			wamp.Dict{wamp.OptForceReregister: true}),
		"force_reregister must let callee2 take over")

	// A CALL must now be routed to callee2.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := caller.Call(ctx, procName, nil, nil, nil, nil)
	require.NoError(t, err, "post-force_reregister CALL should succeed")
	require.NotEmpty(t, res.Arguments)
	require.Equal(t, "callee2", res.Arguments[0],
		"force_reregister should route subsequent CALLs to the new callee")
	require.Equal(t, int32(1), hits2.Load(), "callee2 should have served the call")
	require.Equal(t, int32(0), hits1.Load(), "callee1 must not have served the call")
}
