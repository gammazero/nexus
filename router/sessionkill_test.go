package router

import (
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

func TestSessionKill(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)

	cli1 := testClient(t, r)
	cli2 := testClient(t, r)
	cli3 := testClient(t, r)

	reason := wamp.URI("foo.bar.baz")
	message := "this is a test"

	// Request that client-3 be killed.
	cli1.Send(&wamp.Call{
		Request:     wamp.GlobalID(),
		Procedure:   wamp.MetaProcSessionKill,
		Arguments:   wamp.List{cli3.ID},
		ArgumentsKw: wamp.Dict{"reason": reason, "message": message}})

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
	cli1.Send(&wamp.Call{
		Request:     wamp.GlobalID(),
		Procedure:   wamp.MetaProcSessionKill,
		Arguments:   wamp.List{cli1.ID},
		ArgumentsKw: nil})

	msg, err = wamp.RecvTimeout(cli1, time.Second)
	require.NoError(t, err)
	e, ok := msg.(*wamp.Error)
	require.True(t, ok, "Expected ERROR")
	require.Equal(t, wamp.ErrNoSuchSession, e.Error)
}

func TestSessionKillAll(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)

	cli1 := testClient(t, r)
	cli2 := testClient(t, r)
	cli3 := testClient(t, r)

	reason := wamp.URI("foo.bar.baz")
	message := "this is a test"

	cli1.Send(&wamp.Call{
		Request:     wamp.GlobalID(),
		Procedure:   wamp.MetaProcSessionKillAll,
		ArgumentsKw: wamp.Dict{"reason": reason, "message": message}})

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
}

func TestSessionKillByAuthid(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)

	cli1 := testClient(t, r)
	cli2 := testClient(t, r)
	cli3 := testClient(t, r)

	reason := wamp.URI("foo.bar.baz")
	message := "this is a test"

	// All clients have the same authid, so killing by authid should kill all
	// except the requesting client.
	cli1.Send(&wamp.Call{
		Request:     wamp.GlobalID(),
		Procedure:   wamp.MetaProcSessionKillByAuthid,
		Arguments:   wamp.List{cli1.Details["authid"]},
		ArgumentsKw: wamp.Dict{"reason": reason, "message": message}})

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
}

func TestSessionModifyDetails(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)

	caller := testClient(t, r)
	sessID := caller.ID

	// Call session meta-procedure to get session information.
	callID := wamp.GlobalID()
	delta := wamp.Dict{"xyzzy": nil, "pi": 3.14, "authid": "bob"}
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSessionModifyDetails,
		Arguments: wamp.List{caller.ID, delta},
	})
	msg, err := wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err)
	result, ok := msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")

	// Call session meta-procedure to get session information.
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSessionGet,
		Arguments: wamp.List{sessID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err)
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	details, ok := result.Arguments[0].(wamp.Dict)
	require.True(t, ok, "expected dict type arg")
	authid, _ := wamp.AsString(details["authid"])
	require.Equal(t, "bob", authid)
	_, ok = details["xyzzy"]
	require.False(t, ok, "xyzzy should have been delete from details")
	val, _ := wamp.AsFloat64(details["pi"])
	require.Equal(t, 3.14, val)
}
