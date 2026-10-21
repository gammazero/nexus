package test_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

const (
	testTopic       = "nexus.test.topic"
	testTopicPrefix = "nexus.test"
	testTopicWC     = "nexus..topic"
)

func TestPubSub(t *testing.T) {
	checkGoLeaks(t)

	// Connect subscriber session.
	subscriber := connectClient(t)

	errChan := make(chan error)
	eventHandler := func(event *wamp.Event) {
		if len(event.Arguments) == 0 {
			errChan <- errors.New("missing arg")
			return
		}
		arg, _ := wamp.AsString(event.Arguments[0])
		if arg != "hello world" {
			errChan <- errors.New("bad arg")
			return
		}
		errChan <- nil
	}

	// Subscribe to event.
	err := subscriber.Subscribe(testTopic, eventHandler, nil)
	require.NoError(t, err)

	// Connect publisher session.
	publisher := connectClient(t)
	// Publish an event to topic.
	err = publisher.Publish(testTopic, wamp.Dict{wamp.OptAcknowledge: true},
		wamp.List{"hello world"}, nil)
	require.NoError(t, err, "Error waiting for published response")

	// Make sure the event was received.
	select {
	case err = <-errChan:
		require.NoError(t, err, "Event error")
	case <-time.After(200 * time.Millisecond):
		require.FailNow(t, "did not get published event")
	}

	err = subscriber.Unsubscribe(testTopic)
	require.NoError(t, err)
}

func TestPubSubWildcard(t *testing.T) {
	checkGoLeaks(t)
	// Connect subscriber session.
	subscriber := connectClient(t)

	// Check for feature support in router.
	has := subscriber.HasFeature(wamp.RoleBroker, wamp.FeaturePatternSub)
	require.Truef(t, has, "Broker does not support %s", wamp.FeaturePatternSub)

	errChan := make(chan error)
	eventHandler := func(event *wamp.Event) {
		if len(event.Arguments) == 0 {
			errChan <- errors.New("missing arg")
			return
		}
		arg, _ := wamp.AsString(event.Arguments[0])
		if arg != "hello world" {
			errChan <- errors.New("bad arg")
			return
		}
		origTopic, _ := wamp.AsURI(event.Details["topic"])
		if origTopic != testTopic {
			errChan <- errors.New("wrong original topic")
			return
		}
		errChan <- nil
	}

	// Subscribe to event with wildcard match.
	err := subscriber.Subscribe(testTopicWC, eventHandler, wamp.SetOption(nil, "match", "wildcard"))
	require.NoError(t, err)

	// Connect publisher session.
	publisher := connectClient(t)
	// Publish an event to something that matches by wildcard.
	publisher.Publish(testTopic, nil, wamp.List{"hello world"}, nil)

	// Make sure the event was received.
	select {
	case err = <-errChan:
	case <-time.After(200 * time.Millisecond):
		require.FailNow(t, "did not get published event")
	}
	require.NoError(t, err)

	err = subscriber.Unsubscribe(testTopicWC)
	require.NoError(t, err)
}

func TestUnsubscribeWrongTopic(t *testing.T) {
	checkGoLeaks(t)
	// Connect subscriber session.
	subscriber := connectClient(t)

	eventHandler := func(_ *wamp.Event) {}

	// Subscribe to event.
	err := subscriber.Subscribe(testTopic, eventHandler, nil)
	require.NoError(t, err)

	err = subscriber.Unsubscribe(testTopicWC)
	require.Error(t, err, "expected error unsubscribing from wrong topic")

	// Connect subscriber session2.
	subscriber2 := connectClient(t)

	// Subscribe to other event.
	topic2 := "nexus.test.topic2"
	err = subscriber2.Subscribe(topic2, eventHandler, nil)
	require.NoError(t, err)

	// Unsubscribe from other subscriber's topic.
	err = subscriber2.Unsubscribe(testTopic)
	require.Error(t, err, "expected error unsubscribing from other's topic")

	err = subscriber.Unsubscribe(testTopic)
	require.NoError(t, err)

	err = subscriber2.Unsubscribe(topic2)
	require.NoError(t, err)
}

func TestSubscribeBurst(t *testing.T) {
	checkGoLeaks(t)
	// Connect subscriber session.
	sub := connectClient(t)

	eventHandler := func(_ *wamp.Event) {}

	for i := range 10 {
		// Subscribe to event.
		topic := fmt.Sprintf("test.topic%d", i)
		err := sub.Subscribe(topic, eventHandler, nil)
		require.NoError(t, err)
	}

	for i := range 10 {
		// Subscribe to event.
		topic := fmt.Sprintf("test.topic%d", i)
		err := sub.Unsubscribe(topic)
		require.NoError(t, err)
	}
}

