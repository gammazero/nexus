package test_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

// errMsgContains returns true if any depth of err string contains s.
func errMsgContains(err error, s string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), s)
}

// TestSpecErrorURIMatrix is a single table-driven test that exercises every
// standard error URI defined in wamp/uris.go (interaction subset) and pins
// each trigger scenario to the URI the router currently returns. This is
// the regression net for the §1.2 dispatch-seam refactor: any subtly wrong
// URI mapping shows up here.
//
// For cases where the high-level client API maps the URI to a human-readable
// message before surfacing the error to Go callers, the test pins the
// substring so the message contract is at least locked in. Restoring
// URI-level pinning across all cases is captured as future work for the
// spec/ wire-frame harness (Track B).
//
// Per spec §13.6.2.
func TestSpecErrorURIMatrix(t *testing.T) {
	checkGoLeaks(t)

	cases := []struct {
		name      string
		errURI    wamp.URI // checked if non-empty
		errSubstr string   // alternative substring assertion when client API hides URI
		trigger   func(t *testing.T) error
	}{
		{
			name:   "invalid_uri/subscribe",
			errURI: wamp.ErrInvalidURI,
			trigger: func(t *testing.T) error {
				sub := connectClient(t)
				return sub.Subscribe("trailing.dot.", func(_ *wamp.Event) {}, nil)
			},
		},
		{
			name:   "invalid_uri/register",
			errURI: wamp.ErrInvalidURI,
			trigger: func(t *testing.T) error {
				callee := connectClient(t)
				return callee.Register("trailing.dot.",
					func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
						return client.InvokeResult{}
					}, nil)
			},
		},
		{
			name:   "no_such_procedure",
			errURI: wamp.ErrNoSuchProcedure,
			trigger: func(t *testing.T) error {
				caller := connectClient(t)
				_, err := caller.Call(context.Background(),
					"spec.errs.no.such.proc", nil, nil, nil, nil)
				return err
			},
		},
		{
			name:   "procedure_already_exists",
			errURI: wamp.ErrProcedureAlreadyExists,
			trigger: func(t *testing.T) error {
				const proc = "spec.errs.dup"
				c1 := connectClient(t)
				require.NoError(t, c1.Register(proc, func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
					return client.InvokeResult{}
				}, nil))
				c2 := connectClient(t)
				err := c2.Register(proc, func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
					return client.InvokeResult{}
				}, nil)
				_ = c1.Unregister(proc)
				return err
			},
		},
		{
			name:      "no_such_registration",
			errSubstr: "not registered",
			trigger: func(t *testing.T) error {
				callee := connectClient(t)
				return callee.Unregister("spec.errs.never.registered")
			},
		},
		{
			name:      "no_such_subscription",
			errSubstr: "not subscribed",
			trigger: func(t *testing.T) error {
				sub := connectClient(t)
				return sub.Unsubscribe("spec.errs.never.subscribed")
			},
		},
		{
			name:   "no_such_realm",
			errURI: wamp.ErrNoSuchRealm,
			trigger: func(t *testing.T) error {
				_, err := connectClientCfgErr(client.Config{
					Realm:           "spec.errs.no.such.realm",
					ResponseTimeout: clientResponseTimeout,
				})
				return err
			},
		},
		{
			name:      "authentication_failed/bad_signature",
			errSubstr: "invalid signature",
			trigger: func(t *testing.T) error {
				// Bad password for known user produces "invalid signature"
				// at the CRA verification step. Pin behavior; the WAMP URI
				// underneath is wamp.error.authentication_failed, not
				// surfaced through the Go client API.
				_, err := connectClientCfgErr(client.Config{
					Realm: testAuthRealm,
					HelloDetails: wamp.Dict{
						"authid": "malory",
					},
					AuthHandlers: map[string]client.AuthFunc{
						"wampcra": clientAuthFunc,
					},
					ResponseTimeout: time.Second,
				})
				return err
			},
		},
		{
			name:   "authorization_failed/authorizer_error",
			errURI: wamp.ErrAuthorizationFailed,
			trigger: func(t *testing.T) error {
				cfg := client.Config{
					Realm:           testAuthRealm,
					ResponseTimeout: time.Second,
				}
				caller := connectClientCfg(t, cfg)
				_, err := caller.Call(context.Background(), "need.ldap.auth",
					wamp.Dict{wamp.OptAcknowledge: true}, nil, nil, nil)
				return err
			},
		},
		{
			name:   "canceled",
			errURI: wamp.ErrCanceled,
			trigger: func(t *testing.T) error {
				// Callee returns the canceled URI directly so the caller's
				// RPCError surfaces it (context-cancellation path returns
				// context.Canceled instead, which doesn't mention the URI).
				const proc = "spec.errs.cancel"
				callee := connectClient(t)
				require.NoError(t, callee.Register(proc, func(_ context.Context, _ *wamp.Invocation) client.InvokeResult {
					return client.InvokeResult{Err: wamp.ErrCanceled}
				}, nil))
				caller := connectClient(t)
				_, err := caller.Call(context.Background(), proc, nil, nil, nil, nil)
				_ = callee.Unregister(proc)
				return err
			},
		},
		{
			name:   "timeout",
			errURI: wamp.ErrTimeout,
			trigger: func(t *testing.T) error {
				const proc = "spec.errs.timeout"
				callee := connectClient(t)
				require.NoError(t, callee.Register(proc, func(ctx context.Context, _ *wamp.Invocation) client.InvokeResult {
					<-ctx.Done()
					return client.InvokeResult{Err: wamp.ErrCanceled}
				}, nil))
				caller := connectClient(t)
				_, err := caller.Call(context.Background(), proc,
					wamp.Dict{wamp.OptTimeout: 200}, nil, nil, nil)
				_ = callee.Unregister(proc)
				return err
			},
		},
		{
			name:   "option_disallowed.disclose_me",
			errURI: wamp.ErrOptionDisallowedDiscloseMe,
			trigger: func(t *testing.T) error {
				// Default test realm has AllowDisclose=false.
				pub := connectClient(t)
				return pub.Publish(testTopic,
					wamp.Dict{wamp.OptAcknowledge: true, wamp.OptDiscloseMe: true},
					wamp.List{"x"}, nil)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.trigger(t)
			require.Errorf(t, err, "case %s: expected error", tc.name)
			switch {
			case tc.errURI != "":
				require.Truef(t, errMsgContains(err, string(tc.errURI)),
					"case %s: error %q must mention URI %q",
					tc.name, err.Error(), tc.errURI)
			case tc.errSubstr != "":
				require.Truef(t, errMsgContains(err, tc.errSubstr),
					"case %s: error %q must contain substring %q",
					tc.name, err.Error(), tc.errSubstr)
			default:
				t.Fatalf("case %s: must specify either errURI or errSubstr", tc.name)
			}
		})
	}
}
