package router //nolint:testpackage

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// joinSlow joins a local client to the test realm of r, with a
// router-to-client queue that holds qsize messages.
func joinSlow(t *testing.T, r Router, qsize int) *wamp.Session {
	t.Helper()
	return joinSlowRoles(t, r, qsize, clientRoles)
}

// joinSlowRoles is like joinSlow, with details as the HELLO details.
func joinSlowRoles(t *testing.T, r Router, qsize int, details wamp.Dict) *wamp.Session {
	t.Helper()
	client, server := transport.LinkedPeersQSize(qsize)
	go func() {
		client.Send() <- &wamp.Hello{Realm: testRealm, Details: details}
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

// TestSlowSubscriberDoesNotBlockBroker checks that a subscriber whose queue
// and EVENT backlog are full does not stop the broker from delivering events
// to other subscribers. The event is dropped for the slow subscriber only.
func TestSlowSubscriberDoesNotBlockBroker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		// The slow subscriber has room for one message, and one more in its EVENT
		// backlog, and the test does not read from it until the end.
		slow := joinSlow(t, r, 1)
		slow.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
		recvType[*wamp.Subscribed](t, slow)

		fast := testClient(t, r)
		fast.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
		recvType[*wamp.Subscribed](t, fast)

		// The first event fills the slow subscriber's queue, the second its
		// backlog, and the third does not fit.
		pub := testClient(t, r)
		publishN(pub, 1, 3)
		for i := 1; i <= 3; i++ {
			event := recvType[*wamp.Event](t, fast)
			require.Equal(t, wamp.List{i}, event.Arguments)
		}

		synctest.Wait()
		requireEvents(t, slow, 1, 2)
		synctest.Wait()
		requireNoMessage(t, slow)
	})
}

// publishN publishes events with arguments first through last to testTopic.
func publishN(pub *wamp.Session, first, last int) {
	for i := first; i <= last; i++ {
		pub.Send() <- &wamp.Publish{
			Request:   wamp.ID(i),
			Topic:     testTopic,
			Arguments: wamp.List{i},
		}
	}
}

// requireEvents checks that cli receives the events with arguments first
// through last, in order.
func requireEvents(t *testing.T, cli *wamp.Session, first, last int) {
	t.Helper()
	for i := first; i <= last; i++ {
		event := recvType[*wamp.Event](t, cli)
		require.Equal(t, wamp.List{i}, event.Arguments)
	}
}

// TestSlowSubscriberEventBacklog checks that the EVENTs that do not fit in a
// subscriber's queue wait in its backlog, up to EventBacklog of them, and are
// delivered in order once the subscriber reads. With the default "drop"
// EventOverflow, the EVENTs that do not fit in the backlog either are
// dropped, and the subscriber stays connected.
func TestSlowSubscriberEventBacklog(t *testing.T) {
	const qsize = 2
	for _, tc := range []struct {
		name    string
		backlog int
		want    int
	}{
		{name: "default", backlog: 0, want: 2 * qsize},
		{name: "configured", backlog: 5, want: qsize + 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newTestRouterRealm(t, func(c *RealmConfig) {
					c.EventBacklog = tc.backlog
				})
				slow := joinSlow(t, r, qsize)
				slow.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
				recvType[*wamp.Subscribed](t, slow)

				pub := testClient(t, r)
				publishN(pub, 1, tc.want+1)
				synctest.Wait()

				requireEvents(t, slow, 1, tc.want)
				synctest.Wait()
				requireNoMessage(t, slow)

				// The subscriber caught up, so the next event is delivered.
				publishN(pub, tc.want+2, tc.want+2)
				requireEvents(t, slow, tc.want+2, tc.want+2)
			})
		})
	}
}

