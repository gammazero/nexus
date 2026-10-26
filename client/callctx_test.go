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

// TestCallContextWhileSendBlocked verifies that Call returns when its context
// is done, even while sending the CALL is blocked because the router is not
// reading, and that the request ID it allocated is then used by the next
// request, so that request IDs stay sequential. Call used to wait to send for
// as long as the client stayed connected, whatever its context.
func TestCallContextWhileSendBlocked(t *testing.T) {
	cliPeer, rtrPeer := transport.LinkedPeers()
	resume := make(chan struct{})
	gotCall := make(chan *wamp.Call, 1)
	go func() {
		defer rtrPeer.Close()
		<-rtrPeer.Recv() // HELLO
		rtrPeer.Send() <- &wamp.Welcome{ID: 1, Details: wamp.Dict{
			"roles": wamp.Dict{"broker": wamp.Dict{}, "dealer": wamp.Dict{}},
		}}
		// Stop reading, so that the client's sends block.
		<-resume
		for msg := range rtrPeer.Recv() {
			switch m := msg.(type) {
			case *wamp.Call:
				gotCall <- m
				rtrPeer.Send() <- &wamp.Result{Request: m.Request}
			case *wamp.Goodbye:
				rtrPeer.Send() <- &wamp.Goodbye{Reason: wamp.CloseGoodbyeAndOut}
				return
			}
		}
	}()

	c, err := client.NewClient(cliPeer, client.Config{
		Realm:           testRealm,
		ResponseTimeout: time.Second,
		Logger:          logger,
	})
	require.NoError(t, err)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(ctx, "test.proc", nil, nil, nil, nil)
		done <- err
	}()
	select {
	case err = <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Call did not return when its context was done")
	}

	// The router reads again. The next CALL has the ID that the abandoned
	// CALL would have had.
	close(resume)
	_, err = c.Call(context.Background(), "test.proc", nil, nil, nil, nil)
	require.NoError(t, err)
	call := <-gotCall
	require.Equal(t, wamp.ID(1), call.Request)
}
