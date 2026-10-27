package router //nolint:testpackage

import (
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

func TestSessionKill(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		cli1 := testClient(t, r)
		cli2 := testClient(t, r)
		cli3 := testClient(t, r)

		reason := wamp.URI("foo.bar.baz")
		message := "this is a test"

		// Request that client-3 be killed.
		cli1.Send() <- &wamp.Call{
			Request:     wamp.GlobalID(),
			Procedure:   wamp.MetaProcSessionKill,
			Arguments:   wamp.List{cli3.ID},
			ArgumentsKw: wamp.Dict{"reason": reason, "message": message},
		}

		msg, err := wamp.RecvTimeout(cli1, time.Second)
		require.NoError(t, err)
		_, ok := msg.(*wamp.Result)
		require.True(t, ok, "Expected RESULT")

		// Check that client-3 received GOODBYE.
		msg, err = wamp.RecvTimeout(cli3, time.Second)
		require.NoError(t, err)
		g, ok := msg.(*wamp.Goodbye)
		require.True(t, ok, "expected GOODBYE")
		require.Equal(t, reason, g.Reason, "Wrong GOODBYE.Reason")
		m, _ := wamp.AsString(g.Details["message"])
		require.Equal(t, message, m, "Wrong message in GOODBYE")

		// Check that client-2 did not get anything.
		_, err = wamp.RecvTimeout(cli2, time.Millisecond)
		require.Error(t, err, "Expected timeout")

		// Test that killing self gets error.
		cli1.Send() <- &wamp.Call{
			Request:     wamp.GlobalID(),
			Procedure:   wamp.MetaProcSessionKill,
			Arguments:   wamp.List{cli1.ID},
			ArgumentsKw: nil,
		}

		msg, err = wamp.RecvTimeout(cli1, time.Second)
		require.NoError(t, err)
		e, ok := msg.(*wamp.Error)
		require.True(t, ok, "Expected ERROR")
		require.Equal(t, wamp.ErrNoSuchSession, e.Error)
	})
}

func TestSessionKillAll(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		cli1 := testClient(t, r)
		cli2 := testClient(t, r)
		cli3 := testClient(t, r)

		reason := wamp.URI("foo.bar.baz")
		message := "this is a test"

		cli1.Send() <- &wamp.Call{
			Request:     wamp.GlobalID(),
			Procedure:   wamp.MetaProcSessionKillAll,
			ArgumentsKw: wamp.Dict{"reason": reason, "message": message},
		}

		msg, err := wamp.RecvTimeout(cli1, time.Second)
		require.NoError(t, err)
		_, ok := msg.(*wamp.Result)
		require.True(t, ok, "Expected RESULT")

		msg, err = wamp.RecvTimeout(cli2, time.Second)
		require.NoError(t, err)
		g, ok := msg.(*wamp.Goodbye)
		require.True(t, ok, "expected GOODBYE")
		require.Equal(t, reason, g.Reason, "Wrong GOODBYE.Reason")
		m, _ := wamp.AsString(g.Details["message"])
		require.Equal(t, message, m, "Wrong message in GOODBYE")

		msg, err = wamp.RecvTimeout(cli3, time.Second)
		require.NoError(t, err)
		g, ok = msg.(*wamp.Goodbye)
		require.True(t, ok, "expected GOODBYE")
		require.Equal(t, reason, g.Reason, "Wrong GOODBYE.Reason")
		m, _ = wamp.AsString(g.Details["message"])
		require.Equal(t, message, m, "Wrong message in GOODBYE")

		_, err = wamp.RecvTimeout(cli1, time.Millisecond)
		require.Error(t, err, "Expected timeout")
	})
}

func TestSessionKillByAuthid(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		cli1 := testClient(t, r)
		cli2 := testClient(t, r)
		cli3 := testClient(t, r)

		reason := wamp.URI("foo.bar.baz")
		message := "this is a test"

		// All clients have the same authid, so killing by authid should kill all
		// except the requesting client.
		cli1.Send() <- &wamp.Call{
			Request:     wamp.GlobalID(),
			Procedure:   wamp.MetaProcSessionKillByAuthid,
			Arguments:   wamp.List{cli1.Details["authid"]},
			ArgumentsKw: wamp.Dict{"reason": reason, "message": message},
		}

		msg, err := wamp.RecvTimeout(cli1, time.Second)
		require.NoError(t, err)
		_, ok := msg.(*wamp.Result)
		require.True(t, ok, "Expected RESULT")

		// Check that client 2 gets kicked off.
		msg, err = wamp.RecvTimeout(cli2, time.Second)
		require.NoError(t, err)
		g, ok := msg.(*wamp.Goodbye)
		require.True(t, ok, "expected GOODBYE")
		require.Equal(t, reason, g.Reason, "Wrong GOODBYE.Reason")
		m, _ := wamp.AsString(g.Details["message"])
		require.Equal(t, message, m, "Wrong message in GOODBYE")

		// Check that client 3 gets kicked off.
		msg, err = wamp.RecvTimeout(cli3, time.Second)
		require.NoError(t, err)
		g, ok = msg.(*wamp.Goodbye)
		require.True(t, ok, "expected GOODBYE")
		require.Equal(t, reason, g.Reason, "Wrong GOODBYE.Reason")
		m, _ = wamp.AsString(g.Details["message"])
		require.Equal(t, message, m, "Wrong message in GOODBYE")

		// Check that client 1 is not kicked off.
		_, err = wamp.RecvTimeout(cli1, time.Millisecond)
		require.Error(t, err, "Expected timeout")
	})
}