// TestSlowSubscriberClosed checks that, with the "close" EventOverflow, the
// router closes a subscriber that has more EVENTs than fit in its queue and
// its EVENT backlog, and that other subscribers keep receiving events.
func TestSlowSubscriberClosed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const qsize = 2
		r := newTestRouterRealm(t, func(c *RealmConfig) {
			c.EventOverflow = EventOverflowClose
		})
		slow := joinSlow(t, r, qsize)
		slow.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
		recvType[*wamp.Subscribed](t, slow)

		fast := testClient(t, r)
		fast.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
		recvType[*wamp.Subscribed](t, fast)

		pub := testClient(t, r)
		publishN(pub, 1, 2*qsize+1)
		requireEvents(t, fast, 1, 2*qsize+1)
		synctest.Wait()

		// The slow subscriber gets the events that were in its queue when it was
		// closed. The backlog is discarded, and the GOODBYE does not fit.
		requireEvents(t, slow, 1, qsize)
		_, open := <-slow.Recv()
		require.False(t, open, "slow subscriber not closed")

		publishN(pub, 2*qsize+2, 2*qsize+2)
		requireEvents(t, fast, 2*qsize+2, 2*qsize+2)
	})
}

// progressiveRoles are HELLO details for a client that subscribes, and calls
// and registers procedures with progressive results and call canceling.
var progressiveRoles = wamp.Dict{ //nolint:gochecknoglobals
	"roles": wamp.Dict{
		"subscriber": wamp.Dict{},
		"publisher":  wamp.Dict{},
		"caller": wamp.Dict{"features": wamp.Dict{
			wamp.FeatureProgCallResults: true,
			wamp.FeatureCallCanceling:   true,
		}},
		"callee": wamp.Dict{"features": wamp.Dict{
			wamp.FeatureProgCallResults: true,
			wamp.FeatureCallCanceling:   true,
		}},
	},
}

// TestEventBacklogDoesNotStarveCallee checks that a callee that is also a
// slow subscriber gets its INVOCATIONs and INTERRUPTs, in order after the
// EVENTs that wait for it, instead of the call failing for as long as any
// EVENT waits.
func TestEventBacklogDoesNotStarveCallee(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const qsize = 2
		r := newTestRouterRealm(t, func(c *RealmConfig) {
			c.EventBacklog = 4
		})
		callee := joinSlowRoles(t, r, qsize, progressiveRoles)
		callee.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
		recvType[*wamp.Subscribed](t, callee)
		const proc = wamp.URI("nexus.test.starve")
		callee.Send() <- &wamp.Register{Request: 2, Procedure: proc}
		recvType[*wamp.Registered](t, callee)

		// Fill the callee's queue, and put two EVENTs in its backlog.
		pub := testClient(t, r)
		publishN(pub, 1, qsize+2)
		synctest.Wait()

		caller := joinSlowRoles(t, r, 0, progressiveRoles)
		caller.Send() <- &wamp.Call{Request: 1, Procedure: proc}
		synctest.Wait()
		requireNoMessage(t, caller)
		caller.Send() <- &wamp.Cancel{
			Request: 1,
			Options: wamp.Dict{wamp.OptMode: wamp.CancelModeKillNoWait},
		}
		errMsg := recvType[*wamp.Error](t, caller)
		require.Equal(t, wamp.ErrCanceled, errMsg.Error)

		requireEvents(t, callee, 1, qsize+2)
		inv := recvType[*wamp.Invocation](t, callee)
		intr := recvType[*wamp.Interrupt](t, callee)
		require.Equal(t, inv.Request, intr.Request)
	})
}

