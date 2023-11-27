package aat_test

import (
	"context"
	"testing"
	"time"

	"github.com/dtegapp/nexus/v3/client"
	"github.com/dtegapp/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

const (
	metaKill           = string(wamp.MetaProcSessionKill)
	metaKillByAuthid   = string(wamp.MetaProcSessionKillByAuthid)
	metaKillByAuthrole = string(wamp.MetaProcSessionKillByAuthrole)
	metaKillAll        = string(wamp.MetaProcSessionKillAll)

	metaModifyDetails = string(wamp.MetaProcSessionModifyDetails)
)

func TestSessionKill(t *testing.T) {
	checkGoLeaks(t)

	cli1 := connectClient(t)
	cli2 := connectClient(t)
	cli3 := connectClient(t)

	reason := wamp.URI("test.session.kill")
	message := "this is a test"

	// Call meta procedure to kill a client-3
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	args := wamp.List{cli3.ID()}
	kwArgs := wamp.Dict{"reason": reason, "message": message}
	result, err := cli1.Call(ctx, metaKill, nil, args, kwArgs, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check that client-3 was booted.
	select {
	case <-cli3.Done():
	case <-time.After(time.Second):
		require.FailNow(t, "Client 3 did not shutdown")
	}
	// Check for expected GOODBYE message.
	goodbye := cli3.RouterGoodbye()
	require.NotNil(t, goodbye, "Did not receive goodbye from router")
	require.Equal(t, reason, goodbye.Reason)
	gm, ok := wamp.AsString(goodbye.Details["message"])
	require.True(t, ok, "Expected message in GOODBYE")
	require.Equal(t, message, gm, "Did not get expected goodbye message")

	// Check that client-2 is still connected.
	select {
	case <-cli2.Done():
		require.FailNow(t, "Client 2 should still be connected")
	default:
	}

	// Test that trying to kill self receives error.
	args = wamp.List{cli1.ID}
	_, err = cli1.Call(ctx, metaKill, nil, args, kwArgs, nil)
	require.Error(t, err)
	var rpcErr client.RPCError
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, wamp.ErrNoSuchSession, rpcErr.Err.Error)

	// Test that killing a sesson that does not exist works correctly.
	ctx, c2 := context.WithTimeout(context.Background(), time.Second)
	defer c2()
	args = wamp.List{wamp.ID(12345)}
	kwArgs = wamp.Dict{"reason": reason, "message": message}
	_, err = cli1.Call(ctx, metaKill, nil, args, kwArgs, nil)
	require.Error(t, err)
	require.ErrorAs(t, err, &rpcErr)
}

func TestSessionKillAll(t *testing.T) {
	checkGoLeaks(t)

	cli1 := connectClient(t)
	cli2 := connectClient(t)
	cli3 := connectClient(t)

	reason := wamp.URI("test.session.kill")
	message := "this is a test"

	// Call meta procedure to kill a client-3
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	kwArgs := wamp.Dict{"reason": reason, "message": message}
	result, err := cli1.Call(ctx, metaKillAll, nil, nil, kwArgs, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check that client-3 was booted.
	select {
	case <-cli3.Done():
	case <-time.After(3 * time.Second):
		require.FailNow(t, "Client 3 did not shutdown")
	}
	// Check for expected GOODBYE message.
	goodbye := cli3.RouterGoodbye()
	require.NotNil(t, goodbye, "Did not receive goodbye from router")
	require.Equal(t, reason, goodbye.Reason)
	gm, ok := wamp.AsString(goodbye.Details["message"])
	require.True(t, ok, "Expected message in GOODBYE")
	require.Equal(t, message, gm)

	// Check that client-2 was booted.
	select {
	case <-cli2.Done():
	case <-time.After(3 * time.Second):
		require.FailNow(t, "Client 2 did not shutdown")
	}

	// Test that client-1 still connected.
	select {
	case <-cli1.Done():
		require.FailNow(t, "Client 1 should still be connected")
	default:
	}

	cli4 := connectClient(t)

	// Call killall again, with no reason.
	ctx, c2 := context.WithTimeout(context.Background(), time.Second)
	defer c2()
	result, err = cli1.Call(ctx, metaKillAll, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check that client-4 was booted.
	select {
	case <-cli4.Done():
	case <-time.After(3 * time.Second):
		require.FailNow(t, "Client 4 did not shutdown")
	}

	// Check for expected GOODBYE message.
	goodbye = cli4.RouterGoodbye()
	require.NotNil(t, goodbye, "Did not receive goodbye from router")
	require.Equal(t, wamp.CloseNormal, goodbye.Reason)
	_, ok = goodbye.Details["message"]
	require.False(t, ok, "Should not have received message in GOODBYE.Details")
}

func TestSessionModifyDetails(t *testing.T) {
	checkGoLeaks(t)

	cli := connectClient(t)

	// Call meta procedure to modify details.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	delta := wamp.Dict{"pi": 3.14, "authid": "bob"}
	args := wamp.List{cli.ID(), delta}
	result, err := cli.Call(ctx, metaModifyDetails, nil, args, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result, "Did not receive result")

	// Call session meta-procedure to get session information.
	ctx = context.Background()
	args = wamp.List{cli.ID()}
	result, err = cli.Call(ctx, metaGet, nil, args, nil, nil)
	require.NoError(t, err)
	require.NotZero(t, len(result.Arguments), "Missing result argument")
	details, ok := wamp.AsDict(result.Arguments[0])
	require.True(t, ok, "Could not convert result to wamp.Dict")
	pi, _ := wamp.AsFloat64(details["pi"])
	require.Equal(t, 3.14, pi)
	authid, _ := wamp.AsString(details["authid"])
	require.Equal(t, "bob", authid)
}

func TestSessionKillDeadlock(t *testing.T) {
	// Test fix for co-recursive deadlock between dealer goroutine and meta
	// session inbound message handler (issue #180)

	remoteCli := connectClient(t)

	// Register procedure
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Args: wamp.List{"done"}}
	}
	const procName = "dostuff"
	err := remoteCli.Register(procName, handler, nil)
	require.NoError(t, err)

	localCli := connectClient(t)

	// Subscribe to event.  Note this needs to be a buffered channel so that
	// event can be "handled" while the test is waiting for a response.
	onUnregEvents := make(chan *wamp.Event, 1)
	err = localCli.SubscribeChan(string(wamp.MetaEventRegOnUnregister), onUnregEvents, nil)
	require.NoError(t, err)

	reason := wamp.URI("test.session.kill")
	message := "this is a test"

	// Call meta procedure to kill client-2
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := wamp.List{remoteCli.ID()}
	kwArgs := wamp.Dict{"reason": reason, "message": message}
	result, err := localCli.Call(ctx, metaKill, nil, args, kwArgs, nil)
	require.NoError(t, err)
	require.NotNil(t, result, "Did not receive result")

	// Check that remote client was booted.
	select {
	case <-remoteCli.Done():
	case <-time.After(time.Second):
		require.FailNow(t, "remote client did not shutdown")
	}
	// Check for expected GOODBYE message.
	goodbye := remoteCli.RouterGoodbye()
	if goodbye != nil {
		require.Equal(t, reason, goodbye.Reason)
		gm, ok := wamp.AsString(goodbye.Details["message"])
		require.True(t, ok, "Expected message in GOODBYE")
		require.Equal(t, message, gm)
	}
	select {
	case <-onUnregEvents:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Timed out waiting for local client to get on unregister event")
	}
}
