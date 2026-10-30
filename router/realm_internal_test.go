package router

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

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

// syncBuffer is a bytes.Buffer that is safe to write from several goroutines,
// as the router's logger does.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestRealmDebug checks that RealmConfig.Debug enables debug logging for only
// its realm, and that Config.Debug enables it for every realm.
func TestRealmDebug(t *testing.T) {
	for _, routerDebug := range []bool{false, true} {
		var logs syncBuffer
		config := &Config{
			RealmConfigs: []*RealmConfig{
				{URI: testRealm, AnonymousAuth: true, Debug: true},
				{URI: testRealm2, AnonymousAuth: true},
			},
			Debug: routerDebug,
		}
		r, err := NewRouter(config, log.New(&logs, "", 0))
		require.NoError(t, err)

		for uri, want := range map[wamp.URI]bool{testRealm: true, testRealm2: routerDebug} {
			rlm := r.(*router).realms[uri]
			require.Equal(t, want, rlm.debug, "realm %s, router debug %v", uri, routerDebug)
			require.Equal(t, want, rlm.broker.debug, "broker %s, router debug %v", uri, routerDebug)
			require.Equal(t, want, rlm.dealer.debug, "dealer %s, router debug %v", uri, routerDebug)

			// The realm's debug logging shows in the router's log.
			cli := testClientInRealm(t, r, uri)
			attached := fmt.Sprintf("Finished attaching session: %d", cli.ID)
			require.Equal(t, want, strings.Contains(logs.String(), attached),
				"debug log for realm %s, router debug %v", uri, routerDebug)
			cli.Close()
		}
		// Config.Debug does not change the caller's realm configs.
		require.False(t, config.RealmConfigs[1].Debug)
		r.Close()
	}
}

// requireAbortedLast reads the messages for cli until the router closes it,
// and fails unless the last one is ABORT wamp.error.protocol_violation.
func requireAbortedLast(t *testing.T, cli *wamp.Session) {
	t.Helper()
	var last wamp.Message
	for msg := range cli.Recv() {
		last = msg
	}
	abort, ok := last.(*wamp.Abort)
	require.True(t, ok, "last message is %T, not ABORT", last)
	require.Equal(t, wamp.ErrProtocolViolation, abort.Reason)
}

// TestAbortedLocalSession checks that a local session that the broker or
// dealer aborts for a protocol violation is ended by the realm, so that it is
// removed from the broker and dealer before its peer is closed. The broker
// and dealer used to close the peer themselves while the session was still
// subscribed, so that the next event to it panicked the router with "send on
// closed channel". The ABORT must also be the last message the session gets.
func TestAbortedLocalSession(t *testing.T) {
	const proc = wamp.URI("nexus.test.abortproc")
	pptOptions := wamp.Dict{wamp.OptPPTScheme: "x_test", wamp.OptPPTSerializer: "native"}
	pptCallee := wamp.Dict{"roles": wamp.Dict{"callee": wamp.Dict{"features": wamp.Dict{
		wamp.FeaturePayloadPassthruMode: true,
	}}}}
	pptCaller := wamp.Dict{"roles": wamp.Dict{"caller": wamp.Dict{"features": wamp.Dict{
		wamp.FeaturePayloadPassthruMode: true,
	}}}}

	cases := []struct {
		name string
		// violate makes cli violate the protocol, using other clients of r if
		// it needs to.
		violate func(t *testing.T, r Router, cli *wamp.Session)
	}{
		{"publish with undeclared payload passthru", func(t *testing.T, r Router, cli *wamp.Session) {
			cli.Send() <- &wamp.Publish{Request: 2, Topic: "nexus.test.other", Options: pptOptions}
		}},
		{"undeclared progressive call", func(t *testing.T, r Router, cli *wamp.Session) {
			callee := testClient(t, r)
			callee.Send() <- &wamp.Register{Request: 1, Procedure: proc}
			recvType[*wamp.Registered](t, callee)
			cli.Send() <- &wamp.Call{Request: 2, Procedure: proc, Options: wamp.Dict{wamp.OptProgress: true}}
		}},
		{"call with undeclared payload passthru", func(t *testing.T, r Router, cli *wamp.Session) {
			callee := joinWithRoles(t, r, pptCallee)
			callee.Send() <- &wamp.Register{Request: 1, Procedure: proc}
			recvType[*wamp.Registered](t, callee)
			cli.Send() <- &wamp.Call{Request: 2, Procedure: proc, Options: pptOptions}
		}},
		{"yield with undeclared payload passthru", func(t *testing.T, r Router, cli *wamp.Session) {
			cli.Send() <- &wamp.Register{Request: 2, Procedure: proc}
			recvType[*wamp.Registered](t, cli)
			caller := joinWithRoles(t, r, pptCaller)
			caller.Send() <- &wamp.Call{Request: 1, Procedure: proc}
			inv := recvType[*wamp.Invocation](t, cli)
			cli.Send() <- &wamp.Yield{Request: inv.Request, Options: pptOptions}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newTestRouter(t)
				cli := testClient(t, r)
				cli.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
				recvType[*wamp.Subscribed](t, cli)

				tc.violate(t, r, cli)
				synctest.Wait()

				// An event for the aborted session's subscription.
				pub := testClient(t, r)
				pub.Send() <- &wamp.Publish{
					Request: 1,
					Topic:   testTopic,
					Options: wamp.Dict{wamp.OptAcknowledge: true},
				}
				recvType[*wamp.Published](t, pub)

				requireAbortedLast(t, cli)
			})
		})
	}
}
