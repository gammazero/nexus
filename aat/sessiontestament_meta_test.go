package aat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/wamp"
)

const (
	metaAddTestament    = string(wamp.MetaProcSessionAddTestament)
	metaFlushTestaments = string(wamp.MetaProcSessionFlushTestaments)
)

func TestAddTestament(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	errChan := make(chan error)
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) == 0 {
			errChan <- errors.New("missing argument")
			return
		}
		val, ok := wamp.AsString(args[0])
		if !ok {
			errChan <- errors.New("Argument was not string")
			return
		}
		if val != "foo" {
			errChan <- errors.New("Argument value was invalid")
			return
		}
		if len(kwargs) != 0 {
			errChan <- errors.New("Received unexpected kwargs")
		}
		errChan <- nil
	}

	err = subscriber.Subscribe("testament.test", evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	_, err = sess.Call(context.Background(), metaAddTestament, wamp.Dict{}, wamp.List{
		"testament.test",
		wamp.List{
			"foo",
		},
		wamp.Dict{},
	}, wamp.Dict{}, "")
	if err != nil {
		t.Fatal("Failed to add testament:", err)
	}

	// Disconnect client to generate wamp.session.on_leave event.
	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	// Make sure the event was received.
	select {
	case err = <-errChan:
		if err != nil {
			t.Fatal("Event error:", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get published event")
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect subscriber:", err)
	}
}

func TestAddFlushTestament(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	errChan := make(chan error)
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) == 0 {
			errChan <- errors.New("missing argument")
			return
		}
		_, ok := wamp.AsString(args[0])
		if !ok {
			errChan <- errors.New("Argument was not string")
			return
		}
		errChan <- nil
	}

	err = subscriber.Subscribe("testament.test", evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	_, err = sess.Call(context.Background(), metaAddTestament, wamp.Dict{}, wamp.List{
		"testament.test",
		wamp.List{
			"foo",
		},
		wamp.Dict{},
	}, wamp.Dict{}, "")
	if err != nil {
		t.Fatal("Failed to add testament:", err)
	}
	_, err = sess.Call(context.Background(), metaFlushTestaments, wamp.Dict{}, wamp.List{}, wamp.Dict{}, "")
	if err != nil {
		t.Fatal("Failed to flush testament:", err)
	}

	// Disconnect client to generate wamp.session.on_leave event.
	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	// Make sure the event was received.
	select {
	case err = <-errChan:
		if err == nil {
			t.Fatal("Got event, shouldn't have event:", err)
		}
	case <-time.After(200 * time.Millisecond):
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect subscriber:", err)
	}
}
