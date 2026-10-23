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
