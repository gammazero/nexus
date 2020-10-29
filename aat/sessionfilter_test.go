package aat

import (
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

func TestWhitelistAttribute(t *testing.T) {
	// Setup subscriber1
	cfg := client.Config{
		Realm:           testRealm,
		HelloDetails:    wamp.Dict{"org_id": "zcorp"},
		ResponseTimeout: time.Second,
	}
	subscriber1, err := connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	sub1Events := make(chan *wamp.Event)
	err = subscriber1.SubscribeChan(testTopic, sub1Events, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Setup subscriber2
	cfg = client.Config{
		Realm:        testRealm,
		HelloDetails: wamp.Dict{"org_id": "other"},
	}
	subscriber2, err := connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	sub2Events := make(chan *wamp.Event)
	err = subscriber2.SubscribeChan(testTopic, sub2Events, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Connect publisher
	publisher, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Publish an event with whitelist that matches subscriber1 non-standard
	// hello options.
	opts := wamp.Dict{"eligible_org_id": wamp.List{"zcorp", "goodguys"}}
	publisher.Publish(testTopic, opts, wamp.List{"hello world"}, nil)

	// Make sure the event was received by subscriber1
	select {
	case <-sub1Events:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Subscriber1 did not get published event")
	}

	// Make sure the event was not received by subscriber2
	select {
	case <-sub2Events:
		t.Fatal("Subscriber2 received published event")
	case <-time.After(200 * time.Millisecond):
	}

	// Publish an event with whitelist that matches subscriber2 non-standard
	// hello options.
	opts = wamp.Dict{"eligible_org_id": wamp.List{"other"}}
	publisher.Publish(testTopic, opts, wamp.List{"hello world"}, nil)

	// Make sure the event was received by subscriber2
	select {
	case <-sub2Events:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Subscriber2 did not get published event")
	}
	// Make sure the event was not received by subscriber1
	select {
	case <-sub1Events:
		t.Fatal("Subscriber1 received published event")
	case <-time.After(200 * time.Millisecond):
	}

	// Publish an event with whitelist that matches subscriber1 and subscriber2
	// non-standard hello options.
	opts = wamp.Dict{"eligible_org_id": wamp.List{"zcorp", "other"}}
	publisher.Publish(testTopic, opts, wamp.List{"hello world"}, nil)

	// Make sure the event was received by subscriber1
	select {
	case <-sub1Events:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Subscriber1 did not get published event")
	}
	// Make sure the event was received by subscriber2
	select {
	case <-sub2Events:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Subscriber2 did not get published event")
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
	cfg := client.Config{
		Realm:           testRealm,
		HelloDetails:    wamp.Dict{"org_id": "zcorp"},
		ResponseTimeout: time.Second,
	}
	subscriber1, err := connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Check for feature support in router.
	if !subscriber1.HasFeature(wamp.RoleBroker, wamp.FeatureSubBlackWhiteListing) {
		t.Error("Broker does not support", wamp.FeatureSubBlackWhiteListing)
	}

	sub1Events := make(chan *wamp.Event)
	err = subscriber1.SubscribeChan(testTopic, sub1Events, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Setup subscriber2
	cfg = client.Config{
		Realm:        testRealm,
		HelloDetails: wamp.Dict{"org_id": "other"},
	}
	subscriber2, err := connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	sub2Events := make(chan *wamp.Event)
	err = subscriber2.SubscribeChan(testTopic, sub2Events, nil)
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
	case <-sub1Events:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Subscriber1 did not get published event")
	}

	// Make sure the event was not received by subscriber2
	select {
	case <-sub2Events:
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
