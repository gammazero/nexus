package client_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

func testRealmConfig() *router.RealmConfig {
	return &router.RealmConfig{URI: wamp.URI(testRealm), AnonymousAuth: true}
}

// publishUntilError publishes from n goroutines until Publish fails, and
// returns a function that waits for them to stop.
func publishUntilError(c *client.Client, n int) func() {
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			for c.Publish(testTopic, nil, nil, nil) == nil {
			}
		})
	}
	return wg.Wait
}

// TestCloseWhilePublishing verifies that closing a client while other
// goroutines are publishing does not panic with a send on the closed channel
// to the router.
func TestCloseWhilePublishing(t *testing.T) {
	for range 50 {
		r := getTestRouter(t, testRealmConfig())
		c := newTestClient(t, r)
		wait := publishUntilError(c, 10)
		time.Sleep(time.Millisecond)
		require.NoError(t, c.Close())
		wait()
	}
}

// TestAbortWhilePublishing verifies that when the client aborts the session
// itself, here because a RESULT uses payload passthru mode that the router did
// not announce, it does not panic other goroutines that are sending.
func TestAbortWhilePublishing(t *testing.T) {
	for range 50 {
		cliPeer, rtrPeer := transport.LinkedPeers()

		// Fake router that announces no features, and answers a CALL with a payload
		// passthru RESULT.
		go func() {
			<-rtrPeer.Recv() // HELLO
			rtrPeer.Send() <- &wamp.Welcome{ID: 1, Details: wamp.Dict{
				"roles": wamp.Dict{"broker": wamp.Dict{}, "dealer": wamp.Dict{}},
			}}
			for msg := range rtrPeer.Recv() {
				if call, ok := msg.(*wamp.Call); ok {
					rtrPeer.Send() <- &wamp.Result{
						Request: call.Request,
						Details: wamp.Dict{wamp.OptPPTScheme: "x_test"},
					}
				}
			}
		}()

		c, err := client.NewClient(cliPeer, client.Config{
			Realm:           testRealm,
			ResponseTimeout: 500 * time.Millisecond,
			Logger:          logger,
		})
		require.NoError(t, err)

		wait := publishUntilError(c, 10)
		_, err = c.Call(context.Background(), "test.proc", nil, nil, nil, nil)
		require.ErrorIs(t, err, client.ErrPPTNotSupportedByRouter)
		wait()
		c.Close()
	}
}

// TestProgressiveCallSessionEnds verifies that the goroutine that sends the
// rest of a progressive call exits when the session ends during the call.
func TestProgressiveCallSessionEnds(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	realmConfig := testRealmConfig()
	realmConfig.EnableMetaKill = true
	r := getTestRouter(t, realmConfig)
	callee := newTestClient(t, r)
	caller := newTestClient(t, r)

	const procName = "nexus.test.progsessionends"
	err := callee.Register(procName, func(context.Context, *wamp.Invocation) client.InvokeResult {
		return client.InvokeResult{Err: wamp.InternalProgressiveOmitResult}
	}, nil)
	require.NoError(t, err)

	release := make(chan struct{})
	sends := 0
	_, err = caller.CallProgressive(context.Background(), procName,
		func(ctx context.Context) (wamp.Dict, wamp.List, wamp.Dict, error) {
			sends++
			if sends == 2 {
				// End the caller's session while it prepares the next CALL.
				go callee.Call(ctx, string(wamp.MetaProcSessionKill), nil,
					wamp.List{caller.ID()}, nil, nil)
				<-caller.Done()
				close(release)
			}
			if sends >= 2 {
				<-release
			}
			return wamp.Dict{wamp.OptProgress: true}, wamp.List{sends}, nil, nil
		}, nil)
	require.Error(t, err)

	callee.Close()
	caller.Close()
	// Close the router before the deferred leak check runs.
	r.Close()
}
