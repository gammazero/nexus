package test_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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

// TestSpecFilterEligibleSessionID verifies the standard `eligible` whitelist
// option (session-id list) per WAMP spec §14.4.2.
func TestSpecFilterEligibleSessionID(t *testing.T) {
	checkGoLeaks(t)

	sub1 := connectClient(t)
	ev1 := make(chan *wamp.Event, 1)
	require.NoError(t, sub1.SubscribeChan(testTopic, ev1, nil))

	sub2 := connectClient(t)
	ev2 := make(chan *wamp.Event, 1)
	require.NoError(t, sub2.SubscribeChan(testTopic, ev2, nil))

	pub := connectClient(t)
	opts := wamp.Dict{wamp.WhitelistKey: wamp.List{sub1.ID()}}
	require.NoError(t, pub.Publish(testTopic, opts, wamp.List{"hi"}, nil))

	select {
	case <-ev1:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("eligible-listed subscriber did not receive event")
	}
	select {
	case <-ev2:
		t.Fatal("non-eligible subscriber received event")
	case <-time.After(150 * time.Millisecond):
	}

	require.NoError(t, sub1.Unsubscribe(testTopic))
	require.NoError(t, sub2.Unsubscribe(testTopic))
}

// TestSpecFilterExcludeSessionID verifies the standard `exclude` blacklist
// option (session-id list) per WAMP spec §14.4.2.
func TestSpecFilterExcludeSessionID(t *testing.T) {
	checkGoLeaks(t)

	sub1 := connectClient(t)
	ev1 := make(chan *wamp.Event, 1)
	require.NoError(t, sub1.SubscribeChan(testTopic, ev1, nil))

	sub2 := connectClient(t)
	ev2 := make(chan *wamp.Event, 1)
	require.NoError(t, sub2.SubscribeChan(testTopic, ev2, nil))

	pub := connectClient(t)
	opts := wamp.Dict{wamp.BlacklistKey: wamp.List{sub2.ID()}}
	require.NoError(t, pub.Publish(testTopic, opts, wamp.List{"hi"}, nil))

	select {
	case <-ev1:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("non-excluded subscriber did not receive event")
	}
	select {
	case <-ev2:
		t.Fatal("excluded subscriber received event")
	case <-time.After(150 * time.Millisecond):
	}

	require.NoError(t, sub1.Unsubscribe(testTopic))
	require.NoError(t, sub2.Unsubscribe(testTopic))
}

// TestSpecFilterEligibleAndExclude verifies that when both eligible and
// exclude lists name the same session, exclude wins (spec §14.4.2).
func TestSpecFilterEligibleAndExclude(t *testing.T) {
	checkGoLeaks(t)

	sub1 := connectClient(t)
	ev1 := make(chan *wamp.Event, 1)
	require.NoError(t, sub1.SubscribeChan(testTopic, ev1, nil))

	sub2 := connectClient(t)
	ev2 := make(chan *wamp.Event, 1)
	require.NoError(t, sub2.SubscribeChan(testTopic, ev2, nil))

	pub := connectClient(t)
	opts := wamp.Dict{
		wamp.WhitelistKey: wamp.List{sub1.ID(), sub2.ID()},
		wamp.BlacklistKey: wamp.List{sub2.ID()},
	}
	require.NoError(t, pub.Publish(testTopic, opts, wamp.List{"hi"}, nil))

	select {
	case <-ev1:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("eligible subscriber did not receive event")
	}
	select {
	case <-ev2:
		t.Fatal("eligible-but-excluded subscriber received event")
	case <-time.After(150 * time.Millisecond):
	}

	require.NoError(t, sub1.Unsubscribe(testTopic))
	require.NoError(t, sub2.Unsubscribe(testTopic))
}

// TestSpecFilterEligibleAuthRole verifies eligible_authrole filtering using
// the auth realm where wampcra-authenticated clients get authrole=user and
// the publisher's anonymous role is filtered out.
func TestSpecFilterEligibleAuthRole(t *testing.T) {
	checkGoLeaks(t)

	authedCfg := client.Config{
		Realm:        testAuthRealm,
		HelloDetails: wamp.Dict{"authid": "jdoe"},
		AuthHandlers: map[string]client.AuthFunc{
			"wampcra": clientAuthFunc,
		},
		ResponseTimeout: time.Second,
	}
	subAuthed := connectClientCfg(t, authedCfg)
	evAuthed := make(chan *wamp.Event, 1)
	require.NoError(t, subAuthed.SubscribeChan(testTopic, evAuthed, nil))

	anonCfg := client.Config{
		Realm:           testAuthRealm,
		ResponseTimeout: time.Second,
	}
	subAnon := connectClientCfg(t, anonCfg)
	evAnon := make(chan *wamp.Event, 1)
	require.NoError(t, subAnon.SubscribeChan(testTopic, evAnon, nil))

	pub := connectClientCfg(t, anonCfg)
	opts := wamp.Dict{"eligible_authrole": wamp.List{"user"}}
	require.NoError(t, pub.Publish(testTopic, opts, wamp.List{"to user role"}, nil))

	select {
	case <-evAuthed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("subscriber with authrole=user did not receive event")
	}
	select {
	case <-evAnon:
		t.Fatal("anonymous subscriber received event filtered to authrole=user")
	case <-time.After(150 * time.Millisecond):
	}

	require.NoError(t, subAuthed.Unsubscribe(testTopic))
	require.NoError(t, subAnon.Unsubscribe(testTopic))
}
