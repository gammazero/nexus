package router //nolint:testpackage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// joinWithRoles joins a local client with the given HELLO details to the test
// realm of r.
func joinWithRoles(t *testing.T, r Router, details wamp.Dict) *wamp.Session {
	t.Helper()
	client, server := transport.LinkedPeers()
	go func() {
		client.Send() <- &wamp.Hello{Realm: testRealm, Details: details}
	}()
	require.NoError(t, r.Attach(server))
	msg, err := wamp.RecvTimeout(client, time.Second)
	require.NoError(t, err, "error waiting for welcome")
	welcome, ok := msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME, got %s", msg.MessageType())
	return &wamp.Session{Peer: client, ID: welcome.ID, Details: welcome.Details}
}

// drain reads and discards messages from cli until the router closes it.
func drain(cli *wamp.Session) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range cli.Recv() {
		}
	}()
	return done
}

// TestCloseDuringProgressiveCall checks that closing the router while a
// caller is sending progressive call continuations does not panic. Router
// shutdown used to close the callee's session while the caller's session was
// still routing CALLs to it, and the dealer then sent the INVOCATION on the
// closed channel.
func TestCloseDuringProgressiveCall(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	const procName = wamp.URI("nexus.test.progproc")
	calleeRoles := wamp.Dict{
		"roles": wamp.Dict{
			"callee": wamp.Dict{
				"features": wamp.Dict{
					wamp.FeatureProgCallInvocations: true,
					wamp.FeatureCallCanceling:       true,
				},
			},
		},
	}
	callerRoles := wamp.Dict{
		"roles": wamp.Dict{
			"caller": wamp.Dict{
				"features": wamp.Dict{
					wamp.FeatureProgCallInvocations: true,
				},
			},
		},
	}

	for range 100 {
		r, err := NewRouter(&Config{
			RealmConfigs: []*RealmConfig{{URI: testRealm, AnonymousAuth: true}},
		}, logger)
		require.NoError(t, err)

		callee := joinWithRoles(t, r, calleeRoles)
		caller := joinWithRoles(t, r, callerRoles)

		callee.Send() <- &wamp.Register{Request: 1, Procedure: procName}
		recvType[*wamp.Registered](t, callee)

		call := func() *wamp.Call {
			return &wamp.Call{
				Request:   1,
				Procedure: procName,
				Options:   wamp.Dict{wamp.OptProgress: true},
				Arguments: wamp.List{1},
			}
		}
		caller.Send() <- call()
		recvType[*wamp.Invocation](t, callee)

		calleeDone := drain(callee)
		callerDone := drain(caller)

		// Keep sending continuation CALLs until the router stops reading
		// them.
		stopSend := make(chan struct{})
		sendDone := make(chan struct{})
		go func() {
			defer close(sendDone)
			for {
				select {
				case caller.Send() <- call():
				case <-stopSend:
					return
				}
			}
		}()

		r.Close()
		close(stopSend)
		<-sendDone
		<-calleeDone
		<-callerDone
	}
}

// TestCloseWithSlowCaller checks that closing the router while the dealer is
// sending to a slow caller neither panics nor races. Router shutdown used to
// close a session while the dealer could still send to it, which the race
// detector reported as a data race on the session's channel, and which could
// panic with "send on closed channel".
//
// The caller has a 1-message queue that the test never reads. It makes two
// calls, and the callee answers each with an ERROR. The first ERROR fills the
// caller's queue, and the second is dropped. The router is then closed by
// t.Cleanup.
func TestCloseWithSlowCaller(t *testing.T) {
	r := newTestRouter(t)

	// Slow caller: 1-message router-to-client queue.
	callerClient, callerServer := transport.LinkedPeersQSize(1)
	go func() {
		callerClient.Send() <- &wamp.Hello{Realm: testRealm, Details: clientRoles}
	}()
	require.NoError(t, r.Attach(callerServer))
	msg, err := wamp.RecvTimeout(callerClient, time.Second)
	require.NoError(t, err)
	_, ok := msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME, got %T", msg)

	callee := testClient(t, r)
	const proc = wamp.URI("nexus.test.shutdownrace.proc")
	callee.Send() <- &wamp.Register{Request: 1, Procedure: proc}
	recvType[*wamp.Registered](t, callee)

	// Two CALL and INVOCATION ERROR exchanges. The first ERROR stays in the
	// caller's queue, and the second is dropped.
	for i := 1; i <= 2; i++ {
		callerClient.Send() <- &wamp.Call{Request: wamp.ID(i), Procedure: proc}
		inv := recvType[*wamp.Invocation](t, callee)
		callee.Send() <- &wamp.Error{
			Type:    wamp.INVOCATION,
			Request: inv.Request,
			Error:   wamp.ErrInvalidArgument,
		}
	}

	// Check that the callee is still responsive before t.Cleanup closes the
	// router, which is the shutdown under test.
	callee.Send() <- &wamp.Subscribe{Request: 100, Topic: testTopic}
	recvType[*wamp.Subscribed](t, callee)
}

// TestCloseSendsNoCalleeGone checks that closing the router while a call is in
// progress does not tell the caller that the callee is gone. The callee and
// caller are both disconnected because the router is closing, so the caller
// must only see the router's GOODBYE. Removing the callee from the dealer
// during shutdown used to send the caller ERROR wamp.error.canceled "callee
// gone", racing with its disconnect.
func TestCloseSendsNoCalleeGone(t *testing.T) {
	const procName = wamp.URI("nexus.test.closeproc")
	for range 50 {
		r, err := NewRouter(&Config{
			RealmConfigs: []*RealmConfig{{URI: testRealm, AnonymousAuth: true}},
		}, logger)
		require.NoError(t, err)

		callee := testClient(t, r)
		caller := testClient(t, r)

		callee.Send() <- &wamp.Register{Request: 1, Procedure: procName}
		recvType[*wamp.Registered](t, callee)
		caller.Send() <- &wamp.Call{Request: 1, Procedure: procName}
		recvType[*wamp.Invocation](t, callee)

		calleeDone := drain(callee)
		var callerMsgs []wamp.Message
		callerDone := make(chan struct{})
		go func() {
			defer close(callerDone)
			for msg := range caller.Recv() {
				callerMsgs = append(callerMsgs, msg)
			}
		}()

		r.Close()
		<-calleeDone
		<-callerDone
		for _, msg := range callerMsgs {
			_, isErr := msg.(*wamp.Error)
			require.False(t, isErr, "caller got %+v during router shutdown", msg)
		}
	}
}
