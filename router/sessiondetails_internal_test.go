package router

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

// testSessionDetails returns session details with standard and non-standard
// entries, and a transport detail that has auth.
func testSessionDetails() wamp.Dict {
	return wamp.Dict{
		"session":      wamp.ID(123),
		"authid":       "user1",
		"authrole":     "user",
		"authmethod":   "ticket",
		"authprovider": "static",
		"transport": wamp.Dict{
			"type": "websocket",
			"auth": wamp.Dict{"cookie": "secret"},
		},
		"xyzzy": "plugh",
		"foo":   "bar",
	}
}

func TestCleanSessionDetails(t *testing.T) {
	r := &realm{}
	details := testSessionDetails()

	// All details are kept, except transport.auth.
	clean := r.cleanSessionDetails(details)
	expect := testSessionDetails()
	expect["transport"] = wamp.Dict{"type": "websocket"}
	require.Equal(t, expect, clean)

	// The session details are not changed.
	require.Equal(t, testSessionDetails(), details)

	// The result is a copy, so changing it or the details does not change the
	// other.
	clean["added"] = true
	delete(clean, "xyzzy")
	clean["transport"].(wamp.Dict)["added"] = true
	require.Equal(t, testSessionDetails(), details)
	details["later"] = true
	details["transport"].(wamp.Dict)["later"] = true
	require.NotContains(t, clean, "later")
	require.NotContains(t, clean["transport"], "later")
}

// TestCleanSessionDetailsDeepCopy checks that dicts and lists in the details
// are copied too, so that a later change to the session's details does not
// change the result.
func TestCleanSessionDetailsDeepCopy(t *testing.T) {
	r := &realm{}
	authextra := wamp.Dict{"pubkey": "abc"}
	roles := wamp.List{"caller", wamp.Dict{"callee": true}}
	transport := wamp.Dict{"type": "rawsocket", "info": wamp.Dict{"x": 1}}
	details := wamp.Dict{
		"authextra": authextra,
		"roles":     roles,
		"transport": transport,
	}

	clean := r.cleanSessionDetails(details)
	require.Equal(t, details, clean)

	authextra["pubkey"] = "changed"
	roles[1].(wamp.Dict)["callee"] = false
	transport["type"] = "websocket"
	transport["info"].(wamp.Dict)["x"] = 2
	require.Equal(t, wamp.Dict{
		"authextra": wamp.Dict{"pubkey": "abc"},
		"roles":     wamp.List{"caller", wamp.Dict{"callee": true}},
		"transport": wamp.Dict{"type": "rawsocket", "info": wamp.Dict{"x": 1}},
	}, clean)
}

func TestCleanSessionDetailsStrict(t *testing.T) {
	r := &realm{
		metaStrict:     true,
		metaIncDetails: []string{"xyzzy", "missing"},
	}
	details := testSessionDetails()

	// Only the standard details and the included details are kept, and
	// transport.auth is removed. Included details that the session does not have
	// are not added.
	clean := r.cleanSessionDetails(details)
	require.Equal(t, wamp.Dict{
		"session":      wamp.ID(123),
		"authid":       "user1",
		"authrole":     "user",
		"authmethod":   "ticket",
		"authprovider": "static",
		"transport":    wamp.Dict{"type": "websocket"},
		"xyzzy":        "plugh",
	}, clean)
	require.Equal(t, testSessionDetails(), details)

	// Standard details that the session does not have are not added.
	clean = r.cleanSessionDetails(wamp.Dict{"authid": "user1", "foo": "bar"})
	require.Equal(t, wamp.Dict{"authid": "user1"}, clean)
}

func TestCleanSessionDetailsTransport(t *testing.T) {
	r := &realm{}

	// No transport detail.
	clean := r.cleanSessionDetails(wamp.Dict{"authid": "user1"})
	require.Equal(t, wamp.Dict{"authid": "user1"}, clean)

	// A transport detail without auth is kept.
	clean = r.cleanSessionDetails(wamp.Dict{
		"transport": wamp.Dict{"type": "rawsocket"},
	})
	require.Equal(t, wamp.Dict{"transport": wamp.Dict{"type": "rawsocket"}}, clean)

	// An auth that is not a dict is removed too.
	clean = r.cleanSessionDetails(wamp.Dict{
		"transport": wamp.Dict{"type": "rawsocket", "auth": "secret"},
	})
	require.Equal(t, wamp.Dict{"transport": wamp.Dict{"type": "rawsocket"}}, clean)

	// A transport detail of another map type has auth removed.
	details := wamp.Dict{
		"transport": map[string]any{
			"type": "websocket",
			"auth": map[string]any{"cookie": "secret"},
		},
	}
	clean = r.cleanSessionDetails(details)
	require.Equal(t, wamp.Dict{"type": "websocket"}, clean["transport"])
	require.Contains(t, details["transport"], "auth")

	// A transport detail that has only auth is left out.
	clean = r.cleanSessionDetails(wamp.Dict{
		"authid":    "user1",
		"transport": wamp.Dict{"auth": wamp.Dict{"cookie": "secret"}},
	})
	require.Equal(t, wamp.Dict{"authid": "user1"}, clean)

	// The same in strict mode.
	strict := &realm{metaStrict: true}
	clean = strict.cleanSessionDetails(wamp.Dict{
		"authid":    "user1",
		"transport": wamp.Dict{"auth": wamp.Dict{"cookie": "secret"}},
	})
	require.Equal(t, wamp.Dict{"authid": "user1"}, clean)
}
