package aat_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

const (
	metaOnCreate      = string(wamp.MetaEventSubOnCreate)
	metaOnSubscribe   = string(wamp.MetaEventSubOnSubscribe)
	metaOnUnsubscribe = string(wamp.MetaEventSubOnUnsubscribe)
	metaOnDelete      = string(wamp.MetaEventSubOnDelete)

	metaSubGet = string(wamp.MetaProcSubGet)
)

func TestMetaEventOnCreateOnSubscribe(t *testing.T) {
	// Connect subscriber session.
	subscriber := connectClient(t)

	// Check for feature support in router.
	has := subscriber.HasFeature(wamp.RoleBroker, wamp.FeatureSubMetaAPI)
	require.Truef(t, has, "Broker does not support %s", wamp.FeatureSubMetaAPI)

	// Subscribe to on_create meta event.
	onCreateEvents := make(chan *wamp.Event)
	err := subscriber.SubscribeChan(metaOnCreate, onCreateEvents, nil)
	require.NoError(t, err)

	// Subscribe to on_subscribe meta event.
	onSubEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnSubscribe, onSubEvents, nil)
	require.NoError(t, err)

	select {
	case <-onCreateEvents:
		require.FailNow(t, "Received on_create when subscribing to meta event")
	case <-onSubEvents:
		require.FailNow(t, "Received on_subscribe when subscribing to meta event")
	case <-time.After(200 * time.Millisecond):
	}

	// Connect another client.
	sess := connectClient(t)

	// Subscribe to something to generate meta on_subcribe event
	nullHandler := func(_ *wamp.Event) {}
	err = sess.Subscribe("some.topic", nullHandler, nil)
	require.NoError(t, err)
	subID, ok := sess.SubscriptionID("some.topic")
	require.True(t, ok, "client does not have subscription ID")

	var onCreateID, onCreateSessID wamp.ID
	onCreate := func(event *wamp.Event) error {
		args := event.Arguments
		if len(args) != 2 {
			return errors.New("wrong number of arguments")
		}
		dict, ok := wamp.AsDict(args[1])
		if !ok {
			return errors.New("arg 1 was not wamp.Dict")
		}
		onCreateSessID, ok = wamp.AsID(args[0])
		if !ok {
			return errors.New("argument 0 (session) was not wamp.ID")
		}
		onCreateID, _ = wamp.AsID(dict["id"])
		if u, _ := wamp.AsURI(dict["uri"]); u != wamp.URI("some.topic") {
			return fmt.Errorf(
				"on_create had wrong topic, got '%v' want 'some.topic'", u)
		}
		if s, _ := wamp.AsString(dict["created"]); s == "" {
			return errors.New("on_create missing created time")
		}
		return nil
	}

	// Make sure the on_create event was received.
	var event *wamp.Event
	select {
	case event = <-onCreateEvents:
		err = onCreate(event)
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		require.FailNowf(t, "did not get %s event", metaOnCreate)
	}
	require.Equal(t, sess.ID(), onCreateSessID, "meta even had wrong session ID")
	require.Equal(t, subID, onCreateID, "meta event did not return expected subscription ID")

	var onSubSubID, onSubSessID wamp.ID
	onSub := func(event *wamp.Event) error {
		args := event.Arguments
		if len(args) != 2 {
			return errors.New("wrong number of arguments")
		}
		var ok bool
		onSubSessID, ok = wamp.AsID(args[0])
		if !ok {
			return errors.New("argument 0 (session) was not wamp.ID")
		}
		onSubSubID, ok = wamp.AsID(args[1])
		if !ok {
			return errors.New("argument 1 (subscription) was not wamp.ID")
		}
		return nil
	}

	// Make sure the on_subscribe event was received.
	select {
	case event = <-onSubEvents:
		err = onSub(event)
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		require.FailNowf(t, "did not get %s event", metaOnSubscribe)
	}
	require.Equal(t, sess.ID(), onSubSessID, "meta even had wrong session ID")
	require.Equal(t, subID, onSubSubID, "meta event did not return expected subscription ID")

	err = sess.Unsubscribe("some.topic")
	require.NoError(t, err)

	err = subscriber.Unsubscribe(metaOnSubscribe)
	require.NoError(t, err)
}

