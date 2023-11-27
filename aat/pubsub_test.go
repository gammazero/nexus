package aat_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dtegapp/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
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

	for i := 0; i < 10; i++ {
		// Subscribe to event.
		topic := fmt.Sprintf("test.topic%d", i)
		err := sub.Subscribe(topic, eventHandler, nil)
		require.NoError(t, err)
	}

	for i := 0; i < 10; i++ {
		// Subscribe to event.
		topic := fmt.Sprintf("test.topic%d", i)
		err := sub.Unsubscribe(topic)
		require.NoError(t, err)
	}
}
