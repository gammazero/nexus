package test_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

// TestSpecPPTCallRoundTrip verifies a CALL with ppt_scheme set carries the
// PPT options and payload through to the callee untouched (spec §14.7).
// Exercises the non-native (msgpack) PPT serializer so the test
// covers the binary-payload path across the full transport matrix
// (local / ws+json / ws+msgpack / ws+cbor / tcp+msgpack / tcp+cbor /
// wss / tcps / unix). Native is covered by TestSpecPPTPublishRoundTrip.
func TestSpecPPTCallRoundTrip(t *testing.T) {
	checkGoLeaks(t)

	callee := connectClient(t)

	gotScheme := make(chan string, 1)
	gotArgs := make(chan wamp.List, 1)
	const procName = "spec.ppt.echo"

	require.NoError(t, callee.Register(procName, func(_ context.Context, inv *wamp.Invocation) client.InvokeResult {
		s, _ := wamp.AsString(inv.Details[wamp.OptPPTScheme])
		gotScheme <- s
		gotArgs <- inv.Arguments
		// Echo back with the same PPT options.
		return client.InvokeResult{
			Args:   inv.Arguments,
			Kwargs: inv.ArgumentsKw,
			Options: wamp.Dict{
				wamp.OptPPTScheme:     "mqtt",
				wamp.OptPPTSerializer: "msgpack",
			},
		}
	}, nil))

	caller := connectClient(t)
	opts := wamp.Dict{
		wamp.OptPPTScheme:     "mqtt",
		wamp.OptPPTSerializer: "msgpack",
	}
	res, err := caller.Call(context.Background(), procName, opts,
		wamp.List{"opaque-payload", int64(7)}, wamp.Dict{"k": "v"}, nil)
	require.NoError(t, err)

	select {
	case s := <-gotScheme:
		require.Equal(t, "mqtt", s, "callee must see ppt_scheme in INVOCATION details")
	case <-time.After(time.Second):
		t.Fatal("callee did not receive INVOCATION")
	}

	args := <-gotArgs
	require.Equal(t, "opaque-payload", args[0])

	require.NotEmpty(t, res.Arguments, "RESULT carries the payload back")

	require.NoError(t, callee.Unregister(procName))
}

// TestSpecPPTPublishRoundTrip verifies PUBLISH with ppt_scheme carries the
// PPT options and payload through to the subscriber untouched.
// Uses ppt_serializer="native" — args/kwargs ride the wire serializer
// directly with no envelope, which complements the msgpack-envelope
// path covered by TestSpecPPTCallRoundTrip.
func TestSpecPPTPublishRoundTrip(t *testing.T) {
	checkGoLeaks(t)

	sub := connectClient(t)

	type evtData struct {
		scheme string
		args   wamp.List
	}
	gotEvent := make(chan evtData, 1)
	const topic = "spec.ppt.topic"

	require.NoError(t, sub.Subscribe(topic, func(ev *wamp.Event) {
		s, _ := wamp.AsString(ev.Details[wamp.OptPPTScheme])
		gotEvent <- evtData{scheme: s, args: ev.Arguments}
	}, nil))

	pub := connectClient(t)
	opts := wamp.Dict{
		wamp.OptAcknowledge:   true,
		wamp.OptPPTScheme:     "mqtt",
		wamp.OptPPTSerializer: "native",
	}
	require.NoError(t, pub.Publish(topic, opts,
		wamp.List{"opaque-payload"}, nil))

	select {
	case e := <-gotEvent:
		require.Equal(t, "mqtt", e.scheme, "EVENT must carry ppt_scheme through")
		require.Equal(t, "opaque-payload", e.args[0])
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive EVENT")
	}

	require.NoError(t, sub.Unsubscribe(topic))
}