func TestMetaEventOnUnsubscribeOnDelete(t *testing.T) {
	// Connect subscriber session.
	subscriber := connectClient(t)

	// Subscribe to on_unsubscribe event.
	onUnsubEvents := make(chan *wamp.Event)
	err := subscriber.SubscribeChan(metaOnUnsubscribe, onUnsubEvents, nil)
	require.NoError(t, err)

	// Subscribe to on_delete event.
	onDelEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnDelete, onDelEvents, nil)
	require.NoError(t, err)

	select {
	case <-onUnsubEvents:
		require.FailNow(t, "Received on_unsubscribe when unsubscribing to meta event")
	case <-onDelEvents:
		require.FailNow(t, "Received on_delete when unsubscribing to meta event")
	case <-time.After(200 * time.Millisecond):
	}

	// Connect another client.
	sess := connectClient(t)

	// Subscribe to something and then unsubscribe to generate meta on_subcribe
	// event.
	nullHandler := func(_ *wamp.Event) {}
	err = sess.Subscribe("some.topic", nullHandler, nil)
	require.NoError(t, err)
	subID, ok := sess.SubscriptionID("some.topic")
	require.True(t, ok, "client does not have subscription ID")
	err = sess.Unsubscribe("some.topic")
	require.NoError(t, err)

	var onUnsubSubID, onUnsubSessID wamp.ID
	onUnsub := func(event *wamp.Event) error {
		args := event.Arguments
		if len(args) != 2 {
			return errors.New("wrong number of arguments")
		}
		var ok bool
		onUnsubSessID, ok = wamp.AsID(args[0])
		if !ok {
			return errors.New("argument 0 (session) was not wamp.ID")
		}
		onUnsubSubID, ok = wamp.AsID(args[1])
		if !ok {
			return errors.New("argument 1 (subscription) was not wamp.ID")
		}
		return nil
	}

	// Make sure the unsubscribe event was received.
	var event *wamp.Event
	select {
	case event := <-onUnsubEvents:
		err = onUnsub(event)
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		require.FailNowf(t, "did not get %s event", metaOnUnsubscribe)
	}
	require.Equal(t, sess.ID(), onUnsubSessID, "meta even had wrong session ID")
	require.Equal(t, subID, onUnsubSubID, "meta event did not return expected subscription ID")

	var onDelSubID, onDelSessID wamp.ID
	onDel := func(event *wamp.Event) error {
		args := event.Arguments
		if len(args) != 2 {
			return errors.New("wrong number of arguments")
		}
		var ok bool
		onDelSessID, ok = wamp.AsID(args[0])
		if !ok {
			return errors.New("argument 0 (session) was not wamp.ID")
		}
		onDelSubID, ok = wamp.AsID(args[1])
		if !ok {
			return errors.New("argument 1 (subscription) was not wamp.ID")
		}
		return nil
	}

	// Make sure the delete event was received.
	select {
	case event = <-onDelEvents:
		err = onDel(event)
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		require.FailNowf(t, "did not get %s event", metaOnDelete)
	}
	require.Equal(t, sess.ID(), onDelSessID, "meta even had wrong session ID")
	require.Equal(t, subID, onDelSubID, "meta event did not return expected subscription ID")

	err = subscriber.Unsubscribe(metaOnUnsubscribe)
	require.NoError(t, err)
}

func TestMetaProcSubGet(t *testing.T) {
	// Connect subscriber session.
	subscriber := connectClient(t)

	// Subscribe to something
	nullHandler := func(_ *wamp.Event) {}
	err := subscriber.Subscribe(testTopicWC, nullHandler, wamp.Dict{"match": "wildcard"})
	require.NoError(t, err)
	subID, ok := subscriber.SubscriptionID(testTopicWC)
	require.True(t, ok, "client does not have subscription ID")

	// Connect caller
	caller := connectClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := caller.Call(ctx, metaSubGet, nil, wamp.List{subID}, nil, nil)
	require.NoError(t, err)
	subDetails, ok := wamp.AsDict(result.Arguments[0])
	require.True(t, ok, "Result argument is not dict")
	id, _ := wamp.AsID(subDetails["id"])
	require.Equal(t, subID, id, "Received wrong subscription ID")
	m, _ := wamp.AsString(subDetails["match"])
	require.Equal(t, "wildcard", m, "subscription does not have wildcard policy")
	uri, _ := wamp.AsURI(subDetails["uri"])
	require.Equal(t, wamp.URI(testTopicWC), uri, "subscription has wrong topic URI")

	err = subscriber.Unsubscribe(testTopicWC)
	require.NoError(t, err)

	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err = caller.Call(ctx, metaSubGet, nil, wamp.List{subID}, nil, nil)
	require.Error(t, err)
	var rpcErr client.RPCError
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, wamp.ErrNoSuchSubscription, rpcErr.Err.Error)
}
