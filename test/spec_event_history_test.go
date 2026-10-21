package test_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/wamp"
)

// newEventHistoryRouter creates a dedicated router with event history
// configured for `topic` (exact match, given limit). The router's Close is
// registered as a t.Cleanup so it runs AFTER any client cleanups registered
// later in the test (LIFO order).
func newEventHistoryRouter(t *testing.T, topic wamp.URI, limit int) router.Router {
	t.Helper()
	cfg := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			{
				URI:           "nexus.test.history",
				StrictURI:     false,
				AnonymousAuth: true,
				TopicEventHistoryConfigs: []*router.TopicEventHistoryConfig{
					{
						Topic:       topic,
						MatchPolicy: wamp.MatchExact,
						Limit:       limit,
					},
				},
			},
		},
	}
	r, err := router.NewRouter(cfg, rtrLogger)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })
	return r
}

func historyConnect(t *testing.T, r router.Router) *client.Client {
	t.Helper()
	cli, err := client.ConnectLocal(r, client.Config{
		Realm:           "nexus.test.history",
		ResponseTimeout: clientResponseTimeout,
		Logger:          cliLogger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { cli.Close() })
	return cli
}

// TestSpecEventHistoryReturnsRecentEvents verifies that wamp.subscription.get_events
// returns events published before the subscription was made (spec §14.4.6).
func TestSpecEventHistoryReturnsRecentEvents(t *testing.T) {
	if scheme != "" {
		t.Skip("dedicated router; runs only on local in-process scheme")
	}
	checkGoLeaks(t)

	const topic = wamp.URI("spec.history.topic")
	r := newEventHistoryRouter(t, topic, 100)

	pub := historyConnect(t, r)
	for i := range 5 {
		require.NoError(t, pub.Publish(string(topic),
			wamp.Dict{wamp.OptAcknowledge: true},
			wamp.List{int64(i)}, nil))
	}

	// Subscribe AFTER publishing — proves we're reading from history.
	sub := historyConnect(t, r)
	subID := wamp.ID(0)
	require.NoError(t, sub.Subscribe(string(topic), func(_ *wamp.Event) {}, nil))
	// Pull the subscription id via the meta procedure (lookup).
	res, err := sub.Call(context.Background(), string(wamp.MetaProcSubLookup), nil,
		wamp.List{string(topic)}, nil, nil)
	require.NoError(t, err)
	subID, _ = wamp.AsID(res.Arguments[0])
	require.NotZero(t, subID, "lookup should return non-zero subscription id")

	// Fetch event history.
	res, err = sub.Call(context.Background(), string(wamp.MetaProcEventHistory),
		nil, wamp.List{subID}, nil, nil)
	require.NoError(t, err)
	require.Len(t, res.Arguments, 5, "expected 5 historical events")

	require.NoError(t, sub.Unsubscribe(string(topic)))
}

// TestSpecEventHistoryLimitOption verifies the limit kwarg restricts how
// many events are returned.
func TestSpecEventHistoryLimitOption(t *testing.T) {
	if scheme != "" {
		t.Skip("dedicated router; runs only on local in-process scheme")
	}
	checkGoLeaks(t)

	const topic = wamp.URI("spec.history.limit")
	r := newEventHistoryRouter(t, topic, 100)

	pub := historyConnect(t, r)
	for i := range 10 {
		require.NoError(t, pub.Publish(string(topic),
			wamp.Dict{wamp.OptAcknowledge: true},
			wamp.List{int64(i)}, nil))
	}

	sub := historyConnect(t, r)
	require.NoError(t, sub.Subscribe(string(topic), func(_ *wamp.Event) {}, nil))
	res, err := sub.Call(context.Background(), string(wamp.MetaProcSubLookup), nil,
		wamp.List{string(topic)}, nil, nil)
	require.NoError(t, err)
	subID, _ := wamp.AsID(res.Arguments[0])

	res, err = sub.Call(context.Background(), string(wamp.MetaProcEventHistory),
		nil, wamp.List{subID}, wamp.Dict{"limit": 3}, nil)
	require.NoError(t, err)
	require.Len(t, res.Arguments, 3, "limit=3 must cap returned events")

	require.NoError(t, sub.Unsubscribe(string(topic)))
}
