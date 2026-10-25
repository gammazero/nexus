package client_test

import (
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
