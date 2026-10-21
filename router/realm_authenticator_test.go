package router //nolint:testpackage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// captureRealmAuthenticator is a stub authenticator that records the
// details dict it receives, then returns a WELCOME. Used to verify that
// the realm injects "nexus.session.realm" into the details before
// calling Authenticate.
type captureRealmAuthenticator struct {
	gotDetails wamp.Dict
}

func (a *captureRealmAuthenticator) Authenticate(sid wamp.ID, details wamp.Dict, _ wamp.Peer) (*wamp.Welcome, error) {
	// Copy the dict so the caller can't mutate our recorded view via the
	// returned welcome.
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

// TestRealmExposesURIToAuthenticator pins that realm.authClient injects
// the target realm URI into the HELLO Details under the
// "nexus.session.realm" key before invoking the authenticator. Dynamic
// authenticators that share one instance across multiple realms (e.g.
// a single auth-server delegate) rely on this to route per-realm.
func TestRealmExposesURIToAuthenticator(t *testing.T) {
	const realmURI = wamp.URI("nexus.test.realm.with.auth.capture")

	captor := &captureRealmAuthenticator{}

	r, err := NewRouter(&Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:              realmURI,
				StrictURI:        false,
				Authenticators:   []auth.Authenticator{captor},
				RequireLocalAuth: true, // LinkedPeers are local — opt into auth
			},
		},
	}, logger)
	require.NoError(t, err)
	// Router shutdown also disposes attached peers; do NOT call peer.Close
	// manually here — main's localPeer panics on double-close.
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

	// Sanity: the underscore-prefixed key from the previous internal
	// shape is NOT what we use anymore — guards against drift.
	_, oldKey := captor.gotDetails["_realm"]
	require.False(t, oldKey, "legacy _realm key should not be set")
}
