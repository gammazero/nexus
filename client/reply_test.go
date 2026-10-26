package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// TestLateReplyDoesNotBlockClient verifies that a reply that arrives just
// after the client stopped waiting for it does not block the client. The
// goroutine that receives from the router used to block forever handing such
// a reply to the waiter that had already timed out, so that the client never
// read another message and Close never returned.
//
// The router answers each SUBSCRIBE close to when the client's wait times out,
// so that some replies arrive while the waiter is giving up.
func TestLateReplyDoesNotBlockClient(t *testing.T) {
	const timeout = 2 * time.Millisecond
	for i := range 100 {
		delay := timeout - 500*time.Microsecond + time.Duration(i%40)*25*time.Microsecond
		cliPeer, rtrPeer := transport.LinkedPeers()
		go func() {
			defer rtrPeer.Close()
			<-rtrPeer.Recv() // HELLO
			rtrPeer.Send() <- &wamp.Welcome{ID: 1, Details: wamp.Dict{
				"roles": wamp.Dict{"broker": wamp.Dict{}, "dealer": wamp.Dict{}},
			}}
			for msg := range rtrPeer.Recv() {
				switch m := msg.(type) {
				case *wamp.Subscribe:
					time.Sleep(delay)
					rtrPeer.Send() <- &wamp.Subscribed{Request: m.Request, Subscription: 1}
				case *wamp.Goodbye:
					rtrPeer.Send() <- &wamp.Goodbye{Reason: wamp.CloseGoodbyeAndOut}
					return
				}
			}
		}()

		c, err := client.NewClient(cliPeer, client.Config{
			Realm:           testRealm,
			ResponseTimeout: timeout,
			Logger:          logger,
		})
		require.NoError(t, err)
		_ = c.Subscribe(testTopic, func(*wamp.Event) {}, nil)
		// Let a late SUBSCRIBED arrive.
		time.Sleep(2 * timeout)

		closed := make(chan struct{})
		go func() {
			_ = c.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			require.FailNowf(t, "Close blocked", "after %d clients", i)
		}
	}
}

// earlyRouter is a fake router that sends an EVENT right after each
// SUBSCRIBED, and an INVOCATION right after each REGISTERED, as a router does
// when a publication or call is already on its way. It reports on gotErr
// whether the client answered the INVOCATION with ERROR. delay is how long it
// waits before each SUBSCRIBED or REGISTERED.
func earlyRouter(rtr wamp.Peer, delay time.Duration, gotErr chan<- bool) {
	defer rtr.Close()
	<-rtr.Recv() // HELLO
	rtr.Send() <- &wamp.Welcome{ID: 1, Details: wamp.Dict{
		"roles": wamp.Dict{"broker": wamp.Dict{}, "dealer": wamp.Dict{}},
	}}
	for msg := range rtr.Recv() {
		switch m := msg.(type) {
		case *wamp.Subscribe:
			time.Sleep(delay)
			rtr.Send() <- &wamp.Subscribed{Request: m.Request, Subscription: 7}
			rtr.Send() <- &wamp.Event{Subscription: 7, Publication: 1, Details: wamp.Dict{}}
		case *wamp.Register:
			time.Sleep(delay)
			rtr.Send() <- &wamp.Registered{Request: m.Request, Registration: 9}
			rtr.Send() <- &wamp.Invocation{Request: 1, Registration: 9, Details: wamp.Dict{}}
		case *wamp.Error:
			gotErr <- true
		case *wamp.Yield:
			gotErr <- false
		case *wamp.Goodbye:
			rtr.Send() <- &wamp.Goodbye{Reason: wamp.CloseGoodbyeAndOut}
			return
		}
	}
}

