package test_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

// TestSpecPPTCallRoundTrip verifies a CALL with ppt_scheme set carries the PPT
// options and payload through to the callee untouched (spec §14.7). Exercises
// the non-native (msgpack) PPT serializer so the test covers the
// binary-payload path across the full transport matrix (local / ws+json /
// ws+msgpack / ws+cbor / tcp+msgpack / tcp+cbor / wss / tcps / unix). Native
// is covered by TestSpecPPTPublishRoundTrip.
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

// TestSpecPPTPublishRoundTrip verifies PUBLISH with ppt_scheme carries the PPT
// options and payload through to the subscriber untouched. Uses
// ppt_serializer="native": args/kwargs ride the wire serializer directly with
// no envelope, which complements the msgpack-envelope path covered by
// TestSpecPPTCallRoundTrip.
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

// TestSpecPPTE2EEPublishRoundTrip verifies that an end-to-end encrypted
// PUBLISH (ppt_scheme "wamp") reaches the subscriber intact across the whole
// transport matrix. Over a JSON wire, the binary payload arrives as a base64
// string.
func TestSpecPPTE2EEPublishRoundTrip(t *testing.T) {
	checkGoLeaks(t)

	sub := connectClient(t)
	gotArgs := make(chan wamp.List, 1)
	const topic = "spec.ppt.e2ee.topic"
	require.NoError(t, sub.Subscribe(topic, func(ev *wamp.Event) {
		gotArgs <- ev.Arguments
	}, nil))

	pub := connectClient(t)
	opts := wamp.Dict{
		wamp.OptAcknowledge:   true,
		wamp.OptPPTScheme:     client.WampPPTScheme,
		wamp.OptPPTSerializer: "cbor",
	}
	require.NoError(t, pub.Publish(topic, opts, wamp.List{"secret"}, nil))

	select {
	case args := <-gotArgs:
		require.Equal(t, wamp.List{"secret"}, args)
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive EVENT")
	}

	require.NoError(t, sub.Unsubscribe(topic))
}

// TestSpecPPTYieldErrorHasNoPayload verifies that when a callee's PPT result
// cannot be prepared, the ERROR sent instead does not carry the result
// payload, which the callee asked to send encrypted.
func TestSpecPPTYieldErrorHasNoPayload(t *testing.T) {
	checkGoLeaks(t)

	callee := connectClient(t)
	const procName = "spec.ppt.badresult"
	require.NoError(t, callee.Register(procName, func(context.Context, *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{
			Args:   wamp.List{"secret"},
			Kwargs: wamp.Dict{"k": "secret"},
			// msgpack is not an E2EE serializer, so packing fails.
			Options: wamp.Dict{
				wamp.OptPPTScheme:     client.WampPPTScheme,
				wamp.OptPPTSerializer: "msgpack",
			},
		}
	}, nil))

	caller := connectClient(t)
	_, err := caller.Call(context.Background(), procName, nil, nil, nil, nil)
	var rpcErr client.RPCError
	require.ErrorAs(t, err, &rpcErr)
	require.Empty(t, rpcErr.Err.Arguments, "ERROR must not carry the payload")
	require.Empty(t, rpcErr.Err.ArgumentsKw, "ERROR must not carry the payload")

	require.NoError(t, callee.Unregister(procName))
}
