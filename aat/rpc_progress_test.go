package aat_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

const (
	progProc  = "nexus.test.progproc"
	chunkProc = "example.progress.text"
)

func TestRPCProgressiveCallResults(t *testing.T) {
	checkGoLeaks(t)
	// Connect callee session.
	callee := connectClient(t)

	// Check for feature support in router.
	has := callee.HasFeature(wamp.RoleDealer, wamp.FeatureProgCallResults)
	require.Truef(t, has, "Dealer does not support %s", wamp.FeatureProgCallResults)

	// Handler sends progressive results.
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		e := callee.SendProgress(ctx, wamp.List{"Alpha"}, nil)
		if e != nil {
			t.Log("Error sending Alpha progress:", e)
		}

		e = callee.SendProgress(ctx, wamp.List{"Bravo"}, nil)
		if e != nil {
			t.Log("Error sending Bravo progress:", e)
		}

		e = callee.SendProgress(ctx, wamp.List{"Charlie"}, nil)
		if e != nil {
			t.Log("Error sending Charlie progress:", e)
		}

		var sum int64
		for _, arg := range inv.Arguments {
			n, ok := wamp.AsInt64(arg)
			if ok {
				sum += n
			}
		}
		return client.InvokeResult{Args: wamp.List{sum}}
	}

	// Register procedure
	err := callee.Register(progProc, handler, nil)
	require.NoError(t, err)

	// Connect caller session.
	caller := connectClient(t)

	progCount := 0
	progHandler := func(result *wamp.Result) {
		arg := result.Arguments[0].(string)
		if (progCount == 0 && arg != "Alpha") || (progCount == 1 && arg != "Bravo") || (progCount == 2 && arg != "Charlie") {
			return
		}
		progCount++
	}

	// Test calling the procedure.
	callArgs := wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ctx := context.Background()
	result, err := caller.Call(ctx, progProc, nil, callArgs, nil, progHandler)
	require.NoError(t, err)
	sum, ok := wamp.AsInt64(result.Arguments[0])
	require.True(t, ok, "Could not convert result to int64")
	require.Equal(t, int64(55), sum)
	require.Equal(t, 3, progCount)

	// Test unregister.
	err = callee.Unregister(progProc)
	require.NoError(t, err)
}

