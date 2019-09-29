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
	metaRegOnCreate  = string(wamp.MetaEventRegOnCreate)
	metaOnRegister   = string(wamp.MetaEventRegOnRegister)
	metaOnUnregister = string(wamp.MetaEventRegOnUnregister)
	metaRegOnDelete  = string(wamp.MetaEventRegOnDelete)
)

func TestMetaEventRegOnCreateRegOnRegister(t *testing.T) {
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Check for feature support in router.
	const featureRegMetaAPI = "registration_meta_api"
	if !subscriber.HasFeature("dealer", featureRegMetaAPI) {
		t.Error("Dealer does not have", featureRegMetaAPI, "feature")
	}

	// Subscribe to event.
	onCreateEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaRegOnCreate, onCreateEvents, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	onRegEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnRegister, onRegEvents, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	select {
	case <-onCreateEvents:
		t.Fatal("Received on_create when subscribing to meta event")
	case <-onRegEvents:
		t.Fatal("Received on_register when subscribing to meta event")
	case <-time.After(200 * time.Millisecond):
	}

	// Connect another client.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Register procedure to generate meta on_register event
	nullHandler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Args: wamp.List{"hello"}}
	}
	err = sess.Register("some.proc", nullHandler, nil)
	if err != nil {
		t.Fatal("register error:", err)
	}
	regID, ok := sess.RegistrationID("some.proc")
	if !ok {
		t.Fatal("client does not have registration ID")
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
		if err = onCreate(event); err != nil {
			t.Fatal("Event error:", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get", metaRegOnCreate, "event")
	}
	if onCreateSessID != sess.ID() {
		t.Fatal(metaRegOnCreate, "meta even had wrong session ID")
	}
	if onCreateID != regID {
		t.Fatal("meta event did not return expected registration ID")
	}

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
		if err = onReg(event); err != nil {
			t.Fatal("Event error:", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get", metaOnRegister, "event")
	}
	if onRegSessID != sess.ID() {
		t.Fatal(metaOnRegister, "meta even had wrong session ID")
	}
	if onRegRegID != regID {
		t.Fatal("meta event did not return expected registration ID")
	}

	err = sess.Unregister("some.proc")
	if err != nil {
		t.Fatal("unregister error:", err)
	}

	err = subscriber.Unsubscribe(metaOnRegister)
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

func TestMetaEventRegOnUnregisterRegOnDelete(t *testing.T) {
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Subscribe to on_unregister event.
	onUnregEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnUnregister, onUnregEvents, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Subscribe to on_delete event.
	onDelEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaRegOnDelete, onDelEvents, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	select {
	case <-onUnregEvents:
		t.Fatal("Received on_unregister when unsubscribing to meta event")
	case <-onDelEvents:
		t.Fatal("Received on_delete when unsubscribing to meta event")
	case <-time.After(200 * time.Millisecond):
	}

	// Connect another client.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Register something and then unregister to generate meta on_unregister
	// event.
	nullHandler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Args: wamp.List{"hello"}}
	}
	err = sess.Register("some.proc", nullHandler, nil)
	if err != nil {
		t.Fatal("Register error:", err)
	}
	regID, ok := sess.RegistrationID("some.proc")
	if !ok {
		t.Fatal("client does not have registration ID")
	}
	err = sess.Unregister("some.proc")
	if err != nil {
		t.Fatal("unregister error:", err)
	}

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
		if err = onUnreg(event); err != nil {
			t.Fatal("Event error:", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get", metaOnUnregister, "event")
	}
	if onUnregSessID != sess.ID() {
		t.Fatal(metaOnUnregister, "meta even had wrong session ID")
	}
	if onUnregRegID != regID {
		t.Fatal("meta event did not return expected registration ID")
	}

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
		if err = onDel(event); err != nil {
			t.Fatal("Event error:", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get", metaRegOnDelete, "event")
	}
	if onDelSessID != sess.ID() {
		t.Fatal(metaOnUnregister, "meta even had wrong session ID")
	}
	if onDelRegID != regID {
		t.Fatal("meta event did not return expected registration ID")
	}

	err = subscriber.Unsubscribe(metaOnUnregister)
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
