package aat_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/v3/wamp"
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

	// Check for feature support in router.
	if !subscriber.HasFeature(wamp.RoleDealer, wamp.FeatureTestamentMetaAPI) {
		t.Error("Dealer does not support", wamp.FeatureTestamentMetaAPI)
	}

	eventChan := make(chan *wamp.Event)
	err = subscriber.SubscribeChan("testament.test", eventChan, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	tstmtArgs := wamp.List{
		"testament.test",
		wamp.List{
			"foo",
		},
		wamp.Dict{},
	}
	_, err = sess.Call(context.Background(), metaAddTestament, nil, tstmtArgs, nil, nil)
	if err != nil {
		t.Fatal("Failed to add testament:", err)
	}

	// Disconnect client to trigger testament
	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	checkEvent := func(event *wamp.Event) error {
		args := event.Arguments
		if len(args) == 0 {
			return errors.New("missing argument")
		}
		val, ok := wamp.AsString(args[0])
		if !ok {
			return errors.New("Argument was not string")
		}
		if val != "foo" {
			return errors.New("Argument value was invalid")
		}
		if len(event.ArgumentsKw) != 0 {
			return errors.New("Received unexpected kwargs")
		}
		return nil
	}

	// Make sure the event was received.
	select {
	case event := <-eventChan:
		if err = checkEvent(event); err != nil {
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

	eventChan := make(chan *wamp.Event)
	err = subscriber.SubscribeChan("testament.test", eventChan, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	tstmtArgs := wamp.List{
		"testament.test",
		wamp.List{
			"foo",
		},
		wamp.Dict{},
	}
	_, err = sess.Call(context.Background(), metaAddTestament, nil, tstmtArgs, nil, nil)
	if err != nil {
		t.Fatal("Failed to add testament:", err)
	}
	_, err = sess.Call(context.Background(), metaFlushTestaments, nil, nil, nil, nil)
	if err != nil {
		t.Fatal("Failed to flush testament:", err)
	}

	// Disconnect client to trigge testament, which should not exist since it
	// was flushed above.
	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	// Make sure the event was received.
	select {
	case <-eventChan:
		t.Fatal("should not have received event")
	case <-time.After(200 * time.Millisecond):
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect subscriber:", err)
	}
}
