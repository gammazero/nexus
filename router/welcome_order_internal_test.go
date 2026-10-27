package router

import (
	"maps"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// slowFirstSendPeer delays only the first Send() to it by wait; every later
// Send() goes straight through. That opens a window in which whatever the
// router queues next overtakes the first message.
type slowFirstSendPeer struct {
	wamp.Peer
	started atomic.Bool
	wait    time.Duration
}

func (p *slowFirstSendPeer) Send() chan<- wamp.Message {
	if p.started.CompareAndSwap(false, true) {
		time.Sleep(p.wait)
	}
	return p.Peer.Send()
}

// The router must queue a session's WELCOME before the session becomes
// visible to the rest of the realm. Otherwise a session killed as soon as
// wamp.session.on_join announces it can receive the kill's GOODBYE before its
// WELCOME, and the client fails the handshake ("received unexpected GOODBYE
// message when expecting WELCOME"). The first Send() to the joining session
// is slowed down while the test kills it through the meta API: without the
// fix that first send is the WELCOME, and the kill's GOODBYE overtakes it.
// With the fix the WELCOME is queued before on_join is even published.
func TestWelcomeQueuedBeforeSessionIsVisible(t *testing.T) {
	r := newTestRouter(t)
	killer := testClient(t, r)

	killer.Send() <- &wamp.Subscribe{Request: 1, Options: wamp.Dict{}, Topic: wamp.MetaEventSessionOnJoin}
	msg, err := wamp.RecvTimeout(killer, time.Second)
	require.NoError(t, err)
	require.IsType(t, &wamp.Subscribed{}, msg)

	client, server := transport.LinkedPeers()
	t.Cleanup(func() { client.Close() })
	slow := &slowFirstSendPeer{Peer: server, wait: time.Second}

	details := maps.Clone(clientRoles)
	details["authid"] = "victim"
	go func() {
		client.Send() <- &wamp.Hello{Realm: testRealm, Details: details}
	}()
	attachErr := make(chan error, 1)
	go func() { attachErr <- r.Attach(slow) }()

	// Kill the session the moment on_join announces it. The killer may see
	// its own on_join first: it is published after the killer's WELCOME.
	var joined wamp.Dict
	for joined == nil {
		msg, err = wamp.RecvTimeout(killer, 5*time.Second)
		require.NoError(t, err)
		ev, ok := msg.(*wamp.Event)
		require.Truef(t, ok, "expected on_join EVENT, got %s", msg.MessageType())
		if d := ev.Arguments[0].(wamp.Dict); d["authid"] == "victim" {
			joined = d
		}
	}
	killer.Send() <- &wamp.Call{
		Request:   2,
		Procedure: wamp.MetaProcSessionKill,
		Arguments: wamp.List{joined["session"]},
		Options:   wamp.Dict{},
	}
	msg, err = wamp.RecvTimeout(killer, time.Second)
	require.NoError(t, err)
	require.IsTypef(t, &wamp.Result{}, msg, "kill failed: %+v", msg)
	first, err := wamp.RecvTimeout(client, 5*time.Second)
	require.NoError(t, err)
	require.Equalf(t, wamp.WELCOME, first.MessageType(), "first message to the joining session: %+v", first)
	next, err := wamp.RecvTimeout(client, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, wamp.GOODBYE, next.MessageType())
	require.NoError(t, <-attachErr)
}

// A client must not get its WELCOME before the session is visible to the
// realm. Otherwise a client that another client acts on as soon as it has
// joined, as by wamp.session.kill_all, can be missed. The realm goroutine is
// held while the client joins, so that the session cannot become visible, and
// the client must not get its WELCOME until the realm goroutine is released.
func TestWelcomeOnlyOnceSessionIsVisible(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		rlm := r.(*router).realms[testRealm]

		release := make(chan struct{})
		rlm.actionChan <- func() { <-release }

		client, server := transport.LinkedPeers()
		t.Cleanup(func() { client.Close() })
		go func() {
			client.Send() <- &wamp.Hello{Realm: testRealm, Details: clientRoles}
		}()
		attachErr := make(chan error, 1)
		go func() { attachErr <- r.Attach(server) }()

		synctest.Wait()
		select {
		case msg := <-client.Recv():
			close(release)
			require.FailNowf(t, "client got a message before it was visible",
				"%s", msg.MessageType())
		default:
		}
		close(release)

		msg, err := wamp.RecvTimeout(client, time.Second)
		require.NoError(t, err)
		require.IsType(t, &wamp.Welcome{}, msg)

		// Once the client has its WELCOME, kill_all closes it.
		require.Equal(t, 1, rlm.killAllSessions("", "", 0))
		msg, err = wamp.RecvTimeout(client, time.Second)
		require.NoError(t, err)
		require.IsType(t, &wamp.Goodbye{}, msg)
		require.NoError(t, <-attachErr)
	})
}