// TestEventBacklogDoesNotStarveCaller checks that a caller that is also a slow
// subscriber gets its progressive RESULTs, in order after the EVENTs that wait
// for it, instead of the call being canceled because the RESULTs cannot be
// sent until no EVENT waits.
func TestEventBacklogDoesNotStarveCaller(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const qsize = 2
		r := newTestRouterRealm(t, func(c *RealmConfig) {
			c.EventBacklog = 4
		})
		callee := joinSlowRoles(t, r, 0, progressiveRoles)
		const proc = wamp.URI("nexus.test.starve")
		callee.Send() <- &wamp.Register{Request: 1, Procedure: proc}
		recvType[*wamp.Registered](t, callee)

		caller := joinSlowRoles(t, r, qsize, progressiveRoles)
		caller.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
		recvType[*wamp.Subscribed](t, caller)
		caller.Send() <- &wamp.Call{
			Request:   2,
			Procedure: proc,
			Options:   wamp.Dict{wamp.OptReceiveProgress: true},
		}
		inv := recvType[*wamp.Invocation](t, callee)

		// Fill the caller's queue, and put two EVENTs in its backlog.
		pub := testClient(t, r)
		publishN(pub, 1, qsize+2)
		synctest.Wait()

		for i := 1; i <= 2; i++ {
			callee.Send() <- &wamp.Yield{
				Request:   inv.Request,
				Options:   wamp.Dict{wamp.OptProgress: true},
				Arguments: wamp.List{i},
			}
		}
		synctest.Wait()
		requireNoMessage(t, callee)

		// The RESULTs wait in the backlog, so the dealer does not cancel the call
		// however long the caller takes to read.
		time.Sleep(2 * sendResultDeadline)
		synctest.Wait()
		requireNoMessage(t, callee)

		requireEvents(t, caller, 1, qsize+2)
		for i := 1; i <= 2; i++ {
			result := recvType[*wamp.Result](t, caller)
			require.Equal(t, wamp.List{i}, result.Arguments)
		}
		callee.Send() <- &wamp.Yield{Request: inv.Request}
		result := recvType[*wamp.Result](t, caller)
		_, progress := result.Details[wamp.OptProgress]
		require.False(t, progress, "expected final RESULT")
	})
}

// TestInvalidRealmConfig checks that the router does not start with an
// unknown EventOverflow, a negative EventBacklog or an invalid realm URI, and
// that it leaves no goroutines running when it does not start.
func TestInvalidRealmConfig(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	for _, configure := range []func(*RealmConfig){
		func(c *RealmConfig) { c.EventOverflow = "block" },
		func(c *RealmConfig) { c.EventBacklog = -1 },
		func(c *RealmConfig) { c.URI = "invalid..uri" },
	} {
		realmConfig := &RealmConfig{URI: testRealm, AnonymousAuth: true}
		configure(realmConfig)
		r, err := NewRouter(&Config{RealmConfigs: []*RealmConfig{realmConfig}}, logger)
		if err == nil {
			r.Close()
		}
		require.Error(t, err)
	}
}

// TestSlowCalleeRecoverableAfterDrain checks that a call to a callee whose
// queue and backlog are full fails with wamp.error.network_failure, and that
// this leaves the earlier calls and the registration intact, so that the
// callee works normally once it reads its queue.
func TestSlowCalleeRecoverableAfterDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		// The slow callee has room for one message, and one more in its
		// backlog.
		callee := joinSlow(t, r, 1)
		const proc = wamp.URI("nexus.test.slowcallee")
		callee.Send() <- &wamp.Register{Request: 1, Procedure: proc}
		recvType[*wamp.Registered](t, callee)

		caller := testClient(t, r)

		// The INVOCATION for the first call fills the callee's queue, and the
		// one for the second call waits in its backlog.
		for i := 1; i <= 2; i++ {
			caller.Send() <- &wamp.Call{Request: wamp.ID(i), Procedure: proc}
		}
		synctest.Wait()

		// The INVOCATION for the third call does not fit.
		caller.Send() <- &wamp.Call{Request: 3, Procedure: proc}
		errMsg := recvType[*wamp.Error](t, caller)
		require.Equal(t, wamp.ErrNetworkFailure, errMsg.Error)
		require.Equal(t, wamp.ID(3), errMsg.Request)

		// The callee reads its queue and answers the first two calls.
		for i := 1; i <= 2; i++ {
			inv := recvType[*wamp.Invocation](t, callee)
			callee.Send() <- &wamp.Yield{Request: inv.Request}
			result := recvType[*wamp.Result](t, caller)
			require.Equal(t, wamp.ID(i), result.Request)
		}
		synctest.Wait()
		requireNoMessage(t, callee)

		// A new call works normally.
		caller.Send() <- &wamp.Call{Request: 4, Procedure: proc}
		inv := recvType[*wamp.Invocation](t, callee)
		callee.Send() <- &wamp.Yield{Request: inv.Request}
		result := recvType[*wamp.Result](t, caller)
		require.Equal(t, wamp.ID(4), result.Request)
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
