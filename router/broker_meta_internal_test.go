package router

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

// metaTestPeer is a peer with room for all the messages a test sends it, which
// is local or remote as set by local.
type metaTestPeer struct {
	ch    chan wamp.Message
	local bool
}

func (p *metaTestPeer) Recv() <-chan wamp.Message { return p.ch }
func (p *metaTestPeer) Send() chan<- wamp.Message { return p.ch }
func (p *metaTestPeer) Close()                    {}
func (p *metaTestPeer) IsLocal() bool             { return p.local }

// metaSubscriber is a session subscribed to a meta topic.
type metaSubscriber struct {
	sess  *wamp.Session
	subID wamp.ID
}

// subscribeMeta subscribes a new local or remote session to topic with the
// match policy, and returns it with its subscription ID.
func subscribeMeta(t *testing.T, b *broker, topic wamp.URI, match string, local bool) metaSubscriber {
	t.Helper()
	peer := &metaTestPeer{ch: make(chan wamp.Message, 16), local: local}
	sess := wamp.NewSession(peer, wamp.GlobalID(), nil, nil)
	b.subscribe(sess, &wamp.Subscribe{
		Request: 1,
		Topic:   topic,
		Options: wamp.Dict{wamp.OptMatch: match},
	})
	msg := <-sess.Recv()
	subscribed, ok := msg.(*wamp.Subscribed)
	require.True(t, ok, "expected SUBSCRIBED")
	return metaSubscriber{sess: sess, subID: subscribed.Subscription}
}

// drainEvents returns the messages that have been sent to each subscriber,
// once the broker and any backlog goroutines are idle.
func drainEvents(subs ...metaSubscriber) [][]wamp.Message {
	synctest.Wait()
	all := make([][]wamp.Message, len(subs))
	for i, s := range subs {
		for len(s.sess.Peer.(*metaTestPeer).ch) != 0 {
			all[i] = append(all[i], <-s.sess.Recv())
		}
	}
	return all
}

// runOnBroker runs f on the broker goroutine, as sync functions must be.
func runOnBroker(b *broker, f func()) {
	done := make(chan struct{})
	b.actionChan <- func() {
		f()
		close(done)
	}
	<-done
}

// requireOneEvent checks that msgs is one EVENT for sub, and returns it.
func requireOneEvent(t *testing.T, msgs []wamp.Message, sub metaSubscriber) *wamp.Event {
	t.Helper()
	require.Len(t, msgs, 1, "session %s", sub.sess)
	evt, ok := msgs[0].(*wamp.Event)
	require.True(t, ok, "expected EVENT")
	require.Equal(t, sub.subID, evt.Subscription)
	return evt
}

func TestSyncPubSubMeta(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newTestBroker(t, nil)
		metaTopic := wamp.MetaEventSubOnSubscribe

		exact := subscribeMeta(t, b, metaTopic, wamp.MatchExact, true)
		remote1 := subscribeMeta(t, b, metaTopic, wamp.MatchExact, false)
		remote2 := subscribeMeta(t, b, metaTopic, wamp.MatchExact, false)
		cause := subscribeMeta(t, b, metaTopic, wamp.MatchExact, true)
		prefix := subscribeMeta(t, b, "wamp.subscription", wamp.MatchPrefix, true)
		wildcard := subscribeMeta(t, b, "wamp..on_subscribe", wamp.MatchWildcard, true)
		other := subscribeMeta(t, b, wamp.MetaEventSubOnDelete, wamp.MatchExact, true)
		subs := []metaSubscriber{exact, remote1, remote2, cause, prefix, wildcard, other}

		// Discard the meta events from the subscriptions above.
		drainEvents(subs...)

		const subID = wamp.ID(42)
		runOnBroker(b, func() {
			b.syncPubSubMeta(metaTopic, cause.sess.ID, subID)
		})
		got := drainEvents(subs...)

		evtExact := requireOneEvent(t, got[0], exact)
		evtRemote1 := requireOneEvent(t, got[1], remote1)
		evtRemote2 := requireOneEvent(t, got[2], remote2)
		require.Empty(t, got[3], "event sent to the session that caused it")
		evtPrefix := requireOneEvent(t, got[4], prefix)
		evtWildcard := requireOneEvent(t, got[5], wildcard)
		require.Empty(t, got[6], "event sent for another meta topic")

		pubID := evtExact.Publication
		for _, evt := range []*wamp.Event{evtExact, evtRemote1, evtPrefix, evtWildcard} {
			require.Equal(t, pubID, evt.Publication, "publication IDs differ")
			require.Equal(t, wamp.List{cause.sess.ID, subID}, evt.Arguments)
		}

		// The topic is in the details only for pattern-based subscriptions.
		require.NotContains(t, evtExact.Details, detailTopic)
		require.NotContains(t, evtRemote1.Details, detailTopic)
		require.Equal(t, metaTopic, evtPrefix.Details[detailTopic])
		require.Equal(t, metaTopic, evtWildcard.Details[detailTopic])

		// Remote subscribers share one EVENT, and a local subscriber gets its own.
		require.Same(t, evtRemote1, evtRemote2, "remote subscribers not shared")
		require.NotSame(t, evtExact, evtRemote1)
	})
}

