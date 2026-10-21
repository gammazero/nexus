package test_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

// TestSpecPatternSubscribePrefix verifies SUBSCRIBE with match=prefix
// receives events on any topic that starts with the registered prefix
// (spec §14.4.7).
func TestSpecPatternSubscribePrefix(t *testing.T) {
	checkGoLeaks(t)

	sub := connectClient(t)

	got := make(chan wamp.URI, 4)
	handler := func(ev *wamp.Event) {
		topic, _ := wamp.AsURI(ev.Details["topic"])
		got <- topic
	}
	require.NoError(t, sub.Subscribe("com.app.alarm",
		handler, wamp.SetOption(nil, wamp.OptMatch, wamp.MatchPrefix)))

	pub := connectClient(t)
	require.NoError(t, pub.Publish("com.app.alarm.fire",
		wamp.Dict{wamp.OptAcknowledge: true}, wamp.List{1}, nil))
	require.NoError(t, pub.Publish("com.app.alarm.smoke",
		wamp.Dict{wamp.OptAcknowledge: true}, wamp.List{2}, nil))
	require.NoError(t, pub.Publish("com.other.event",
		wamp.Dict{wamp.OptAcknowledge: true}, wamp.List{3}, nil))

	seen := map[wamp.URI]bool{}
	for range 2 {
		select {
		case topic := <-got:
			seen[topic] = true
		case <-time.After(500 * time.Millisecond):
			t.Fatal("did not receive expected prefix event")
		}
	}
	require.True(t, seen["com.app.alarm.fire"])
	require.True(t, seen["com.app.alarm.smoke"])

	select {
	case topic := <-got:
		t.Fatalf("unexpected event for %q (should not match prefix)", topic)
	case <-time.After(150 * time.Millisecond):
	}

	require.NoError(t, sub.Unsubscribe("com.app.alarm"))
}

// TestSpecPatternSubscribeWildcard verifies SUBSCRIBE with match=wildcard
// matches segments treated as wildcards (empty between dots).
func TestSpecPatternSubscribeWildcard(t *testing.T) {
	checkGoLeaks(t)

	sub := connectClient(t)
	got := make(chan wamp.URI, 4)
	handler := func(ev *wamp.Event) {
		topic, _ := wamp.AsURI(ev.Details["topic"])
		got <- topic
	}
	// "com..fire" matches anything in the second segment.
	require.NoError(t, sub.Subscribe("com..fire",
		handler, wamp.SetOption(nil, wamp.OptMatch, wamp.MatchWildcard)))

	pub := connectClient(t)
	require.NoError(t, pub.Publish("com.alarm.fire",
		wamp.Dict{wamp.OptAcknowledge: true}, wamp.List{1}, nil))
	require.NoError(t, pub.Publish("com.smoke.fire",
		wamp.Dict{wamp.OptAcknowledge: true}, wamp.List{2}, nil))
	// Should NOT match: different last segment.
	require.NoError(t, pub.Publish("com.alarm.smoke",
		wamp.Dict{wamp.OptAcknowledge: true}, wamp.List{3}, nil))

	seen := map[wamp.URI]bool{}
	for range 2 {
		select {
		case topic := <-got:
			seen[topic] = true
		case <-time.After(500 * time.Millisecond):
			t.Fatal("did not receive expected wildcard event")
		}
	}
	require.True(t, seen["com.alarm.fire"])
	require.True(t, seen["com.smoke.fire"])

	select {
	case topic := <-got:
		t.Fatalf("unexpected event for %q (wrong last segment)", topic)
	case <-time.After(150 * time.Millisecond):
	}

	require.NoError(t, sub.Unsubscribe("com..fire"))
}

// TestSpecPatternSubscribeExactDoesNotPrefixMatch verifies the default
// match=exact requires byte-equal topic, not prefix.
func TestSpecPatternSubscribeExactDoesNotPrefixMatch(t *testing.T) {
	checkGoLeaks(t)

	sub := connectClient(t)
	gotChan := make(chan struct{}, 1)
	require.NoError(t, sub.Subscribe("com.app",
		func(_ *wamp.Event) { gotChan <- struct{}{} }, nil))

	pub := connectClient(t)
	require.NoError(t, pub.Publish("com.app.fire",
		wamp.Dict{wamp.OptAcknowledge: true}, wamp.List{1}, nil))

	select {
	case <-gotChan:
		t.Fatal("exact subscriber received an extended-topic event")
	case <-time.After(150 * time.Millisecond):
	}

	require.NoError(t, sub.Unsubscribe("com.app"))
}

// TestSpecPatternRegisterPrefix verifies REGISTER with match=prefix
// dispatches calls for any procedure starting with the prefix
// (spec §14.3.7).
func TestSpecPatternRegisterPrefix(t *testing.T) {
	checkGoLeaks(t)

	callee := connectClient(t)
	got := make(chan wamp.URI, 4)
	handler := func(_ context.Context, inv *wamp.Invocation) client.InvokeResult {
		proc, _ := wamp.AsURI(inv.Details["procedure"])
		got <- proc
		return client.InvokeResult{Args: wamp.List{string(proc)}}
	}
	require.NoError(t, callee.Register("com.svc.",
		handler, wamp.SetOption(nil, wamp.OptMatch, wamp.MatchPrefix)))

	caller := connectClient(t)
	for _, proc := range []string{"com.svc.foo", "com.svc.bar.baz"} {
		_, err := caller.Call(context.Background(), proc, nil, nil, nil, nil)
		require.NoErrorf(t, err, "call to %q failed", proc)
	}

	seen := map[wamp.URI]bool{}
	for range 2 {
		seen[<-got] = true
	}
	require.True(t, seen["com.svc.foo"])
	require.True(t, seen["com.svc.bar.baz"])

	require.NoError(t, callee.Unregister("com.svc."))
}

// TestSpecPatternRegisterWildcard verifies REGISTER with match=wildcard.
func TestSpecPatternRegisterWildcard(t *testing.T) {
	checkGoLeaks(t)

	callee := connectClient(t)
	got := make(chan wamp.URI, 4)
	handler := func(_ context.Context, inv *wamp.Invocation) client.InvokeResult {
		proc, _ := wamp.AsURI(inv.Details["procedure"])
		got <- proc
		return client.InvokeResult{}
	}
	require.NoError(t, callee.Register("com..ping",
		handler, wamp.SetOption(nil, wamp.OptMatch, wamp.MatchWildcard)))

	caller := connectClient(t)
	_, err := caller.Call(context.Background(), "com.alpha.ping", nil, nil, nil, nil)
	require.NoError(t, err)
	_, err = caller.Call(context.Background(), "com.beta.ping", nil, nil, nil, nil)
	require.NoError(t, err)

	// Should NOT match: wrong last segment.
	_, err = caller.Call(context.Background(), "com.alpha.pong", nil, nil, nil, nil)
	require.Error(t, err, "wildcard pattern must not match wrong segment")

	seen := map[wamp.URI]bool{}
	for range 2 {
		seen[<-got] = true
	}
	require.True(t, seen["com.alpha.ping"])
	require.True(t, seen["com.beta.ping"])

	require.NoError(t, callee.Unregister("com..ping"))
}
