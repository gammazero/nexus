package aat

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
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
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Check for feature support in router.
	if !subscriber.HasFeature(wamp.RoleBroker, wamp.FeatureSubMetaAPI) {
		t.Error("Broker does not support", wamp.FeatureSubMetaAPI)
	}

	// Subscribe to on_create meta event.
	onCreateEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnCreate, onCreateEvents, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Subscribe to on_subscribe meta event.
	onSubEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnSubscribe, onSubEvents, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	select {
	case <-onCreateEvents:
		t.Fatal("Received on_create when subscribing to meta event")
	case <-onSubEvents:
		t.Fatal("Received on_subscribe when subscribing to meta event")
	case <-time.After(200 * time.Millisecond):
	}

	// Connect another client.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Subscribe to something to generate meta on_subcribe event
	nullHandler := func(_ *wamp.Event) { return }
	err = sess.Subscribe("some.topic", nullHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}
	subID, ok := sess.SubscriptionID("some.topic")
	if !ok {
		t.Fatal("client does not have subscription ID")
	}

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
		if err = onCreate(event); err != nil {
			t.Fatal("Event error:", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get", metaOnCreate, "event")
	}
	if onCreateSessID != sess.ID() {
		t.Fatal(metaOnCreate, "meta even had wrong session ID")
	}
	if onCreateID != subID {
		t.Fatal("meta event did not return expected subscription ID")
	}

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
		if err = onSub(event); err != nil {
			t.Fatal("Event error:", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get", metaOnSubscribe, "event")
	}
	if onSubSessID != sess.ID() {
		t.Fatal(metaOnSubscribe, "meta even had wrong session ID")
	}
	if onSubSubID != subID {
		t.Fatal("meta event did not return expected subscription ID")
	}

	err = sess.Unsubscribe("some.topic")
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}

	err = subscriber.Unsubscribe(metaOnSubscribe)
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}

	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestMetaEventOnUnsubscribeOnDelete(t *testing.T) {
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Subscribe to on_unsubscribe event.
	onUnsubEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnUnsubscribe, onUnsubEvents, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Subscribe to on_delete event.
	onDelEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnDelete, onDelEvents, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	select {
	case <-onUnsubEvents:
		t.Fatal("Received on_unsubscribe when unsubscribing to meta event")
	case <-onDelEvents:
		t.Fatal("Received on_delete when unsubscribing to meta event")
	case <-time.After(200 * time.Millisecond):
	}

	// Connect another client.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Subscribe to something and then unsubscribe to generate meta on_subcribe
	// event.
	nullHandler := func(_ *wamp.Event) { return }
	if err = sess.Subscribe("some.topic", nullHandler, nil); err != nil {
		t.Fatal("subscribe error:", err)
	}
	subID, ok := sess.SubscriptionID("some.topic")
	if !ok {
		t.Fatal("client does not have subscription ID")
	}
	if err = sess.Unsubscribe("some.topic"); err != nil {
		t.Fatal("unsubscribe error:", err)
	}

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
		if err = onUnsub(event); err != nil {
			t.Fatal("Event error:", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get", metaOnUnsubscribe, "event")
	}
	if onUnsubSessID != sess.ID() {
		t.Fatal(metaOnUnsubscribe, "meta even had wrong session ID")
	}
	if onUnsubSubID != subID {
		t.Fatal("meta event did not return expected subscription ID")
	}

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
		if err = onDel(event); err != nil {
			t.Fatal("Event error:", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get", metaOnDelete, "event")
	}
	if onDelSessID != sess.ID() {
		t.Fatal(metaOnUnsubscribe, "meta even had wrong session ID")
	}
	if onDelSubID != subID {
		t.Fatal("meta event did not return expected subscription ID")
	}

	err = subscriber.Unsubscribe(metaOnUnsubscribe)
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}

	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestMetaProcSubGet(t *testing.T) {
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer subscriber.Close()

	// Subscribe to something
	nullHandler := func(_ *wamp.Event) { return }
	err = subscriber.Subscribe(testTopicWC, nullHandler, wamp.Dict{"match": "wildcard"})
	if err != nil {
		t.Fatal("subscribe error:", err)
	}
	subID, ok := subscriber.SubscriptionID(testTopicWC)
	if !ok {
		t.Fatal("client does not have subscription ID")
	}

	// Connect caller
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer caller.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := caller.Call(ctx, metaSubGet, nil, wamp.List{subID}, nil, nil)
	if err != nil {
		t.Fatal("error calling", metaSubGet, err)
	}
	subDetails, ok := wamp.AsDict(result.Arguments[0])
	if !ok {
		t.Fatal("Result argument is not dict")
	}
	id, _ := wamp.AsID(subDetails["id"])
	if id != subID {
		t.Error("Received wrong subscription ID")
	}
	m, _ := wamp.AsString(subDetails["match"])
	if m != "wildcard" {
		t.Error("subscription does not have wildcard policy, has", m)
	}
	uri, _ := wamp.AsURI(subDetails["uri"])
	if uri != testTopicWC {
		t.Error("subscription has wrong topic URI")
	}

	err = subscriber.Unsubscribe(testTopicWC)
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}

	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err = caller.Call(ctx, metaSubGet, nil, wamp.List{subID}, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	rpcErr, ok := err.(client.RPCError)
	if !ok {
		t.Fatal("expected error to be client.RPCError")
	}
	if rpcErr.Err.Error != wamp.ErrNoSuchSubscription {
		t.Fatal("wrong error:", rpcErr.Err.Error)
	}
}