func TestSessionKillByAuthrole(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		// All three clients have the authrole "anonymous", and cli1 is the
		// caller.
		cli1 := testClient(t, r)
		cli2 := testClient(t, r)
		cli3 := testClient(t, r)

		callerAuthrole, _ := wamp.AsString(cli1.Details["authrole"])
		require.NotEmpty(t, callerAuthrole, "test setup should give clients a non-empty authrole")

		reason := wamp.URI("foo.bar.baz")
		message := "kicked by authrole"

		// Kill all sessions with this authrole. The caller's own session
		// has the same authrole and must be excluded from the kill set.
		cli1.Send() <- &wamp.Call{
			Request:     wamp.GlobalID(),
			Procedure:   wamp.MetaProcSessionKillByAuthrole,
			Arguments:   wamp.List{callerAuthrole},
			ArgumentsKw: wamp.Dict{"reason": reason, "message": message},
		}

		msg, err := wamp.RecvTimeout(cli1, time.Second)
		require.NoError(t, err)
		res, ok := msg.(*wamp.Result)
		require.True(t, ok, "expected RESULT, got %T", msg)
		// RESULT carries the count of kicked sessions (2: cli2 and cli3).
		count, ok := wamp.AsInt64(res.Arguments[0])
		require.True(t, ok, "RESULT arg[0] should be an integer count")
		require.Equal(t, int64(2), count)

		// cli2 and cli3 must receive GOODBYE with the supplied reason+message.
		for i, victim := range []*wamp.Session{cli2, cli3} {
			msg, err := wamp.RecvTimeout(victim, time.Second)
			require.NoErrorf(t, err, "victim %d did not receive GOODBYE", i+2)
			g, ok := msg.(*wamp.Goodbye)
			require.Truef(t, ok, "victim %d expected GOODBYE, got %T", i+2, msg)
			require.Equal(t, reason, g.Reason)
			m, _ := wamp.AsString(g.Details["message"])
			require.Equal(t, message, m)
		}

		// cli1 (the caller) must NOT have been kicked off.
		_, err = wamp.RecvTimeout(cli1, time.Millisecond)
		require.Error(t, err, "caller should be excluded from authrole-kill")
	})
}

// TestSessionKillByAuthroleMissingArg pins the no-argument error
// path: kill_by_authrole with no Arguments must return
// wamp.error.no_such_session.
func TestSessionKillByAuthroleMissingArg(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		cli := testClient(t, r)

		cli.Send() <- &wamp.Call{
			Request:   wamp.GlobalID(),
			Procedure: wamp.MetaProcSessionKillByAuthrole,
			// Arguments intentionally empty.
		}

		msg, err := wamp.RecvTimeout(cli, time.Second)
		require.NoError(t, err)
		errMsg, ok := msg.(*wamp.Error)
		require.True(t, ok, "expected ERROR, got %T", msg)
		require.Equal(t, wamp.ErrNoSuchSession, errMsg.Error)
	})
}

// TestSessionKillByAuthroleInvalidReasonURI pins the URI-validation
// error path: a malformed reason URI is rejected with
// wamp.error.invalid_uri.
func TestSessionKillByAuthroleInvalidReasonURI(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		cli := testClient(t, r)
		callerAuthrole, _ := wamp.AsString(cli.Details["authrole"])

		cli.Send() <- &wamp.Call{
			Request:   wamp.GlobalID(),
			Procedure: wamp.MetaProcSessionKillByAuthrole,
			Arguments: wamp.List{callerAuthrole},
			ArgumentsKw: wamp.Dict{
				"reason": "not a valid uri at all",
			},
		}

		msg, err := wamp.RecvTimeout(cli, time.Second)
		require.NoError(t, err)
		errMsg, ok := msg.(*wamp.Error)
		require.True(t, ok, "expected ERROR for invalid reason URI, got %T", msg)
		require.Equal(t, wamp.ErrInvalidURI, errMsg.Error)
	})
}

