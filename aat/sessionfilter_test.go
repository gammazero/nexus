package aat

import (
	"testing"
	"time"

	"github.com/gammazero/nexus/wamp"
)

func TestWhitelistAttribute(t *testing.T) {
	// Setup subscriber1
	subscriber1, err := connectClientDetails(wamp.Dict{"org_id": "spirent"})
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	sync1 := make(chan struct{})
	evtHandler1 := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		sync1 <- struct{}{}
	}
	err = subscriber1.Subscribe(testTopic, evtHandler1, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Setup subscriber2
	subscriber2, err := connectClientDetails(wamp.Dict{"org_id": "other"})
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	sync2 := make(chan struct{})
	evtHandler2 := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		sync2 <- struct{}{}
	}
	err = subscriber2.Subscribe(testTopic, evtHandler2, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Connect publisher
	publisher, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	opts := wamp.Dict{"eligible_org_id": wamp.List{"spirent", "goodguys"}}

	// Publish an event to something that matches by wildcard.
	publisher.Publish(testTopic, opts, wamp.List{"hello world"}, nil)

	// Make sure the event was received by subscriber1
	select {
	case <-sync1:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Subscriber1 did not get published event")
	}

	// Make sure the event was not received by subscriber2
	select {
	case <-sync2:
		t.Fatal("Subscriber2 received published event")
	case <-time.After(200 * time.Millisecond):
	}

	err = subscriber1.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
	err = subscriber2.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestBlacklistAttribute(t *testing.T) {
	// Setup subscriber1
	subscriber1, err := connectClientDetails(wamp.Dict{"org_id": "spirent"})
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	sync1 := make(chan struct{})
	evtHandler1 := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		sync1 <- struct{}{}
	}
	err = subscriber1.Subscribe(testTopic, evtHandler1, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Setup subscriber2
	subscriber2, err := connectClientDetails(wamp.Dict{"org_id": "other"})
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	sync2 := make(chan struct{})
	evtHandler2 := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		sync2 <- struct{}{}
	}
	err = subscriber2.Subscribe(testTopic, evtHandler2, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Connect publisher
	publisher, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	opts := wamp.Dict{"exclude_org_id": wamp.List{"other", "bagduy"}}

	// Publish an event to something that matches by wildcard.
	publisher.Publish(testTopic, opts, wamp.List{"hello world"}, nil)

	// Make sure the event was received by subscriber1
	select {
	case <-sync1:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Subscriber1 did not get published event")
	}

	// Make sure the event was not received by subscriber2
	select {
	case <-sync2:
		t.Fatal("Subscriber2 received published event")
	case <-time.After(200 * time.Millisecond):
	}

	err = subscriber1.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
	err = subscriber2.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}
