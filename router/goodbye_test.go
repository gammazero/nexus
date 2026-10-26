package router //nolint:testpackage

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

// requireNothingAfterGoodbye reads the messages for cli until the router
// closes it, and fails if any arrives after the router's GOODBYE.
func requireNothingAfterGoodbye(t *testing.T, cli *wamp.Session) {
	t.Helper()
	var gotGoodbye bool
	for msg := range cli.Recv() {
		if _, ok := msg.(*wamp.Goodbye); ok {
			gotGoodbye = true
			continue
		}
		require.False(t, gotGoodbye, "router sent %T after GOODBYE", msg)
	}
	require.True(t, gotGoodbye, "no GOODBYE from router")
}

// joinWithMetaSubscription joins a client to r that subscribes to
// wamp.registration.on_delete and registers a procedure, so that removing its
// registration when it leaves publishes an event it is subscribed to.
func joinWithMetaSubscription(t *testing.T, r Router) *wamp.Session {
	t.Helper()
	cli := testClient(t, r)
	cli.Send() <- &wamp.Subscribe{Request: 1, Topic: wamp.MetaEventRegOnDelete}
	recvType[*wamp.Subscribed](t, cli)
	cli.Send() <- &wamp.Register{Request: 2, Procedure: "nexus.test.leaving"}
	recvType[*wamp.Registered](t, cli)
	return cli
}

// TestNothingSentAfterClientGoodbye pins gammazero/nexus#347: once the router
// answers a client's GOODBYE, it must send that client nothing more. The
// wamp.registration.on_delete event for the client's own registration used to
// arrive after the GOODBYE, which some clients treat as a protocol error.
func TestNothingSentAfterClientGoodbye(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		for range 100 {
			cli := joinWithMetaSubscription(t, r)
			cli.Send() <- &wamp.Goodbye{Reason: wamp.CloseNormal, Details: wamp.Dict{}}
			requireNothingAfterGoodbye(t, cli)
		}
	})
}

// TestNothingSentAfterKill checks the same for a session that the router
// closes, here by wamp.session.kill.
func TestNothingSentAfterKill(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		admin := testClient(t, r)
		for i := range 100 {
			cli := joinWithMetaSubscription(t, r)
			admin.Send() <- &wamp.Call{
				Request:   wamp.ID(i + 1),
				Procedure: wamp.MetaProcSessionKill,
				Arguments: wamp.List{cli.ID},
			}
			recvType[*wamp.Result](t, admin)
			requireNothingAfterGoodbye(t, cli)
		}
	})
}
