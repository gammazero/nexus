package router //nolint:testpackage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// captureRealmAuthenticator is a stub authenticator that records the details
// dict it receives, then returns a WELCOME. Used to verify that the realm adds
// "nexus.session.realm" to the details it passes to Authenticate.
type captureRealmAuthenticator struct {
	gotDetails wamp.Dict
}

func (a *captureRealmAuthenticator) Authenticate(sid wamp.ID, details wamp.Dict, _ wamp.Peer) (*wamp.Welcome, error) {
	// Copy the dict so the caller can't mutate our recorded view via the returned
	// welcome.
	a.gotDetails = wamp.Dict{}
	for k, v := range details {
		a.gotDetails[k] = v
	}
	return &wamp.Welcome{
		ID:      sid,
		Details: wamp.Dict{"authrole": "tester"},
	}, nil
}

func (a *captureRealmAuthenticator) AuthMethod() string { return "capture-realm" }

// TestRealmExposesURIToAuthenticator pins that realm.authClient passes the
// target realm URI to the authenticator, under the "nexus.session.realm" key
// of the details it gives Authenticate. Dynamic authenticators that share one
// instance across multiple realms (e.g. a single auth-server delegate) rely on
// this to route per-realm.
func TestRealmExposesURIToAuthenticator(t *testing.T) {
	const realmURI = wamp.URI("nexus.test.realm.with.auth.capture")

	captor := &captureRealmAuthenticator{}

	r, err := NewRouter(&Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:              realmURI,
				StrictURI:        false,
				Authenticators:   []auth.Authenticator{captor},
				RequireLocalAuth: true, // LinkedPeers are local, so opt into auth
			},
		},
	}, logger)
	require.NoError(t, err)
	// Router shutdown also disposes attached peers; do NOT call peer.Close
	// manually here, since localPeer panics on double-close.
	t.Cleanup(func() { r.Close() })

	client, server := transport.LinkedPeers()

	go func() { _ = r.Attach(server) }()

	hello := &wamp.Hello{
		Realm: realmURI,
		Details: wamp.Dict{
			"authmethods": wamp.List{"capture-realm"},
			"roles":       wamp.Dict{"caller": wamp.Dict{}},
		},
	}
	client.Send() <- hello

	select {
	case msg := <-client.Recv():
		_, ok := msg.(*wamp.Welcome)
		require.True(t, ok, "expected WELCOME, got %T", msg)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for WELCOME")
	}

	require.NotNil(t, captor.gotDetails, "Authenticate was never called")
	got, ok := captor.gotDetails["nexus.session.realm"]
	require.True(t, ok, "nexus.session.realm missing from authenticator details (got: %v)", captor.gotDetails)
	require.Equal(t, string(realmURI), got,
		"nexus.session.realm should equal the HELLO target realm")

	// Sanity: the underscore-prefixed key from the previous internal shape is NOT
	// what we use anymore, to guard against drift.
	_, oldKey := captor.gotDetails["_realm"]
	require.False(t, oldKey, "legacy _realm key should not be set")
}

// TestRealmURINotInSessionDetails pins that the realm URI given to the
// authenticator is not added to the session details, which other clients see
// in session meta events and meta procedure results. WAMP requires
// implementation-specific detail keys to match _[a-z0-9_]{3,}, and the client
// already knows the realm it joined.
func TestRealmURINotInSessionDetails(t *testing.T) {
	const realmURI = wamp.URI("nexus.test.realm.with.auth.capture")

	r, err := NewRouter(&Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:              realmURI,
				Authenticators:   []auth.Authenticator{&captureRealmAuthenticator{}},
				RequireLocalAuth: true, // LinkedPeers are local, so opt into auth
			},
		},
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })

	join := func() wamp.Peer {
		client, server := transport.LinkedPeers()
		go func() { _ = r.Attach(server) }()
		client.Send() <- &wamp.Hello{
			Realm: realmURI,
			Details: wamp.Dict{
				"authmethods": wamp.List{"capture-realm"},
				"roles":       wamp.Dict{"subscriber": wamp.Dict{}},
			},
		}
		msg, err := wamp.RecvTimeout(client, 2*time.Second)
		require.NoError(t, err)
		_, ok := msg.(*wamp.Welcome)
		require.True(t, ok, "expected WELCOME, got %T", msg)
		return client
	}

	watcher := join()
	watcher.Send() <- &wamp.Subscribe{Request: 1, Topic: wamp.MetaEventSessionOnJoin}
	msg, err := wamp.RecvTimeout(watcher, 2*time.Second)
	require.NoError(t, err)
	_, ok := msg.(*wamp.Subscribed)
	require.True(t, ok, "expected SUBSCRIBED, got %T", msg)

	join()

	msg, err = wamp.RecvTimeout(watcher, 2*time.Second)
	require.NoError(t, err)
	event, ok := msg.(*wamp.Event)
	require.True(t, ok, "expected EVENT, got %T", msg)
	details, ok := wamp.AsDict(event.Arguments[0])
	require.True(t, ok, "on_join payload is not a dict")
	require.NotContains(t, details, "nexus.session.realm",
		"realm URI for the authenticator leaked into session details")
}