// TestHandlerInstalledBeforeNextMessage verifies that the handler for a
// subscription or registration is in place before the client handles the
// message after the SUBSCRIBED or REGISTERED, so that an EVENT or INVOCATION
// that the router sends right away is not lost. Subscribe and Register used to
// install the handler only after they got the reply, by which time the client
// had often already dropped the EVENT, or answered the INVOCATION with ERROR.
func TestHandlerInstalledBeforeNextMessage(t *testing.T) {
	for i := range 50 {
		cliPeer, rtrPeer := transport.LinkedPeers()
		gotErr := make(chan bool, 1)
		go earlyRouter(rtrPeer, 0, gotErr)
		c, err := client.NewClient(cliPeer, client.Config{
			Realm:           testRealm,
			ResponseTimeout: time.Second,
			Logger:          logger,
		})
		require.NoError(t, err)

		gotEvent := make(chan struct{}, 1)
		require.NoError(t, c.Subscribe(testTopic, func(*wamp.Event) {
			gotEvent <- struct{}{}
		}, nil))
		select {
		case <-gotEvent:
		case <-time.After(time.Second):
			require.FailNowf(t, "EVENT lost", "after %d clients", i)
		}

		require.NoError(t, c.Register("nexus.test.early", func(context.Context, *wamp.Invocation) client.InvokeResult {
			return client.InvokeResult{}
		}, nil))
		require.False(t, <-gotErr, "INVOCATION answered with ERROR after %d clients", i)
		require.NoError(t, c.Close())
	}
}

// TestNoHandlerAfterReplyTimeout verifies that when Subscribe or Register has
// already given up waiting, a SUBSCRIBED or REGISTERED that arrives late does
// not install the handler, since Subscribe or Register returned an error.
func TestNoHandlerAfterReplyTimeout(t *testing.T) {
	const timeout = 20 * time.Millisecond
	cliPeer, rtrPeer := transport.LinkedPeers()
	gotErr := make(chan bool, 1)
	go earlyRouter(rtrPeer, 3*timeout, gotErr)
	c, err := client.NewClient(cliPeer, client.Config{
		Realm:           testRealm,
		ResponseTimeout: timeout,
		Logger:          logger,
	})
	require.NoError(t, err)
	defer c.Close()

	called := make(chan struct{}, 1)
	err = c.Subscribe(testTopic, func(*wamp.Event) { called <- struct{}{} }, nil)
	require.ErrorIs(t, err, client.ErrReplyTimeout)
	select {
	case <-called:
		require.FailNow(t, "handler of a timed out Subscribe called")
	case <-time.After(10 * timeout):
	}
	_, ok := c.SubscriptionID(testTopic)
	require.False(t, ok, "timed out Subscribe left a subscription")

	err = c.Register("nexus.test.late", func(context.Context, *wamp.Invocation) client.InvokeResult {
		called <- struct{}{}
		return client.InvokeResult{}
	}, nil)
	require.ErrorIs(t, err, client.ErrReplyTimeout)
	require.True(t, <-gotErr, "INVOCATION for a timed out Register not answered with ERROR")
	select {
	case <-called:
		require.FailNow(t, "handler of a timed out Register called")
	default:
	}
}

// TestSubscribeResultMatchesHandler verifies that when a SUBSCRIBED arrives
// just as Subscribe times out, Subscribe either succeeds with its handler
// installed, or fails with no handler installed, never one without the other.
func TestSubscribeResultMatchesHandler(t *testing.T) {
	const timeout = 2 * time.Millisecond
	for i := range 300 {
		delay := timeout - 500*time.Microsecond + time.Duration(i%40)*25*time.Microsecond
		cliPeer, rtrPeer := transport.LinkedPeers()
		go earlyRouter(rtrPeer, delay, make(chan bool, 1))
		c, err := client.NewClient(cliPeer, client.Config{
			Realm:           testRealm,
			ResponseTimeout: timeout,
			Logger:          logger,
		})
		require.NoError(t, err)

		err = c.Subscribe(testTopic, func(*wamp.Event) {}, nil)
		_, ok := c.SubscriptionID(testTopic)
		if err == nil {
			require.True(t, ok, "Subscribe succeeded without a handler")
		} else {
			require.ErrorIs(t, err, client.ErrReplyTimeout)
			require.False(t, ok, "Subscribe failed but left a handler")
		}
		_ = c.Close()
	}
}
