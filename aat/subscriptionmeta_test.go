package aat

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gammazero/nexus/wamp"
)

const (
	metaOnCreate      = string(wamp.MetaEventSubOnCreate)
	metaOnSubscribe   = string(wamp.MetaEventSubOnSubscribe)
	metaOnUnsubscribe = string(wamp.MetaEventSubOnUnsubscribe)
	metaOnDelete      = string(wamp.MetaEventSubOnDelete)
)

func TestMetaEventOnCreateOnSubscribe(t *testing.T) {
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
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
		onCreateID = wamp.OptionID(dict, "id")
		if wamp.OptionURI(dict, "uri") != wamp.URI("some.topic") {
			errChanC <- fmt.Errorf(
				"on_create had wrong topic, got '%v' want 'some.topic'",
				wamp.OptionURI(dict, "uri"))
			return
		}
		if wamp.OptionString(dict, "created") == "" {
			errChanC <- errors.New("on_create missing created time")
			return
		}
		errChanC <- nil
	}

	var onSubSubID, onSubSessID wamp.ID
	errChanS := make(chan error)
	onSubHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) != 2 {
			errChanS <- errors.New("wrong number of arguments")
			return
		}
		var ok bool
		onSubSessID, ok = wamp.AsID(args[0])
		if !ok {
			errChanS <- errors.New("argument 0 (session) was not wamp.ID")
			return
		}
		onSubSubID, ok = wamp.AsID(args[1])
		if !ok {
			errChanS <- errors.New("argument 1 (subscription) was not wamp.ID")
			return
		}
		errChanS <- nil
	}

	// Subscribe to event.
	err = subscriber.Subscribe(metaOnCreate, onCreateHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	err = subscriber.Subscribe(metaOnSubscribe, onSubHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	select {
	case <-errChanC:
		t.Fatal("Received on_create when subscribing to meta event")
	case <-errChanS:
		t.Fatal("Received on_subscribe when subscribing to meta event")
	case <-time.After(200 * time.Millisecond):
	}

	// Connect another client.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Subscribe to something to generate meta on_subcribe event
	nullHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		return
	}
	err = sess.Subscribe("some.topic", nullHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}
	subID, ok := sess.SubscriptionID("some.topic")
	if !ok {
		t.Fatal("client does not have subscription ID")
	}

	// Make sure the on_create event was received.
	select {
	case err = <-errChanC:
		if err != nil {
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

	// Make sure the on_subscribe event was received.
	select {
	case err = <-errChanS:
		if err != nil {
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

	var onUnsubSubID, onUnsubSessID wamp.ID
	errChan := make(chan error)
	onUnsubHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) != 2 {
			errChan <- errors.New("wrong number of arguments")
			return
		}
		var ok bool
		onUnsubSessID, ok = wamp.AsID(args[0])
		if !ok {
			errChan <- errors.New("argument 0 (session) was not wamp.ID")
			return
		}
		onUnsubSubID, ok = wamp.AsID(args[1])
		if !ok {
			errChan <- errors.New("argument 1 (subscription) was not wamp.ID")
			return
		}
		errChan <- nil
	}

	var onDelSubID, onDelSessID wamp.ID
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
		onDelSubID, ok = wamp.AsID(args[1])
		if !ok {
			errChanD <- errors.New("argument 1 (subscription) was not wamp.ID")
			return
		}
		errChanD <- nil
	}

	// Clear any meta events from subscription removal in previous tests.
	select {
	case <-errChan:
	case <-errChanD:
	case <-time.After(200 * time.Millisecond):
	}

	// Subscribe to on_unsubscribe event.
	err = subscriber.Subscribe(metaOnUnsubscribe, onUnsubHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Subscribe to on_delete event.
	err = subscriber.Subscribe(metaOnDelete, onDelHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	select {
	case <-errChan:
		t.Fatal("Received on_unsubscribe when unsubscribing to meta event")
	case <-errChanD:
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
	nullHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		return
	}
	err = sess.Subscribe("some.topic", nullHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}
	subID, ok := sess.SubscriptionID("some.topic")
	if !ok {
		t.Fatal("client does not have subscription ID")
	}
	err = sess.Unsubscribe("some.topic")
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}

	// Make sure the unsubscribe event was received.
	select {
	case err = <-errChan:
		if err != nil {
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

	// Make sure the delete event was received.
	select {
	case err = <-errChanD:
		if err != nil {
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
