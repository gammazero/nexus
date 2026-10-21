package test_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/wamp"
)

// newDiscloseRouter spins up a dedicated router with AllowDisclose=true so
// disclose_me / disclose_caller / disclose_publisher options are honored.
// The default testRealm has AllowDisclose=false, which would reject these.
// Close is registered as a t.Cleanup so it runs AFTER client cleanups (LIFO).
func newDiscloseRouter(t *testing.T) router.Router {
	t.Helper()
	cfg := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			{
				URI:           "nexus.test.disclose",
				StrictURI:     false,
				AnonymousAuth: true,
				AllowDisclose: true,
			},
		},
	}
	r, err := router.NewRouter(cfg, rtrLogger)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })
	return r
}

func discloseConnect(t *testing.T, r router.Router) *client.Client {
	t.Helper()
	cli, err := client.ConnectLocal(r, client.Config{
		Realm:           "nexus.test.disclose",
		ResponseTimeout: clientResponseTimeout,
		Logger:          cliLogger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { cli.Close() })
	return cli
}

// TestSpecDiscloseCaller verifies CALL with disclose_me=true delivers the
// caller's session ID in INVOCATION.Details (spec §14.3.5).
func TestSpecDiscloseCaller(t *testing.T) {
	if scheme != "" {
		t.Skip("dedicated router; runs only on local in-process scheme")
	}
	checkGoLeaks(t)

	r := newDiscloseRouter(t)

	callee := discloseConnect(t, r)
	caller := discloseConnect(t, r)

	const procName = "spec.disclose.caller"
	gotCaller := make(chan wamp.ID, 1)
	require.NoError(t, callee.Register(procName, func(_ context.Context, inv *wamp.Invocation) client.InvokeResult {
		id, _ := wamp.AsID(inv.Details["caller"])
		gotCaller <- id
		return client.InvokeResult{}
	}, nil))

	_, err := caller.Call(context.Background(), procName,
		wamp.Dict{wamp.OptDiscloseMe: true}, nil, nil, nil)
	require.NoError(t, err)

	select {
	case id := <-gotCaller:
		require.Equal(t, caller.ID(), id, "INVOCATION must carry caller session ID")
	case <-time.After(time.Second):
		t.Fatal("callee did not see disclosed caller ID")
	}

	require.NoError(t, callee.Unregister(procName))
}

// TestSpecDiscloseCallerAbsentByDefault verifies that without disclose_me
// the INVOCATION does NOT carry the caller field.
func TestSpecDiscloseCallerAbsentByDefault(t *testing.T) {
	if scheme != "" {
		t.Skip("dedicated router; runs only on local in-process scheme")
	}
	checkGoLeaks(t)

	r := newDiscloseRouter(t)

	callee := discloseConnect(t, r)
	caller := discloseConnect(t, r)

	const procName = "spec.disclose.absent"
	gotDetails := make(chan wamp.Dict, 1)
	require.NoError(t, callee.Register(procName, func(_ context.Context, inv *wamp.Invocation) client.InvokeResult {
		gotDetails <- inv.Details
		return client.InvokeResult{}
	}, nil))

	_, err := caller.Call(context.Background(), procName, nil, nil, nil, nil)
	require.NoError(t, err)

	select {
	case details := <-gotDetails:
		_, has := details["caller"]
		require.False(t, has, "INVOCATION must NOT include caller field without disclose_me")
	case <-time.After(time.Second):
		t.Fatal("call did not deliver INVOCATION")
	}

	require.NoError(t, callee.Unregister(procName))
}

// TestSpecDisclosePublisher verifies PUBLISH with disclose_me=true delivers
// the publisher's session ID in EVENT.Details (spec §14.4.4).
func TestSpecDisclosePublisher(t *testing.T) {
	if scheme != "" {
		t.Skip("dedicated router; runs only on local in-process scheme")
	}
	checkGoLeaks(t)

	r := newDiscloseRouter(t)

	sub := discloseConnect(t, r)
	pub := discloseConnect(t, r)

	const topic = "spec.disclose.topic"
	gotPub := make(chan wamp.ID, 1)
	require.NoError(t, sub.Subscribe(topic, func(ev *wamp.Event) {
		id, _ := wamp.AsID(ev.Details["publisher"])
		gotPub <- id
	}, nil))

	require.NoError(t, pub.Publish(topic,
		wamp.Dict{wamp.OptAcknowledge: true, wamp.OptDiscloseMe: true},
		wamp.List{"hello"}, nil))

	select {
	case id := <-gotPub:
		require.Equal(t, pub.ID(), id, "EVENT must carry publisher session ID")
	case <-time.After(time.Second):
		t.Fatal("subscriber did not see disclosed publisher ID")
	}

	require.NoError(t, sub.Unsubscribe(topic))
}

// TestSpecDiscloseRejectedWhenDisallowed verifies that a realm with
// AllowDisclose=false rejects disclose_me=true on PUBLISH with the
// option-disallowed error URI.
func TestSpecDiscloseRejectedWhenDisallowed(t *testing.T) {
	checkGoLeaks(t)
	// The default testRealm in main_test.go has AllowDisclose=false.

	pub := connectClient(t)
	err := pub.Publish(testTopic,
		wamp.Dict{wamp.OptAcknowledge: true, wamp.OptDiscloseMe: true},
		wamp.List{"x"}, nil)
	require.Error(t, err, "AllowDisclose=false realm must reject disclose_me=true")
}
