package router //nolint:testpackage

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// joinSlow joins a local client to the test realm of r, with a
// router-to-client queue that holds qsize messages.
func joinSlow(t *testing.T, r Router, qsize int) *wamp.Session {
	t.Helper()
	client, server := transport.LinkedPeersQSize(qsize)
	go func() {
		client.Send() <- &wamp.Hello{Realm: testRealm, Details: clientRoles}
	}()
	require.NoError(t, r.Attach(server))
	msg, err := wamp.RecvTimeout(client, time.Second)
	require.NoError(t, err, "error waiting for welcome")
	welcome, ok := msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME, got %T", msg)
	return &wamp.Session{Peer: client, ID: welcome.ID, Details: welcome.Details}
}

// requireNoMessage fails if cli has a message waiting. Call it after
// synctest.Wait, so that the router has sent everything it is going to send.
func requireNoMessage(t *testing.T, cli *wamp.Session) {
	t.Helper()
	select {
	case msg := <-cli.Recv():
		require.FailNowf(t, "unexpected message", "got %T: %+v", msg, msg)
	default:
	}
}

// TestSlowSubscriberDoesNotBlockBroker checks that a subscriber whose queue is
// full does not stop the broker from delivering events to other subscribers.
// The event is dropped for the slow subscriber only.
func TestSlowSubscriberDoesNotBlockBroker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		// The slow subscriber has room for one message, and the test does not
		// read from it until the end.
		slow := joinSlow(t, r, 1)
		slow.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
		recvType[*wamp.Subscribed](t, slow)

		fast := testClient(t, r)
		fast.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
		recvType[*wamp.Subscribed](t, fast)

		// The first event fills the slow subscriber's queue, and the second
		// does not fit.
		pub := testClient(t, r)
		for i := 1; i <= 2; i++ {
			pub.Send() <- &wamp.Publish{
				Request:   wamp.ID(i),
				Topic:     testTopic,
				Arguments: wamp.List{i},
			}
		}

		for i := 1; i <= 2; i++ {
			event := recvType[*wamp.Event](t, fast)
			require.Equal(t, wamp.List{i}, event.Arguments)
		}

		synctest.Wait()
		event := recvType[*wamp.Event](t, slow)
		require.Equal(t, wamp.List{1}, event.Arguments)
		requireNoMessage(t, slow)
	})
}

// TestSlowCalleeRecoverableAfterDrain checks that a call to a callee whose
// queue is full fails with wamp.error.network_failure, and that this leaves
// the earlier call and the registration intact, so that the callee works
// normally once it reads its queue.
func TestSlowCalleeRecoverableAfterDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		// The slow callee has room for one message.
		callee := joinSlow(t, r, 1)
		const proc = wamp.URI("nexus.test.slowcallee")
		callee.Send() <- &wamp.Register{Request: 1, Procedure: proc}
		recvType[*wamp.Registered](t, callee)

		caller := testClient(t, r)

		// The INVOCATION for the first call fills the callee's queue.
		caller.Send() <- &wamp.Call{Request: 1, Procedure: proc}
		synctest.Wait()

		// The INVOCATION for the second call does not fit.
		caller.Send() <- &wamp.Call{Request: 2, Procedure: proc}
		errMsg := recvType[*wamp.Error](t, caller)
		require.Equal(t, wamp.ErrNetworkFailure, errMsg.Error)
		require.Equal(t, wamp.ID(2), errMsg.Request)

		// The callee reads its queue and answers the first call.
		inv := recvType[*wamp.Invocation](t, callee)
		synctest.Wait()
		requireNoMessage(t, callee)
		callee.Send() <- &wamp.Yield{Request: inv.Request}
		result := recvType[*wamp.Result](t, caller)
		require.Equal(t, wamp.ID(1), result.Request)

		// A new call works normally.
		caller.Send() <- &wamp.Call{Request: 3, Procedure: proc}
		inv = recvType[*wamp.Invocation](t, callee)
		callee.Send() <- &wamp.Yield{Request: inv.Request}
		result = recvType[*wamp.Result](t, caller)
		require.Equal(t, wamp.ID(3), result.Request)
	})
}

