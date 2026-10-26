package router //nolint:testpackage

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

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
