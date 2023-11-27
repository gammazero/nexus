package aat_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dtegapp/nexus/v3/client"
	"github.com/dtegapp/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

const (
	metaOnJoin  = string(wamp.MetaEventSessionOnJoin)
	metaOnLeave = string(wamp.MetaEventSessionOnLeave)

	metaCount = string(wamp.MetaProcSessionCount)
	metaList  = string(wamp.MetaProcSessionList)
	metaGet   = string(wamp.MetaProcSessionGet)
)

func TestMetaEventOnJoin(t *testing.T) {
	checkGoLeaks(t)
	// Connect subscriber session.
	subscriber := connectClient(t)

	// Check for feature support in router.
	has := subscriber.HasFeature(wamp.RoleBroker, wamp.FeatureSessionMetaAPI)
	require.Truef(t, has, "Broker does not support %s", wamp.FeatureSessionMetaAPI)
	has = subscriber.HasFeature(wamp.RoleDealer, wamp.FeatureSessionMetaAPI)
	require.Truef(t, has, "Dealer does not support %s", wamp.FeatureSessionMetaAPI)

	// Subscribe to event.
	onJoinEvents := make(chan *wamp.Event)
	err := subscriber.SubscribeChan(metaOnJoin, onJoinEvents, nil)
	require.NoError(t, err)

	// Wait for any event from subscriber joining.
	var timeout bool
	for !timeout {
		select {
		case <-onJoinEvents:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	// Connect client to generate wamp.session.on_join event.
	sess := connectClient(t)

	var onJoinID wamp.ID
	onJoin := func(event *wamp.Event) error {
		args := event.Arguments
		if len(args) == 0 {
			return errors.New("missing argument")
		}
		details := wamp.NormalizeDict(args[0])
		if details == nil {
			return errors.New("argument was not wamp.Dict")
		}
		onJoinID, _ = wamp.AsID(details["session"])
		return nil
	}

	// Make sure the event was received.
	select {
	case event := <-onJoinEvents:
		err = onJoin(event)
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		require.FailNow(t, "did not get published event")
	}

	require.Equal(t, sess.ID(), onJoinID, "meta even had wrong session ID")
}

func TestMetaEventOnLeave(t *testing.T) {
	checkGoLeaks(t)
	// Connect subscriber session.
	subscriber := connectClient(t)

	argsChan := make(chan wamp.List)
	eventHandler := func(event *wamp.Event) {
		argsChan <- event.Arguments
	}

	// Subscribe to event.
	err := subscriber.Subscribe(metaOnLeave, eventHandler, nil)
	require.NoError(t, err)

	// Connect a session.
	sess := connectClient(t)

	// Wait for any events from previously closed clients.
	var timeout bool
	for !timeout {
		select {
		case <-argsChan:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	sid := sess.ID()

	// Disconnect client to generate wamp.session.on_leave event.
	err = sess.Close()
	require.NoError(t, err)

	// Make sure the event was received.
	var eventArgs wamp.List
	select {
	case eventArgs = <-argsChan:
	case <-time.After(200 * time.Millisecond):
		require.FailNow(t, "did not get published event")
	}

	// Check that all expected arguments are returned in on_leave event.
	require.Equal(t, 3, len(eventArgs))
	onLeaveID, _ := wamp.AsID(eventArgs[0])
	require.Equal(t, sid, onLeaveID)
	authid, _ := wamp.AsString(eventArgs[1])
	require.NotZero(t, len(authid), "expected non-empty authid")
	authrole, _ := wamp.AsString(eventArgs[2])
	if authrole != "trusted" && authrole != "anonymous" {
		require.FailNowf(t, "expected authrole of trusted or anonymous, got: %s", authrole)
	}
}

func TestMetaProcSessionCount(t *testing.T) {
	checkGoLeaks(t)
	// Connect caller.
	caller := connectClient(t)
	// Connect subscriber session.
	subscriber := connectClient(t)

	// Subscribe to on_join and on_leave events.
	onJoinEvents := make(chan *wamp.Event)
	err := subscriber.SubscribeChan(metaOnJoin, onJoinEvents, nil)
	require.NoError(t, err)
	onLeaveEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnLeave, onLeaveEvents, nil)
	require.NoError(t, err)

	// Wait for any events from previously closed clients.
	var timeout bool
	for !timeout {
		select {
		case <-onLeaveEvents:
		case <-onJoinEvents:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	// Call meta procedure to get session count.
	ctx := context.Background()
	result, err := caller.Call(ctx, metaCount, nil, nil, nil, nil)
	require.NoError(t, err)
	firstCount, ok := wamp.AsInt64(result.Arguments[0])
	require.True(t, ok, "Could not convert result to int64")
	require.NotZero(t, firstCount, "Session count should not be zero")

	// Connect client to increment session count.
	sess := connectClient(t)
	// Wait for router to register new session.
	select {
	case <-onJoinEvents:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Timed out waiting for router to register new session")
	}

	// Call meta procedure to get session count.
	result, err = caller.Call(ctx, metaCount, nil, nil, nil, nil)
	require.NoError(t, err)
	count, ok := wamp.AsInt64(result.Arguments[0])
	require.True(t, ok, "Could not convert result to int64")
	require.Equal(t, firstCount+1, count, "Session count should one more the previous")

	err = sess.Close()
	require.NoError(t, err)
	// Wait for router to register client leaving.
	select {
	case <-onLeaveEvents:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Timed out waiting for router to register client leaving")
	}

	// Call meta procedure to get session count.
	result, err = caller.Call(ctx, metaCount, nil, nil, nil, nil)
	require.NoError(t, err)
	count, ok = wamp.AsInt64(result.Arguments[0])
	require.True(t, ok, "Could not convert result to int64")
	require.Equal(t, firstCount, count, "Session count should be same as first")
}

func TestMetaProcSessionList(t *testing.T) {
	checkGoLeaks(t)
	// Connect a client to session.
	sess := connectClient(t)
	// Connect caller.
	caller := connectClient(t)
	// Connect subscriber session.
	subscriber := connectClient(t)

	// Subscribe to on_leave event.
	eventChan := make(chan *wamp.Event)
	err := subscriber.SubscribeChan(metaOnLeave, eventChan, nil)
	require.NoError(t, err)

	// Wait for any events from previously closed clients.
	var timeout bool
	for !timeout {
		select {
		case <-eventChan:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	// Call meta procedure to get session list.
	ctx := context.Background()
	result, err := caller.Call(ctx, metaList, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotZero(t, len(result.Arguments), "Missing result argument")
	list, ok := wamp.AsList(result.Arguments[0])
	require.True(t, ok, "Could not convert result to wamp.List")
	require.NotZero(t, len(list), "Session list should not be empty")
	var found bool
	for i := range list {
		id, _ := wamp.AsID(list[i])
		if id == sess.ID() {
			found = true
			break
		}
	}
	require.True(t, found, "Missing session ID from session list")
	firstLen := len(list)

	sid := sess.ID()

	err = sess.Close()
	require.NoError(t, err)
	// Wait for router to register client leaving.
	<-eventChan

	// Call meta procedure to get session list.
	result, err = caller.Call(ctx, metaList, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotZero(t, len(result.Arguments), "Missing result argument")
	list, ok = wamp.AsList(result.Arguments[0])
	require.True(t, ok, "Could not convert result to wamp.List")
	require.Equal(t, firstLen-1, len(list), "Session list should be one less than previous")
	found = false
	for i := range list {
		id, _ := wamp.AsID(list[i])
		if id == sid {
			found = true
			break
		}
	}
	require.False(t, found, "Session ID should not be in session list")
}

func TestMetaProcSessionGet(t *testing.T) {
	checkGoLeaks(t)
	// Connect a client to session.
	sess := connectClient(t)
	// Connect caller.
	caller := connectClient(t)
	// Connect subscriber session.
	subscriber := connectClient(t)

	// Subscribe to on_leave event.
	eventChan := make(chan *wamp.Event)
	err := subscriber.SubscribeChan(metaOnLeave, eventChan, nil)
	require.NoError(t, err)

	// Call meta procedure to get session info.
	ctx := context.Background()
	args := wamp.List{sess.ID()}
	result, err := caller.Call(ctx, metaGet, nil, args, nil, nil)
	require.NoError(t, err)
	require.NotZero(t, len(result.Arguments), "Missing result argument")
	dict, ok := wamp.AsDict(result.Arguments[0])
	require.True(t, ok, "Could not convert result to wamp.Dict")
	resultID, _ := wamp.AsID(dict["session"])
	require.Equal(t, sess.ID(), resultID, "Wrong session ID in result")
	for _, attr := range []string{"authid", "authrole", "authmethod", "authprovider"} {
		_, ok = dict[attr]
		require.Truef(t, ok, "Result missing: %s", attr)
	}

	// Wait for any events from previously closed clients.
	var timeout bool
	for !timeout {
		select {
		case <-eventChan:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	sid := sess.ID()

	err = sess.Close()
	require.NoError(t, err)
	// Wait for router to register client leaving.
	<-eventChan

	// Call meta procedure to get session list.
	result, err = caller.Call(ctx, metaGet, nil, wamp.List{sid}, nil, nil)
	require.Error(t, err)

	var rpcErr client.RPCError
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, wamp.ErrNoSuchSession, rpcErr.Err.Error)
}