func TestSessionModifyDetails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		caller := testClient(t, r)
		sessID := caller.ID

		// Call session meta-procedure to get session information.
		callID := wamp.GlobalID()
		delta := wamp.Dict{"xyzzy": nil, "pi": 3.14, "authid": "bob"}
		caller.Send() <- &wamp.Call{
			Request:   callID,
			Procedure: wamp.MetaProcSessionModifyDetails,
			Arguments: wamp.List{caller.ID, delta},
		}
		msg, err := wamp.RecvTimeout(caller, time.Second)
		require.NoError(t, err)
		result, ok := msg.(*wamp.Result)
		require.True(t, ok, "expected RESULT")
		require.Equal(t, callID, result.Request, "wrong result ID")

		// Call session meta-procedure to get session information.
		callID = wamp.GlobalID()
		caller.Send() <- &wamp.Call{
			Request:   callID,
			Procedure: wamp.MetaProcSessionGet,
			Arguments: wamp.List{sessID},
		}
		msg, err = wamp.RecvTimeout(caller, time.Second)
		require.NoError(t, err)
		result, ok = msg.(*wamp.Result)
		require.True(t, ok, "expected RESULT")
		require.Equal(t, callID, result.Request, "wrong result ID")
		require.NotZero(t, len(result.Arguments), "missing expected argument")
		details, ok := result.Arguments[0].(wamp.Dict)
		require.True(t, ok, "expected dict type arg")
		authid, _ := wamp.AsString(details["authid"])
		require.Equal(t, "bob", authid)
		_, ok = details["xyzzy"]
		require.False(t, ok, "xyzzy should have been delete from details")
		val, _ := wamp.AsFloat64(details["pi"])
		require.Equal(t, 3.14, val)
	})
}

// killer returns r as a SessionKiller, which the Router that NewRouter
// returns implements.
func killer(t *testing.T, r Router) SessionKiller {
	t.Helper()
	k, ok := r.(SessionKiller)
	require.True(t, ok, "Router does not implement SessionKiller")
	return k
}

// requireGoodbye fails unless the next message for cli is GOODBYE with the
// reason and message.
func requireGoodbye(t *testing.T, cli *wamp.Session, reason wamp.URI, message string) {
	t.Helper()
	g := recvType[*wamp.Goodbye](t, cli)
	require.Equal(t, reason, g.Reason)
	m, _ := wamp.AsString(g.Details["message"])
	require.Equal(t, message, m)
}

// TestRouterKillSessionsByAuthid checks that Router.KillSessionsByAuthid
// closes every session with the authid, and only those, from Go code.
func TestRouterKillSessionsByAuthid(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		cli1 := testClient(t, r)
		cli2 := testClient(t, r)
		authid, _ := wamp.AsString(cli1.Details["authid"])
		require.NotEmpty(t, authid)

		n, err := killer(t, r).KillSessionsByAuthid(testRealm, "nobody", "", "")
		require.NoError(t, err)
		require.Zero(t, n)

		reason := wamp.URI("nexus.test.kicked")
		n, err = killer(t, r).KillSessionsByAuthid(testRealm, authid, reason, "bye")
		require.NoError(t, err)
		require.Equal(t, 2, n)
		requireGoodbye(t, cli1, reason, "bye")
		requireGoodbye(t, cli2, reason, "bye")
	})
}

// TestRouterKillSessionsByAuthrole checks the same for
// Router.KillSessionsByAuthrole, and that an empty reason sends
// wamp.close.normal.
func TestRouterKillSessionsByAuthrole(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		cli := testClient(t, r)
		authrole, _ := wamp.AsString(cli.Details["authrole"])
		require.NotEmpty(t, authrole)

		n, err := killer(t, r).KillSessionsByAuthrole(testRealm, authrole, "", "")
		require.NoError(t, err)
		require.Equal(t, 1, n)
		requireGoodbye(t, cli, wamp.CloseNormal, "")
	})
}

// TestRouterKillSessionsErrors checks the errors from the kill methods.
func TestRouterKillSessionsErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		_, err := killer(t, r).KillSessionsByAuthid("nexus.no.such.realm", "x", "", "")
		require.ErrorContains(t, err, "no realm")

		_, err = killer(t, r).KillSessionsByAuthrole(testRealm, "x", "not a valid uri", "")
		require.ErrorContains(t, err, "invalid reason URI")
	})
}

// TestRouterKillSessionsWhileRemovingRealm checks that killing sessions while
// the realm is being removed returns, with or without an error, instead of
// sending on the closed realm's action channel.
func TestRouterKillSessionsWhileRemovingRealm(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		for i := range 200 {
			uri := wamp.URI(fmt.Sprintf("nexus.test.removing%d", i))
			require.NoError(t, r.AddRealm(&RealmConfig{URI: uri, AnonymousAuth: true}))
			go r.RemoveRealm(uri)
			_, _ = killer(t, r).KillSessionsByAuthid(uri, "x", "", "")
			synctest.Wait()
		}
	})
}