// TestCallerDisconnectMidCall checks that the dealer drops a call whose caller
// leaves before the callee answers it. The callee's YIELD for the dropped call
// is ignored, and the registration keeps working.
func TestCallerDisconnectMidCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		callee := testClient(t, r)
		const proc = wamp.URI("nexus.test.midcall")
		callee.Send() <- &wamp.Register{Request: 1, Procedure: proc}
		recvType[*wamp.Registered](t, callee)

		caller := testClient(t, r)
		caller.Send() <- &wamp.Call{Request: 1, Procedure: proc}
		inv := recvType[*wamp.Invocation](t, callee)

		// Wait for the router to remove the caller's session.
		caller.Close()
		synctest.Wait()

		callee.Send() <- &wamp.Yield{Request: inv.Request}
		synctest.Wait()
		requireNoMessage(t, callee)

		// The registration still works for another caller.
		caller = testClient(t, r)
		caller.Send() <- &wamp.Call{Request: 1, Procedure: proc}
		inv = recvType[*wamp.Invocation](t, callee)
		callee.Send() <- &wamp.Yield{Request: inv.Request}
		recvType[*wamp.Result](t, caller)
	})
}

// TestWaitReplyRoomSlowReader checks that waitReplyRoom does not give up on a
// session that keeps reading its replies, however slowly, and gives up on one
// that reads none of them for replyRoomTimeout.
func TestWaitReplyRoomSlowReader(t *testing.T) {
	// fill gives sess a backlog far past its limit, as RESULTs for calls in flight
	// can, so that making room takes many reads.
	const queueSize = 2
	fill := func() (*wamp.Session, *testPeer) {
		peer := &testPeer{in: make(chan wamp.Message, queueSize)}
		sess := wamp.NewSession(peer, 0, nil, nil)
		for i := 1; i <= 5*queueSize; i++ {
			sess.SendReply(&wamp.Result{Request: wamp.ID(i)})
		}
		require.NotNil(t, sess.ReplyRoom())
		return sess, peer
	}
	r := &realm{log: logger}

	synctest.Test(t, func(t *testing.T) {
		sess, peer := fill()
		defer sess.Close()
		done := make(chan struct{})
		defer close(done)

		// Read one reply every 30 seconds. Making room takes several minutes, much
		// longer than replyRoomTimeout.
		go func() {
			for {
				select {
				case <-time.After(replyRoomTimeout / 2):
					<-peer.Recv()
				case <-done:
					return
				}
			}
		}()
		start := time.Now()
		require.True(t, r.waitReplyRoom(sess, nil), "slow reader closed")
		require.Greater(t, time.Since(start), 2*replyRoomTimeout)
	})

	synctest.Test(t, func(t *testing.T) {
		sess, _ := fill()
		defer sess.Close()
		start := time.Now()
		require.False(t, r.waitReplyRoom(sess, nil), "stuck reader not closed")
		require.Equal(t, replyRoomTimeout, time.Since(start))
	})
}

// TestDisconnectDuringReplyWait checks that a client that disconnects while
// the router waits for room for its replies is removed from the realm right
// away, not when the wait times out. Until it is removed, its registrations
// stay, so a replacement callee could not register the same procedure.
func TestDisconnectDuringReplyWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const qsize = 2
		r := newTestRouter(t)
		callee := joinSlow(t, r, qsize)
		callee.Send() <- &wamp.Register{Request: 1, Procedure: testProcedure}
		recvType[*wamp.Registered](t, callee)

		// Without reading, fill the callee's queue and its reply backlog, and send one
		// more request, which the router holds until there is room.
		go func() {
			for i := range 2*qsize + 1 {
				callee.Send() <- &wamp.Publish{
					Request: wamp.ID(i + 2),
					Topic:   testTopic,
					Options: wamp.Dict{wamp.OptAcknowledge: true},
				}
			}
		}()
		synctest.Wait()

		// Disconnect without GOODBYE.
		callee.Close()
		synctest.Wait()

		start := time.Now()
		next := testClient(t, r)
		next.Send() <- &wamp.Register{Request: 1, Procedure: testProcedure}
		msg, err := wamp.RecvTimeout(next, time.Second)
		require.NoError(t, err)
		require.IsType(t, &wamp.Registered{}, msg, "disconnected callee still registered")
		require.Less(t, time.Since(start), replyRoomTimeout)
	})
}