// Test that killing the caller, while in the middle of receiving progressive
// results, is handled correctly by both the closed caller and the callee.
func TestRPCProgressiveCallInterrupt(t *testing.T) {
	checkGoLeaks(t)
	// Connect callee session.
	callee := connectClient(t)

	callerKiller := make(chan struct{})
	callerClosed := make(chan struct{})
	sentFinal := make(chan struct{})

	// Handler sends progressive results.
	var sendProgErr error
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		defer close(sentFinal)
		// Send a progressive result.  This should go through just fine.
		e := callee.SendProgress(ctx, wamp.List{"Alpha"}, nil)
		if e != nil {
			t.Log("Error sending Alpha progress:", e)
		}

		// Give caller time to receive first message before closing.
		time.Sleep(50 * time.Millisecond)

		// Wait for caller to close so that remaining messages will fail.
		<-callerClosed

		// This first result will cause the dealer to respond with INTERRUPT.
		// An error is not returned here, since the result was sent to dealer.
		e = callee.SendProgress(ctx, wamp.List{"Bravo"}, nil)
		if e != nil {
			t.Log("Error sending Bravo progress:", e)
		}

		// This second result will cause the dealer to respond with INTERRUPT
		// if this client has not yet processed the INTERRUPT from the previous
		// result.  If the result was sent to the dealer, then no error.
		e = callee.SendProgress(ctx, wamp.List{"Charlie"}, nil)
		if e != nil {
			t.Log("Error sending progress:", e)
		}

		// Give time for this client to process INTERRUPTs.
		//
		// The client will process the first INTERRUPT and close the invocation
		// handler.  The client will get the second INTERRUPT and see that the
		// invocation no longer exists, and ignore the INTERRUPT.
		time.Sleep(50 * time.Millisecond)

		// This result will not be sent since the client has closed the
		// invocation handler.  An error is returned saying "caller not
		// accepting progressive results".
		e = callee.SendProgress(ctx, wamp.List{"Delta"}, nil)
		if e != nil && e.Error() != "caller not accepting progressive results" {
			sendProgErr = fmt.Errorf("error sending progress: %w", e)
			// Normally the callee should cancel the call, but this test makes
			// sure a callee that keeps trying to send is handled correctly.
			//return client.InvokeResult{Err: wamp.ErrCanceled}
		}

		// This progressive result receives the same error as the previous.
		e = callee.SendProgress(ctx, wamp.List{"Echo"}, nil)
		if e != nil && e.Error() != "caller not accepting progressive results" {
			sendProgErr = fmt.Errorf("error sending progress: %w", e)
		}

		// This goes nowhere (gets put in dead buffered channel), because the
		// invocation handler has been closed and not handle the message.
		return client.InvokeResult{Args: wamp.List{"final"}}
	}

	// Register procedure
	err := callee.Register(progProc, handler, nil)
	require.NoError(t, err)

	// Connect caller session.
	caller := connectClient(t)

	progHandler := func(result *wamp.Result) {
		arg := result.Arguments[0].(string)
		t.Log("Caller received progress response:", arg)
		select {
		case callerKiller <- struct{}{}:
		default:
		}
	}

	// Test calling the procedure.
	recvProgErr := make(chan error)
	go func() {
		ctx := context.Background()
		_, e := caller.Call(ctx, progProc, nil, nil, nil, progHandler)
		recvProgErr <- e
	}()

	// Wait for progressive results to start being returned, then kill caller.
	<-callerKiller

	err = caller.Close()
	require.NoError(t, err)

	err = <-recvProgErr
	require.ErrorIs(t, err, client.ErrNotConn, "unexpected error from CallProgress")
	<-caller.Done()
	close(callerClosed)

	select {
	case <-sentFinal:
		require.Fail(t, "Callee should not have finished sending progressive results")
	default:
	}

	<-sentFinal

	require.Nil(t, sendProgErr)
}

func TestProgressStress(t *testing.T) {
	checkGoLeaks(t)
	// Connect callee session.
	callee := connectClient(t)

	// Total amount of data that is sent as progressive results.
	const dataLen = 8192

	var sendCount, recvCount int

	// Define invocation handler.
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		// Get chunksize requested by caller, use default if not set.
		var chunkSize int
		if len(inv.Arguments) != 0 {
			i, _ := wamp.AsInt64(inv.Arguments[0])
			chunkSize = int(i)
		}
		if chunkSize == 0 {
			chunkSize = 64
		}

		// Make a chunk of data to send as a progressive result.
		chunk := make([]byte, chunkSize)
		for i := 0; i < chunkSize; i++ {
			chunk[i] = byte((i % 26) + int('a'))
		}

		toSend := dataLen

		// Read and send chunks of data until the buffer is empty.
		for ; toSend >= chunkSize; toSend -= chunkSize {
			// Send a chunk of data.
			e := callee.SendProgress(ctx, wamp.List{string(chunk)}, nil)
			if e != nil {
				// If send failed, return an error saying the call canceled.
				return client.InvocationCanceled
			}
			sendCount++
		}
		// If there is any leftover data, send it.
		if toSend != 0 {
			chunk = chunk[:toSend]
			e := callee.SendProgress(ctx, wamp.List{string(chunk)}, nil)
			if e != nil {
				// If send failed, return an error saying the call canceled.
				return client.InvocationCanceled
			}
			sendCount++
		}
		// Send total length as final result.
		return client.InvokeResult{Args: wamp.List{dataLen}}
	}

	// Register procedure.
	err := callee.Register(chunkProc, handler, nil)
	require.NoError(t, err)

	// Connect caller session.
	caller := connectClient(t)

	// The progress handler accumulates the chunks of data as they arrive.
	var recvLen int
	progHandler := func(result *wamp.Result) {
		// Received another chunk of data.
		chunk := result.Arguments[0].(string)
		recvLen += len(chunk)
		recvCount++

		// Simulate processing time.  This causes the callee to have to pause
		// and resend the RESULT, which happens when a callee generates results
		// faster than the caller can process them.  The pause is long enough
		// to ensure there are multiple retries.
		//
		// Pause on the 13th RESULT of every third call.
		if recvCount == 13 && result.Request%3 == 0 {
			t.Log("Caller pausing to process result", recvCount, "of request",
				result.Request)
			time.Sleep(time.Millisecond * 250)
		}
	}

	// All results, for all calls, must be received by timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 16; i <= 256; i += 16 {
		// Call the example procedure, specifying the size of chunks to send as
		// progressive results.
		result, err := caller.Call(ctx, chunkProc, nil, wamp.List{i}, nil, progHandler)
		require.NoError(t, err)

		// As a final result, the callee returns the total length the data.
		totalLen, _ := wamp.AsInt64(result.Arguments[0])

		require.Equalf(t, recvCount, sendCount, "Caller received %d chunks, expected %d", recvCount, sendCount)

		// Check if lenth received is correct
		require.Equal(t, dataLen, recvLen, "Caller received wrong amount of data")
		require.Equal(t, dataLen, int(totalLen), "Length sent by callee is wrong")

		sendCount = 0
		recvCount = 0
		recvLen = 0
	}
}

