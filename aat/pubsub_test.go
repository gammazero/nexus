package aat

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/wamp"
)

const (
	testTopic       = "nexus.test.topic"
	testTopicPrefix = "nexus.test"
	testTopicWC     = "nexus..topic"
)

func TestPubSub(t *testing.T) {
	defer leaktest.Check(t)()

	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	errChan := make(chan error)
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		arg, _ := wamp.AsString(args[0])
		if arg != "hello world" {
			errChan <- errors.New("event missing or bad args")
			return
		}
		errChan <- nil
	}

	// Subscribe to event.
	err = subscriber.Subscribe(testTopic, evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Connect publisher session.
	publisher, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	// Publish an event to topic.
	err = publisher.Publish(testTopic, wamp.Dict{wamp.OptAcknowledge: true},
		wamp.List{"hello world"}, nil)
	if err != nil {
		t.Fatal("Error waiting for published response:", err)
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

	err = subscriber.Unsubscribe(testTopic)
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}

	err = publisher.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestPubSubWildcard(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Check for feature support in router.
	const featurePatternSub = "pattern_based_subscription"
	if !subscriber.HasFeature("broker", featurePatternSub) {
		t.Error("Broker does not have", featurePatternSub, "feature")
	}

	errChan := make(chan error)
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		arg, _ := wamp.AsString(args[0])
		if arg != "hello world" {
			errChan <- errors.New("event missing or bad args")
			return
		}
		origTopic, _ := wamp.AsURI(details["topic"])
		if origTopic != testTopic {
			errChan <- errors.New("wrong original topic")
			return
		}
		errChan <- nil
	}

	// Subscribe to event with wildcard match.
	err = subscriber.Subscribe(testTopicWC, evtHandler, wamp.SetOption(nil, "match", "wildcard"))
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Connect publisher session.
	publisher, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	// Publish an event to something that matches by wildcard.
	publisher.Publish(testTopic, nil, wamp.List{"hello world"}, nil)

	// Make sure the event was received.
	select {
	case err = <-errChan:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get published event")
	}
	if err != nil {
		t.Fatal(err)
	}

	err = subscriber.Unsubscribe(testTopicWC)
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}

	err = publisher.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestUnsubscribeWrongTopic(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		return
	}

	// Subscribe to event.
	err = subscriber.Subscribe(testTopic, evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	err = subscriber.Unsubscribe(testTopicWC)
	if err == nil {
		t.Fatal("expected error unsubscribing from wrong topic")
	}

	// Connect subscriber session2.
	subscriber2, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Subscribe to other event.
	topic2 := "nexus.test.topic2"
	err = subscriber2.Subscribe(topic2, evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Unsubscribe from other subscriber's topic.
	err = subscriber2.Unsubscribe(testTopic)
	if err == nil {
		t.Fatal("expected error unsubscribing from other's topic")
	}

	err = subscriber.Unsubscribe(testTopic)
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}

	err = subscriber2.Unsubscribe(topic2)
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = subscriber2.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestSubscribeBurst(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect subscriber session.
	sub, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		return
	}

	for i := 0; i < 10; i++ {
		// Subscribe to event.
		topic := fmt.Sprintf("test.topic%d", i)
		err = sub.Subscribe(topic, evtHandler, nil)
		if err != nil {
			t.Fatal("subscribe error:", err)
		}
	}

	for i := 0; i < 10; i++ {
		// Subscribe to event.
		topic := fmt.Sprintf("test.topic%d", i)
		err = sub.Unsubscribe(topic)
		if err != nil {
			t.Fatal("subscribe error:", err)
		}
	}

	sub.Close()
}
