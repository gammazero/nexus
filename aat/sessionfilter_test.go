package aat_test

import (
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

func TestWhitelistAttribute(t *testing.T) {
	// Setup subscriber1
	cfg := client.Config{
		Realm:           testRealm,
		HelloDetails:    wamp.Dict{"org_id": "zcorp"},
		ResponseTimeout: time.Second,
	}
	subscriber1 := connectClientCfg(t, cfg)
	sub1Events := make(chan *wamp.Event)
	err := subscriber1.SubscribeChan(testTopic, sub1Events, nil)
	require.NoError(t, err)

	// Setup subscriber2
	cfg = client.Config{
		Realm:        testRealm,
		HelloDetails: wamp.Dict{"org_id": "other"},
	}
	subscriber2 := connectClientCfg(t, cfg)
	sub2Events := make(chan *wamp.Event)
	err = subscriber2.SubscribeChan(testTopic, sub2Events, nil)
	require.NoError(t, err)

	// Connect publisher
	publisher := connectClient(t)

	// Publish an event with whitelist that matches subscriber1 non-standard
	// hello options.
	opts := wamp.Dict{"eligible_org_id": wamp.List{"zcorp", "goodguys"}}
	publisher.Publish(testTopic, opts, wamp.List{"hello world"}, nil)

	// Make sure the event was received by subscriber1
	select {
	case <-sub1Events:
	case <-time.After(200 * time.Millisecond):
		require.FailNow(t, "Subscriber1 did not get published event")
	}

	// Make sure the event was not received by subscriber2
	select {
	case <-sub2Events:
		require.FailNow(t, "Subscriber2 received published event")
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
		require.FailNow(t, "Subscriber2 did not get published event")
	}
	// Make sure the event was not received by subscriber1
	select {
	case <-sub1Events:
		require.FailNow(t, "Subscriber1 received published event")
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
		require.FailNow(t, "Subscriber1 did not get published event")
	}
	// Make sure the event was received by subscriber2
	select {
	case <-sub2Events:
	case <-time.After(200 * time.Millisecond):
		require.FailNow(t, "Subscriber2 did not get published event")
	}
}

func TestBlacklistAttribute(t *testing.T) {
	// Setup subscriber1
	cfg := client.Config{
		Realm:           testRealm,
		HelloDetails:    wamp.Dict{"org_id": "zcorp"},
		ResponseTimeout: time.Second,
	}
	subscriber1 := connectClientCfg(t, cfg)

	// Check for feature support in router.
	has := subscriber1.HasFeature(wamp.RoleBroker, wamp.FeatureSubBlackWhiteListing)
	require.Truef(t, has, "Broker does not support %s", wamp.FeatureSubBlackWhiteListing)

	sub1Events := make(chan *wamp.Event)
	err := subscriber1.SubscribeChan(testTopic, sub1Events, nil)
	require.NoError(t, err)

	// Setup subscriber2
	cfg = client.Config{
		Realm:        testRealm,
		HelloDetails: wamp.Dict{"org_id": "other"},
	}
	subscriber2 := connectClientCfg(t, cfg)
	sub2Events := make(chan *wamp.Event)
	err = subscriber2.SubscribeChan(testTopic, sub2Events, nil)
	require.NoError(t, err)

	// Connect publisher
	publisher := connectClient(t)

	opts := wamp.Dict{"exclude_org_id": wamp.List{"other", "bagduy"}}

	// Publish an event to something that matches by wildcard.
	publisher.Publish(testTopic, opts, wamp.List{"hello world"}, nil)

	// Make sure the event was received by subscriber1
	select {
	case <-sub1Events:
	case <-time.After(200 * time.Millisecond):
		require.FailNow(t, "Subscriber1 did not get published event")
	}

	// Make sure the event was not received by subscriber2
	select {
	case <-sub2Events:
		require.FailNow(t, "Subscriber2 received published event")
	case <-time.After(200 * time.Millisecond):
	}
}