// TestSpecPubSubSubscribeInvalidURI verifies SUBSCRIBE with a malformed
// URI is rejected per spec §3.6.2 with wamp.error.invalid_uri.
func TestSpecPubSubSubscribeInvalidURI(t *testing.T) {
	checkGoLeaks(t)
	sub := connectClient(t)

	// Trailing dot violates URI shape.
	err := sub.Subscribe("nexus.bad.uri.", func(_ *wamp.Event) {}, nil)
	require.Error(t, err, "expected error subscribing to invalid URI")
	require.ErrorContains(t, err, string(wamp.ErrInvalidURI))
}

// TestSpecPubSubPublishNoSubscribers verifies a PUBLISH with acknowledge=true
// to a topic with no subscribers succeeds (no error, just no delivery).
func TestSpecPubSubPublishNoSubscribers(t *testing.T) {
	checkGoLeaks(t)
	pub := connectClient(t)

	err := pub.Publish("nexus.test.no.subscribers", wamp.Dict{wamp.OptAcknowledge: true},
		wamp.List{"silently dropped"}, nil)
	require.NoError(t, err, "publish to topic with no subscribers must succeed")
}

// TestSpecPubSubExcludeMeFalse verifies disclose=true / exclude_me=false:
// the publisher itself receives the event it published when exclude_me is
// explicitly disabled (spec §14.4.1).
func TestSpecPubSubExcludeMeFalse(t *testing.T) {
	checkGoLeaks(t)
	pub := connectClient(t)

	gotEvent := make(chan struct{}, 1)
	err := pub.Subscribe("nexus.test.self", func(_ *wamp.Event) {
		gotEvent <- struct{}{}
	}, nil)
	require.NoError(t, err)

	err = pub.Publish("nexus.test.self",
		wamp.Dict{wamp.OptAcknowledge: true, wamp.OptExcludeMe: false},
		wamp.List{"hi self"}, nil)
	require.NoError(t, err)

	select {
	case <-gotEvent:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("publisher with exclude_me=false did not receive own event")
	}

	require.NoError(t, pub.Unsubscribe("nexus.test.self"))
}

// TestSpecPubSubExcludeMeDefault verifies the default behavior is for the
// publisher to NOT receive its own event (spec §3.5.4 default).
func TestSpecPubSubExcludeMeDefault(t *testing.T) {
	checkGoLeaks(t)
	pub := connectClient(t)

	gotEvent := make(chan struct{}, 1)
	err := pub.Subscribe("nexus.test.self.default", func(_ *wamp.Event) {
		gotEvent <- struct{}{}
	}, nil)
	require.NoError(t, err)

	err = pub.Publish("nexus.test.self.default",
		wamp.Dict{wamp.OptAcknowledge: true},
		wamp.List{"goes nowhere"}, nil)
	require.NoError(t, err)

	select {
	case <-gotEvent:
		t.Fatal("publisher received own event with default exclude_me")
	case <-time.After(150 * time.Millisecond):
	}

	require.NoError(t, pub.Unsubscribe("nexus.test.self.default"))
}

// TestSpecPubSubArgsRoundTrip verifies arguments and argumentskw survive
// the broker untouched. This catches serializer/deserializer bugs at the
// router boundary.
func TestSpecPubSubArgsRoundTrip(t *testing.T) {
	checkGoLeaks(t)
	sub := connectClient(t)
	pub := connectClient(t)

	type got struct {
		args wamp.List
		kw   wamp.Dict
	}
	gotChan := make(chan got, 1)
	err := sub.Subscribe("nexus.test.args", func(ev *wamp.Event) {
		gotChan <- got{args: ev.Arguments, kw: ev.ArgumentsKw}
	}, nil)
	require.NoError(t, err)

	wantArgs := wamp.List{"a", int64(2), 3.14, true}
	wantKw := wamp.Dict{"key": "value", "n": int64(42)}
	err = pub.Publish("nexus.test.args",
		wamp.Dict{wamp.OptAcknowledge: true}, wantArgs, wantKw)
	require.NoError(t, err)

	select {
	case g := <-gotChan:
		require.Equal(t, "a", g.args[0])
		v, _ := wamp.AsInt64(g.args[1])
		require.Equal(t, int64(2), v)
		require.Equal(t, "value", g.kw["key"])
		n, _ := wamp.AsInt64(g.kw["n"])
		require.Equal(t, int64(42), n)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("event with args+kwargs not delivered")
	}

	require.NoError(t, sub.Unsubscribe("nexus.test.args"))
}
