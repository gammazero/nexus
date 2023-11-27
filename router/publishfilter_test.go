package router

import (
	"testing"

	"github.com/dtegapp/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

func TestFilterBlacklist(t *testing.T) {
	const (
		shouldAllowMsg = "Publish to session should be allowed"
		shouldDenyMsg  = "Publish to session should be denied"
	)

	allowedID := wamp.ID(1234)
	allowedAuthid := "root"
	allowedAuthrole := "admin"

	blacklistID := wamp.ID(9876)
	blacklistAuthid := "guest"
	blacklistAuthrole := "user"

	pub := &wamp.Publish{
		Request: wamp.GlobalID(),
		Options: wamp.Dict{
			"exclude":          wamp.List{blacklistID},
			"exclude_authid":   wamp.List{blacklistAuthid},
			"exclude_authrole": wamp.List{blacklistAuthrole},
		},
		Topic: wamp.URI("blacklist.test"),
	}

	pf := NewSimplePublishFilter(pub)

	details := wamp.Dict{
		"authid":   allowedAuthid,
		"authrole": allowedAuthrole,
		"misc":     "other",
	}
	sess := &wamp.Session{ID: allowedID, Details: details}
	require.True(t, pf.Allowed(sess), shouldAllowMsg)

	sess = &wamp.Session{ID: blacklistID, Details: details}
	// Check that session is denied by ID.
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	sess = &wamp.Session{ID: allowedID, Details: details}
	// Check that session is denied by authid.
	sess.Details["authid"] = blacklistAuthid
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	// Check that session is denied by authrole.
	sess.Details["authid"] = allowedAuthid
	sess.Details["authrole"] = blacklistAuthrole
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	// Check that session is allowed by not having value in blacklist.
	delete(sess.Details, "authrole")
	require.True(t, pf.Allowed(sess), shouldAllowMsg)
}

func TestFilterWhitelist(t *testing.T) {
	const (
		shouldAllowMsg = "Publish to session should be allowed"
		shouldDenyMsg  = "Publish to session should be denied"
	)

	allowedID := wamp.ID(1234)
	allowedAuthid := "root"
	allowedAuthrole := "admin"

	deniedID := wamp.ID(9876)
	deniedAuthid := "guest"
	deniedAuthrole := "user"

	pub := &wamp.Publish{
		Request: wamp.GlobalID(),
		Options: wamp.Dict{
			"eligible":          wamp.List{allowedID},
			"eligible_authid":   wamp.List{allowedAuthid},
			"eligible_authrole": wamp.List{allowedAuthrole},
		},
		Topic: wamp.URI("whitelist.test"),
	}

	pf := NewSimplePublishFilter(pub)

	details := wamp.Dict{
		"authid":   allowedAuthid,
		"authrole": allowedAuthrole,
		"misc":     "other",
	}
	sess := &wamp.Session{ID: allowedID, Details: details}
	require.True(t, pf.Allowed(sess), shouldAllowMsg)

	sess = &wamp.Session{ID: deniedID, Details: details}
	// Check that session is denied by ID.
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	sess = &wamp.Session{ID: allowedID, Details: details}
	// Check that session is denied by authid.
	sess.Details["authid"] = deniedAuthid
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	// Check that session is denied by authrole.
	sess.Details["authid"] = allowedAuthid
	sess.Details["authrole"] = deniedAuthrole
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	// Check that session is denied by not having value in whitelise.
	delete(sess.Details, "authrole")
	require.False(t, pf.Allowed(sess), shouldDenyMsg)
}

func TestFilterBlackWhitelistPrecedence(t *testing.T) {
	const (
		shouldAllowMsg = "Publish to session should be allowed"
		shouldDenyMsg  = "Publish to session should be denied"
	)

	allowedID := wamp.ID(1234)
	allowedAuthid := "root"
	allowedAuthrole := "admin"

	blacklistID := wamp.ID(9876)
	blacklistAuthid := "guest"
	blacklistAuthrole := "user"

	pub := &wamp.Publish{
		Request: wamp.GlobalID(),
		Options: wamp.Dict{
			"exclude":           wamp.List{blacklistID},
			"exclude_authid":    wamp.List{blacklistAuthid},
			"exclude_authrole":  wamp.List{blacklistAuthrole},
			"eligible":          wamp.List{allowedID, blacklistID},
			"eligible_authid":   wamp.List{allowedAuthid, blacklistAuthid},
			"eligible_authrole": wamp.List{allowedAuthrole},
		},
		Topic: wamp.URI("whitelist.test"),
	}

	pf := NewSimplePublishFilter(pub)

	details := wamp.Dict{
		"authid":   allowedAuthid,
		"authrole": allowedAuthrole,
		"misc":     "other",
	}

	sess := &wamp.Session{ID: allowedID, Details: details}
	require.True(t, pf.Allowed(sess), shouldAllowMsg)

	sess = &wamp.Session{ID: blacklistID, Details: details}
	// Check that session is denied by ID even thought ID is also in whitelist.
	require.False(t, pf.Allowed(sess), shouldDenyMsg)

	sess = &wamp.Session{ID: allowedID, Details: details}
	// Check that session is denied by authid even though also whitelisted.
	sess.Details["authid"] = blacklistAuthid
	require.False(t, pf.Allowed(sess), shouldDenyMsg)
}