// Test that cancellng the call due to call_timeout, while in the middle of
// receiving progressive results, is handled correctly by the dealer, callee
// and the caller.
func TestRPCProgressiveCallTimeout(t *testing.T) {
	checkGoLeaks(t)
	// Connect callee session.
	callee := connectClient(t)

	gotProgRsp := make(chan struct{})
	releaseCallee := make(chan struct{})
	sendProgErr := make(chan error)

	// Handler sends progressive results.
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		// Send a progressive result.  This should go through just fine.
		e := callee.SendProgress(ctx, wamp.List{"Alpha"}, nil)
		if e != nil {
			sendProgErr <- e
			return client.InvocationCanceled
		}

		// Wait for the timeout to happen
		<-releaseCallee

		// Wait for client to process interrupt
		<-ctx.Done()

		// Sending more progressive results; one should result in error.
		e = callee.SendProgress(ctx, wamp.List{"Bravo"}, nil)
		if e != nil {
			sendProgErr <- e
			return client.InvocationCanceled
		}
		if e = callee.SendProgress(ctx, wamp.List{"Charlie"}, nil); e != nil {
			sendProgErr <- e
			return client.InvocationCanceled
		}

		sendProgErr <- nil

		// This goes nowhere (gets put in dead buffered channel), because the
		// invocation handler is closed and will not handle the message.
		return client.InvokeResult{Args: wamp.List{"final"}}
	}

	// Register procedure
	err := callee.Register(progProc, handler, nil)
	require.NoError(t, err)

	// Connect caller session.
	caller := connectClient(t)

	progHandler := func(result *wamp.Result) {
		arg := result.Arguments[0].(string)
		t.Log("Caller received progress response:", arg)
		select {
		case gotProgRsp <- struct{}{}:
		default:
		}
	}

	// Test calling the procedure.
	recvProgErr := make(chan error)
	go func() {
		ctx := context.Background()
		opts := wamp.Dict{"timeout": 1000}
		_, e := caller.Call(ctx, progProc, opts, nil, nil, progHandler)
		recvProgErr <- e
	}()

	// Wait for progressive results to start being returned
	<-gotProgRsp

	err = <-recvProgErr
	require.Error(t, err, "expected error from CallProgress")
	var rpce client.RPCError
	require.ErrorAs(t, err, &rpce, "error should be RPCError type")
	require.Equal(t, wamp.ErrTimeout, rpce.Err.Error)
	close(releaseCallee)

	select {
	case err = <-sendProgErr:
		require.ErrorIs(t, err, client.ErrCallerNoProg)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Callee should have finished sending progressive results")
	}
}
