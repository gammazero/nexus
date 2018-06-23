package aat

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
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

	var onCreateID, onCreateSessID wamp.ID
	errChanC := make(chan error)
	onCreateHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) != 2 {
			errChanC <- errors.New("wrong number of arguments")
			return
		}
		dict, ok := wamp.AsDict(args[1])
		if !ok {
			errChanC <- errors.New("arg 1 was not wamp.Dict")
			return
		}
		onCreateSessID, ok = wamp.AsID(args[0])
		if !ok {
			errChanC <- errors.New("argument 0 (session) was not wamp.ID")
			return
		}
		onCreateID, _ = wamp.AsID(dict["id"])
		if u, _ := wamp.AsURI(dict["uri"]); u != wamp.URI("some.proc") {
			errChanC <- fmt.Errorf(
				"on_create had wrong procedure, got '%v' want 'some.proc'", u)
			return
		}
		if s, _ := wamp.AsString(dict["created"]); s == "" {
			errChanC <- errors.New("on_create missing created time")
			return
		}
		errChanC <- nil
	}

	var onRegRegID, onRegSessID wamp.ID
	errChanS := make(chan error)
	onRegHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) != 2 {
			errChanS <- errors.New("wrong number of arguments")
			return
		}
		var ok bool
		onRegSessID, ok = wamp.AsID(args[0])
		if !ok {
			errChanS <- errors.New("argument 0 (session) was not wamp.ID")
			return
		}
		onRegRegID, ok = wamp.AsID(args[1])
		if !ok {
			errChanS <- errors.New("argument 1 (registration) was not wamp.ID")
			return
		}
		errChanS <- nil
	}

	// Subscribe to event.
	err = subscriber.Subscribe(metaRegOnCreate, onCreateHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	err = subscriber.Subscribe(metaOnRegister, onRegHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	select {
	case <-errChanC:
		t.Fatal("Received on_create when subscribing to meta event")
	case <-errChanS:
		t.Fatal("Received on_register when subscribing to meta event")
	case <-time.After(200 * time.Millisecond):
	}

	// Connect another client.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Register procedure to generate meta on_register event
	nullHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		return &client.InvokeResult{Args: wamp.List{"hello"}}
	}
	err = sess.Register("some.proc", nullHandler, nil)
	if err != nil {
		t.Fatal("register error:", err)
	}
	regID, ok := sess.RegistrationID("some.proc")
	if !ok {
		t.Fatal("client does not have registration ID")
	}

	// Make sure the on_create event was received.
	select {
	case err = <-errChanC:
		if err != nil {
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

	// Make sure the on_register event was received.
	select {
	case err = <-errChanS:
		if err != nil {
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

	var onUnregRegID, onUnregSessID wamp.ID
	errChan := make(chan error)
	onUnregHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) != 2 {
			errChan <- errors.New("wrong number of arguments")
			return
		}
		var ok bool
		onUnregSessID, ok = wamp.AsID(args[0])
		if !ok {
			errChan <- errors.New("argument 0 (session) was not wamp.ID")
			return
		}
		onUnregRegID, ok = wamp.AsID(args[1])
		if !ok {
			errChan <- errors.New("argument 1 (registration) was not wamp.ID")
			return
		}
		errChan <- nil
	}

	var onDelRegID, onDelSessID wamp.ID
	errChanD := make(chan error)
	onDelHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) != 2 {
			errChanD <- errors.New("wrong number of arguments")
			return
		}
		var ok bool
		onDelSessID, ok = wamp.AsID(args[0])
		if !ok {
			errChanD <- errors.New("argument 0 (session) was not wamp.ID")
			return
		}
		onDelRegID, ok = wamp.AsID(args[1])
		if !ok {
			errChanD <- errors.New("argument 1 (registration) was not wamp.ID")
			return
		}
		errChanD <- nil
	}

	// Clear any meta events from registration removal in previous tests.
	select {
	case <-errChan:
	case <-errChanD:
	case <-time.After(200 * time.Millisecond):
	}

	// Subscribe to on_unregister event.
	err = subscriber.Subscribe(metaOnUnregister, onUnregHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Subscribe to on_delete event.
	err = subscriber.Subscribe(metaRegOnDelete, onDelHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	select {
	case <-errChan:
		t.Fatal("Received on_unregister when unsubscribing to meta event")
	case <-errChanD:
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
	nullHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		return &client.InvokeResult{Args: wamp.List{"hello"}}
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

	// Make sure the unregister event was received.
	select {
	case err = <-errChan:
		if err != nil {
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

	// Make sure the delete event was received.
	select {
	case err = <-errChanD:
		if err != nil {
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
