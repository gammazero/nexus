package router //nolint:testpackage

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

func TestFilterBlacklist(t *testing.T) {
	const (
		shouldAllowMsg = "Publish to session should be allowed"
		shouldDenyMsg  = "Publish to session should be denied"
	)

	allowedID := wamp.ID(1234)
	allowedAuthID := "root"
	allowedAuthRole := "admin"

	blacklistID := wamp.ID(9876)
	blacklistAuthID := "guest"
	blacklistAuthRole := "user"

	pub := &wamp.Publish{
		Request: wamp.GlobalID(),
		Options: wamp.Dict{
			"exclude":          wamp.List{blacklistID},
			"exclude_authid":   wamp.List{blacklistAuthID},
			"exclude_authrole": wamp.List{blacklistAuthRole},
		},
		Topic: wamp.URI("blacklist.test"),
	}

	pf := NewSimplePublishFilter(pub)

	details := wamp.Dict{
		wamp.DetailAuthID:   allowedAuthID,
		wamp.DetailAuthRole: allowedAuthRole,
		"misc":              "other",
	}
	sess := &wamp.Session{ID: allowedID, Details: details}
	require.True(t, pf.Allowed(sess), shouldAllowMsg)

	sess = &wamp.Session{ID: blacklistID, Details: details}
	// Check that session is denied by ID.
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	sess = &wamp.Session{ID: allowedID, Details: details}
	// Check that session is denied by authid.
	sess.Details[wamp.DetailAuthID] = blacklistAuthID
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	// Check that session is denied by authrole.
	sess.Details[wamp.DetailAuthID] = allowedAuthID
	sess.Details[wamp.DetailAuthRole] = blacklistAuthRole
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	// Check that session is allowed by not having value in blacklist.
	delete(sess.Details, wamp.DetailAuthRole)
	require.True(t, pf.Allowed(sess), shouldAllowMsg)
}

func TestFilterWhitelist(t *testing.T) {
	const (
		shouldAllowMsg = "Publish to session should be allowed"
		shouldDenyMsg  = "Publish to session should be denied"
	)

	allowedID := wamp.ID(1234)
	allowedAuthID := "root"
	allowedAuthRole := "admin"

	deniedID := wamp.ID(9876)
	deniedAuthID := "guest"
	deniedAuthRole := "user"

	pub := &wamp.Publish{
		Request: wamp.GlobalID(),
		Options: wamp.Dict{
			"eligible":          wamp.List{allowedID},
			"eligible_authid":   wamp.List{allowedAuthID},
			"eligible_authrole": wamp.List{allowedAuthRole},
		},
		Topic: wamp.URI("whitelist.test"),
	}

	pf := NewSimplePublishFilter(pub)

	details := wamp.Dict{
		wamp.DetailAuthID:   allowedAuthID,
		wamp.DetailAuthRole: allowedAuthRole,
		"misc":              "other",
	}
	sess := &wamp.Session{ID: allowedID, Details: details}
	require.True(t, pf.Allowed(sess), shouldAllowMsg)

	sess = &wamp.Session{ID: deniedID, Details: details}
	// Check that session is denied by ID.
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	sess = &wamp.Session{ID: allowedID, Details: details}
	// Check that session is denied by authid.
	sess.Details[wamp.DetailAuthID] = deniedAuthID
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	// Check that session is denied by authrole.
	sess.Details[wamp.DetailAuthID] = allowedAuthID
	sess.Details[wamp.DetailAuthRole] = deniedAuthRole
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	// Check that session is denied by not having value in whitelise.
	delete(sess.Details, wamp.DetailAuthRole)
	require.False(t, pf.Allowed(sess), shouldDenyMsg)
}

func TestFilterBlackWhitelistPrecedence(t *testing.T) {
	const (
		shouldAllowMsg = "Publish to session should be allowed"
		shouldDenyMsg  = "Publish to session should be denied"
	)

	allowedID := wamp.ID(1234)
	allowedAuthID := "root"
	allowedAuthRole := "admin"

	blacklistID := wamp.ID(9876)
	blacklistAuthID := "guest"
	blacklistAuthRole := "user"

	pub := &wamp.Publish{
		Request: wamp.GlobalID(),
		Options: wamp.Dict{
			"exclude":           wamp.List{blacklistID},
			"exclude_authid":    wamp.List{blacklistAuthID},
			"exclude_authrole":  wamp.List{blacklistAuthRole},
			"eligible":          wamp.List{allowedID, blacklistID},
			"eligible_authid":   wamp.List{allowedAuthID, blacklistAuthID},
			"eligible_authrole": wamp.List{allowedAuthRole},
		},
		Topic: wamp.URI("whitelist.test"),
	}

	pf := NewSimplePublishFilter(pub)

	details := wamp.Dict{
		wamp.DetailAuthID:   allowedAuthID,
		wamp.DetailAuthRole: allowedAuthRole,
		"misc":              "other",
	}

	sess := &wamp.Session{ID: allowedID, Details: details}
	require.True(t, pf.Allowed(sess), shouldAllowMsg)

	sess = &wamp.Session{ID: blacklistID, Details: details}
	// Check that session is denied by ID even thought ID is also in whitelist.
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	sess = &wamp.Session{ID: allowedID, Details: details}
	// Check that session is denied by authid even though also whitelisted.
	sess.Details[wamp.DetailAuthID] = blacklistAuthID
	require.False(t, pf.Allowed(sess), shouldDenyMsg)
}