func TestSyncPubSubCreateMeta(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newTestBroker(t, nil)
		metaTopic := wamp.MetaEventSubOnCreate

		local1 := subscribeMeta(t, b, metaTopic, wamp.MatchExact, true)
		local2 := subscribeMeta(t, b, metaTopic, wamp.MatchExact, true)
		remote1 := subscribeMeta(t, b, metaTopic, wamp.MatchExact, false)
		remote2 := subscribeMeta(t, b, metaTopic, wamp.MatchExact, false)
		cause := subscribeMeta(t, b, metaTopic, wamp.MatchExact, false)
		prefix := subscribeMeta(t, b, "wamp.subscription", wamp.MatchPrefix, false)
		wildcard := subscribeMeta(t, b, "wamp..on_create", wamp.MatchWildcard, true)
		other := subscribeMeta(t, b, wamp.MetaEventSubOnSubscribe, wamp.MatchExact, true)
		subs := []metaSubscriber{local1, local2, remote1, remote2, cause, prefix, wildcard, other}

		// Discard the meta events from the subscriptions above.
		drainEvents(subs...)

		sub := &subscription{
			id:      wamp.ID(42),
			topic:   "test.topic",
			match:   wamp.MatchPrefix,
			created: "2026-10-29T12:00:00.000Z",
		}
		runOnBroker(b, func() {
			b.syncPubSubCreateMeta(cause.sess.ID, sub)
		})
		got := drainEvents(subs...)

		evtLocal1 := requireOneEvent(t, got[0], local1)
		evtLocal2 := requireOneEvent(t, got[1], local2)
		evtRemote1 := requireOneEvent(t, got[2], remote1)
		evtRemote2 := requireOneEvent(t, got[3], remote2)
		require.Empty(t, got[4], "event sent to the session that caused it")
		evtPrefix := requireOneEvent(t, got[5], prefix)
		evtWildcard := requireOneEvent(t, got[6], wildcard)
		require.Empty(t, got[7], "event sent for another meta topic")

		expectArgs := wamp.List{
			cause.sess.ID,
			wamp.Dict{
				"id":          sub.id,
				"created":     sub.created,
				"uri":         sub.topic,
				wamp.OptMatch: sub.match,
			},
		}
		pubID := evtLocal1.Publication
		for _, evt := range []*wamp.Event{evtLocal1, evtLocal2, evtRemote1, evtPrefix, evtWildcard} {
			require.Equal(t, pubID, evt.Publication, "publication IDs differ")
			require.Equal(t, expectArgs, evt.Arguments)
		}

		// The topic is in the details only for pattern-based subscriptions.
		require.NotContains(t, evtLocal1.Details, detailTopic)
		require.NotContains(t, evtRemote1.Details, detailTopic)
		require.Equal(t, metaTopic, evtPrefix.Details[detailTopic])
		require.Equal(t, metaTopic, evtWildcard.Details[detailTopic])

		// Remote subscribers share one EVENT, and each local subscriber gets its own,
		// so that a local subscriber changing its EVENT, even the dict in its
		// arguments, does not change another's.
		require.Same(t, evtRemote1, evtRemote2, "remote subscribers not shared")
		require.NotSame(t, evtLocal1, evtLocal2)
		require.NotSame(t, evtLocal1, evtRemote1)
		evtLocal1.Arguments[1].(wamp.Dict)["uri"] = "changed"
		evtLocal1.Details["changed"] = true
		require.Equal(t, expectArgs, evtLocal2.Arguments)
		require.Equal(t, expectArgs, evtRemote1.Arguments)
		require.NotContains(t, evtLocal2.Details, "changed")
	})
}
