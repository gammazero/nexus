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
	metaRegOnCreate  = string(wamp.MetaEventRegOnCreate)
	metaOnRegister   = string(wamp.MetaEventRegOnRegister)
	metaOnUnregister = string(wamp.MetaEventRegOnUnregister)
	metaRegOnDelete  = string(wamp.MetaEventRegOnDelete)
)

func TestMetaEventRegOnCreateRegOnRegister(t *testing.T) {
	// Connect subscriber session.
	subscriber := connectClient(t)

	// Check for feature support in router.
	has := subscriber.HasFeature(wamp.RoleDealer, wamp.FeatureRegMetaAPI)
	require.Truef(t, has, "Dealer does not support %s", wamp.FeatureRegMetaAPI)

	// Subscribe to event.
	onCreateEvents := make(chan *wamp.Event)
	err := subscriber.SubscribeChan(metaRegOnCreate, onCreateEvents, nil)
	require.NoError(t, err)

	onRegEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnRegister, onRegEvents, nil)
	require.NoError(t, err)

	select {
	case <-onCreateEvents:
		require.FailNow(t, "Received on_create when subscribing to meta event")
	case <-onRegEvents:
		require.FailNow(t, "Received on_register when subscribing to meta event")
	case <-time.After(200 * time.Millisecond):
	}

	// Connect another client.
	sess := connectClient(t)

	// Register procedure to generate meta on_register event
	nullHandler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Args: wamp.List{"hello"}}
	}
	err = sess.Register("some.proc", nullHandler, nil)
	require.NoError(t, err)

	regID, ok := sess.RegistrationID("some.proc")
	require.True(t, ok, "client does not have registration ID")

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
		if u, _ := wamp.AsURI(dict["uri"]); u != wamp.URI("some.proc") {
			return fmt.Errorf(
				"on_create had wrong procedure, got '%v' want 'some.proc'", u)
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
		require.FailNowf(t, "did not get %s event", metaRegOnCreate)
	}
	require.Equal(t, sess.ID(), onCreateSessID, "meta even had wrong session ID")
	require.Equal(t, regID, onCreateID, "meta event did not return expected registration ID")

	var onRegRegID, onRegSessID wamp.ID
	onReg := func(event *wamp.Event) error {
		args := event.Arguments
		if len(args) != 2 {
			return errors.New("wrong number of arguments")
		}
		var ok bool
		onRegSessID, ok = wamp.AsID(args[0])
		if !ok {
			return errors.New("argument 0 (session) was not wamp.ID")
		}
		onRegRegID, ok = wamp.AsID(args[1])
		if !ok {
			return errors.New("argument 1 (registration) was not wamp.ID")
		}
		return nil
	}

	// Make sure the on_register event was received.
	select {
	case event = <-onRegEvents:
		err = onReg(event)
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		require.FailNowf(t, "did not get %s event", metaOnRegister)
	}
	require.Equal(t, sess.ID(), onRegSessID, "meta even had wrong session ID")
	require.Equal(t, regID, onRegRegID, "meta event did not return expected registration ID")

	err = sess.Unregister("some.proc")
	require.NoError(t, err)

	err = subscriber.Unsubscribe(metaOnRegister)
	require.NoError(t, err)
}

func TestMetaEventRegOnUnregisterRegOnDelete(t *testing.T) {
	// Connect subscriber session.
	subscriber := connectClient(t)

	// Subscribe to on_unregister event.
	onUnregEvents := make(chan *wamp.Event)
	err := subscriber.SubscribeChan(metaOnUnregister, onUnregEvents, nil)
	require.NoError(t, err)

	// Subscribe to on_delete event.
	onDelEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaRegOnDelete, onDelEvents, nil)
	require.NoError(t, err)

	select {
	case <-onUnregEvents:
		require.FailNow(t, "Received on_unregister when unsubscribing to meta event")
	case <-onDelEvents:
		require.FailNow(t, "Received on_delete when unsubscribing to meta event")
	case <-time.After(200 * time.Millisecond):
	}

	// Connect another client.
	sess := connectClient(t)

	// Register something and then unregister to generate meta on_unregister
	// event.
	nullHandler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Args: wamp.List{"hello"}}
	}
	err = sess.Register("some.proc", nullHandler, nil)
	require.NoError(t, err)
	regID, ok := sess.RegistrationID("some.proc")
	require.True(t, ok, "client does not have registration ID")
	err = sess.Unregister("some.proc")
	require.NoError(t, err)

	var onUnregRegID, onUnregSessID wamp.ID
	onUnreg := func(event *wamp.Event) error {
		args := event.Arguments
		if len(args) != 2 {
			return errors.New("wrong number of arguments")
		}
		var ok bool
		onUnregSessID, ok = wamp.AsID(args[0])
		if !ok {
			return errors.New("argument 0 (session) was not wamp.ID")
		}
		onUnregRegID, ok = wamp.AsID(args[1])
		if !ok {
			return errors.New("argument 1 (registration) was not wamp.ID")
		}
		return nil
	}

	// Make sure the unregister event was received.
	var event *wamp.Event
	select {
	case event = <-onUnregEvents:
		err = onUnreg(event)
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		require.FailNowf(t, "did not get %s event", metaOnUnregister)
	}
	require.Equal(t, sess.ID(), onUnregSessID, "meta even had wrong session ID")
	require.Equal(t, regID, onUnregRegID, "meta event did not return expected registration ID")

	var onDelRegID, onDelSessID wamp.ID
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
		onDelRegID, ok = wamp.AsID(args[1])
		if !ok {
			return errors.New("argument 1 (registration) was not wamp.ID")
		}
		return nil
	}

	// Make sure the delete event was received.
	select {
	case event = <-onDelEvents:
		err = onDel(event)
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		require.FailNowf(t, "did not get %s event", metaRegOnDelete)
	}
	require.Equal(t, sess.ID(), onDelSessID, "meta even had wrong session ID")
	require.Equal(t, regID, onDelRegID, "meta event did not return expected registration ID")

	err = subscriber.Unsubscribe(metaOnUnregister)
	require.NoError(t, err)
}
