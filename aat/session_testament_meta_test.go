package aat_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

const (
	metaAddTestament    = string(wamp.MetaProcSessionAddTestament)
	metaFlushTestaments = string(wamp.MetaProcSessionFlushTestaments)
)

func TestAddTestament(t *testing.T) {
	checkGoLeaks(t)
	// Connect subscriber session.
	subscriber := connectClient(t)

	// Check for feature support in router.
	has := subscriber.HasFeature(wamp.RoleDealer, wamp.FeatureTestamentMetaAPI)
	require.Truef(t, has, "Dealer does not support %s", wamp.FeatureTestamentMetaAPI)

	eventChan := make(chan *wamp.Event)
	err := subscriber.SubscribeChan("testament.test", eventChan, nil)
	require.NoError(t, err)
	sess := connectClient(t)
	tstmtArgs := wamp.List{
		"testament.test",
		wamp.List{
			"foo",
		},
		wamp.Dict{},
	}
	_, err = sess.Call(context.Background(), metaAddTestament, nil, tstmtArgs, nil, nil)
	require.NoError(t, err)

	// Disconnect client to trigger testament
	err = sess.Close()
	require.NoError(t, err)

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
		err = checkEvent(event)
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		require.FailNow(t, "did not get published event")
	}
}

func TestAddFlushTestament(t *testing.T) {
	checkGoLeaks(t)
	// Connect subscriber session.
	subscriber := connectClient(t)

	eventChan := make(chan *wamp.Event)
	err := subscriber.SubscribeChan("testament.test", eventChan, nil)
	require.NoError(t, err)
	sess := connectClient(t)
	tstmtArgs := wamp.List{
		"testament.test",
		wamp.List{
			"foo",
		},
		wamp.Dict{},
	}
	_, err = sess.Call(context.Background(), metaAddTestament, nil, tstmtArgs, nil, nil)
	require.NoError(t, err, "Failed to add testament")
	_, err = sess.Call(context.Background(), metaFlushTestaments, nil, nil, nil, nil)
	require.NoError(t, err, "Failed to flush testament")

	// Disconnect client to trigge testament, which should not exist since it
	// was flushed above.
	err = sess.Close()
	require.NoError(t, err)

	// Make sure the event was received.
	select {
	case <-eventChan:
		require.FailNow(t, "should not have received event")
	case <-time.After(200 * time.Millisecond):
	}
}
