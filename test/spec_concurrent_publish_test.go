package test_test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// newRaceRouter starts a dedicated router with a single anonymous-auth realm
// and a TCP rawsocket server on a free local port. It returns the address.
// Real network transport is required for this test because the race we're
// triggering only manifests when subscribers' transport goroutines are
// concurrently serializing the shared Details map that the broker is still
// mutating for later subscribers in the fan-out loop.
func newRaceRouter(t *testing.T) string {
	t.Helper()
	cfg := &router.Config{
		RealmConfigs: []*router.RealmConfig{{
			URI:           "nexus.race.realm",
			StrictURI:     false,
			AnonymousAuth: true,
		}},
	}
	r, err := router.NewRouter(cfg, rtrLogger)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })

	srv := router.NewRawSocketServer(r)
	closer, err := srv.ListenAndServe("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	ln, ok := closer.(net.Listener)
	require.True(t, ok)
	return ln.Addr().String()
}

func raceConnect(t *testing.T, addr string) *client.Client {
	t.Helper()
	cli, err := client.ConnectNet(context.Background(), "tcp://"+addr+"/", client.Config{
		Realm:           "nexus.race.realm",
		Serialization:   serialize.JSON,
		ResponseTimeout: 5 * time.Second,
		Logger:          cliLogger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { cli.Close() })
	return cli
}

// TestSpecBrokerConcurrentPublishMapRace pins the race described in
// gammazero/nexus#343.
//
// Failure mode (without the fix in #344):
//
//	broker.publish() creates a single `details := wamp.Dict{}` per publish
//	call. That same map is aliased into every outbound *wamp.Event for
//	every matching subscriber. When the subscription used pattern-based
//	matching, prepareEvent additionally writes msg.Topic into details. So:
//
//	  1. prepareEvent(sub A) writes details["topic"] = topic, sends event A
//	  2. sub A's transport goroutine begins JSON-encoding event A and
//	     iterates details
//	  3. prepareEvent(sub B) writes details["topic"] = topic again — to the
//	     same shared map
//	  4. Go runtime detects "concurrent map iteration and map write" and
//	     panics, killing the process.
//
// We force the race window by:
//   - Pattern-matched (prefix) subscriptions, which set sendTopic=true and
//     thus trigger the offending write in the broker
//   - Many subscribers, so each publish fans out to a long for-loop with
//     many transport-goroutine handoffs in flight at once
//   - Real TCP rawsocket transport with JSON serialization, so transport
//     goroutines actually iterate the shared map (in-process LocalPeer
//     does not exercise the serializer)
//   - High publish throughput from multiple publishers
//
// The Go runtime's concurrent-map-write detection is always on (no -race
// flag needed); it surfaces as a fatal panic that aborts the test process.
// `go test -race` will additionally flag the underlying data race even when
// the runtime check happens not to fire.
func TestSpecBrokerConcurrentPublishMapRace(t *testing.T) {
	addr := newRaceRouter(t)

	const numSubs = 32
	const numPublishers = 4
	const publishesPerWorker = 250

	// Connect subscribers with prefix-match so the broker's prepareEvent
	// takes the sendTopic=true path on every fan-out.
	subOpts := wamp.SetOption(nil, wamp.OptMatch, wamp.MatchPrefix)
	var eventsReceived atomic.Int64
	for i := 0; i < numSubs; i++ {
		sub := raceConnect(t, addr)
		require.NoError(t, sub.Subscribe("nexus.race", func(_ *wamp.Event) {
			eventsReceived.Add(1)
		}, subOpts))
	}

	// One publisher per worker; sharing a single client across goroutines
	// would funnel all publishes through one client write loop and reduce
	// the race window. Different clients give us independently-paced
	// publish streams.
	publishers := make([]*client.Client, numPublishers)
	for i := range publishers {
		publishers[i] = raceConnect(t, addr)
	}

	// Non-empty Arguments / ArgumentsKw — these get aliased into every
	// outbound event too. Larger payload = more time spent iterating
	// during JSON encode = wider race window.
	args := wamp.List{"alpha", "bravo", "charlie", "delta", "echo"}
	kwargs := wamp.Dict{
		"k1": "v1", "k2": "v2", "k3": "v3", "k4": "v4", "k5": "v5",
		"k6": "v6", "k7": "v7", "k8": "v8", "k9": "v9", "k10": "v10",
	}

	var wg sync.WaitGroup
	for p := 0; p < numPublishers; p++ {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < publishesPerWorker; i++ {
				topic := fmt.Sprintf("nexus.race.event.%d.%d", p, i)
				_ = publishers[p].Publish(topic,
					wamp.Dict{wamp.OptAcknowledge: true},
					args, kwargs)
			}
		}()
	}
	wg.Wait()

	// Drain — give subscribers time to receive remaining events. We don't
	// strictly require all events to land (publish quotas, queue overflow),
	// only that no concurrent-map panic fired.
	deadline := time.Now().Add(2 * time.Second)
	target := int64(numPublishers * publishesPerWorker * numSubs)
	for time.Now().Before(deadline) && eventsReceived.Load() < target {
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("delivered %d / %d events to %d subscribers across %d publishers",
		eventsReceived.Load(), target, numSubs, numPublishers)
}
