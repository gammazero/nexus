package aat

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const (
	progProc  = "nexus.test.progproc"
	chunkProc = "example.progress.text"
)

func TestRPCProgressiveCallResults(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect callee session.
	callee, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Check for feature support in router.
	const featureProgCallResults = "progressive_call_results"
	if !callee.HasFeature("dealer", featureProgCallResults) {
		t.Error("Dealer does not have", featureProgCallResults, "feature")
	}

	// Handler sends progressive results.
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		e := callee.SendProgress(ctx, wamp.List{"Alpha"}, nil)
		if e != nil {
			fmt.Println("Error sending Alpha progress:", e)
		}

		e = callee.SendProgress(ctx, wamp.List{"Bravo"}, nil)
		if e != nil {
			fmt.Println("Error sending Bravo progress:", e)
		}

		e = callee.SendProgress(ctx, wamp.List{"Charlie"}, nil)
		if e != nil {
			fmt.Println("Error sending Charlie progress:", e)
		}

		var sum int64
		for i := range args {
			n, ok := wamp.AsInt64(args[i])
			if ok {
				sum += n
			}
		}
		return &client.InvokeResult{Args: wamp.List{sum}}
	}

	// Register procedure
	if err = callee.Register(progProc, handler, nil); err != nil {
		t.Fatal("Failed to register procedure:", err)
	}

	// Connect caller session.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

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
	result, err := caller.CallProgress(ctx, progProc, nil, callArgs, nil, "", progHandler)
	if err != nil {
		t.Fatal("Failed to call procedure:", err)
	}
	sum, ok := wamp.AsInt64(result.Arguments[0])
	if !ok {
		t.Fatal("Could not convert result to int64")
	}
	if sum != 55 {
		t.Fatal("Wrong result:", sum)
	}
	if progCount != 3 {
		t.Fatal("Expected progCount == 3")
	}

	// Test unregister.
	if err = callee.Unregister(progProc); err != nil {
		t.Fatal("Failed to unregister procedure:", err)
	}

	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = callee.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

// Test that killing the caller, while in the middle of receiving progressive
// results, is handled correctly by both the closed caller and the callee.
func TestRPCProgressiveCallInterrupt(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect callee session.
	callee, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	callerKiller := make(chan struct{})
	callerClosed := make(chan struct{})
	sentFinal := make(chan struct{})

	// Handler sends progressive results.
	var sendProgErr error
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		defer close(sentFinal)
		// Send a progressive result.  This should go through just fine.
		e := callee.SendProgress(ctx, wamp.List{"Alpha"}, nil)
		if e != nil {
			fmt.Println("Error sending Alpha progress:", e)
		}

		// Give caller time to receive first message before closing.
		time.Sleep(50 * time.Millisecond)

		// Wait for caller to close so that remaining messages will fail.
		<-callerClosed

		// This first result will cause the dealer to respond with INTERRUPT.
		// An error is not returned here, since the result was sent to dealer.
		e = callee.SendProgress(ctx, wamp.List{"Bravo"}, nil)
		if e != nil {
			fmt.Println("Error sending Bravo progress:", e)
		}

		// This second result will cause the dealer to respond with INTERRUPT
		// if this client has not yet processed the INTERRUPT from the previous
		// result.  If the result was sent to the dealer, then no error.
		e = callee.SendProgress(ctx, wamp.List{"Charlie"}, nil)
		if e != nil {
			fmt.Println("Error sending progress:", e)
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
			sendProgErr = fmt.Errorf("error sending progress: %s", e)
			// Normally the callee should cancel the call, but this test makes
			// sure a callee that keeps trying to send is handled correctly.
			//return &client.InvokeResult{Err: wamp.ErrCanceled}
		}

		// This progressive result receives the same error as the previous.
		e = callee.SendProgress(ctx, wamp.List{"Echo"}, nil)
		if e != nil && e.Error() != "caller not accepting progressive results" {
			sendProgErr = fmt.Errorf("error sending progress: %s", e)
		}

		// This goes nowhere (gets put in dead buffered channel), because the
		// invocation handler has been closed and not handle the message.
		return &client.InvokeResult{Args: wamp.List{"final"}}
	}

	// Register procedure
	if err = callee.Register(progProc, handler, nil); err != nil {
		t.Fatal("Failed to register procedure:", err)
	}

	// Connect caller session.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	progHandler := func(result *wamp.Result) {
		arg := result.Arguments[0].(string)
		fmt.Println("Caller received progress response:", arg)
		select {
		case callerKiller <- struct{}{}:
		default:
		}
	}

	// Test calling the procedure.
	recvProgErr := make(chan error)
	go func() {
		ctx := context.Background()
		_, e := caller.CallProgress(ctx, progProc, nil, nil, nil, "", progHandler)
		recvProgErr <- e
	}()

	// Wait for progressive results to start being returned, then kill caller.
	<-callerKiller

	err = caller.Close()
	if err != nil {
		t.Error("Failed to disconnect client:", err)
	}

	err = <-recvProgErr
	if err != client.ErrNotConn {
		t.Errorf("unexpected error returned fom CallProgress: %s", err)
	}
	<-caller.Done()
	close(callerClosed)

	select {
	case <-sentFinal:
		t.Error("Callee should not have finished sending progressive results")
	default:
	}

	<-sentFinal

	if sendProgErr != nil {
		t.Error(sendProgErr)
	}

	err = callee.Close()
	if err != nil {
		t.Error("Failed to disconnect client:", err)
	}
}

func TestProgressStress(t *testing.T) {
	defer leaktest.Check(t)()

	// Connect callee session.
	callee, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer callee.Close()

	// Total amount of data that is sent as progressive results.
	const dataLen = 8192

	var sendCount, recvCount int

	// Define invocation handler.
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		// Get chunksize requested by caller, use default if not set.
		var chunkSize int
		if len(args) != 0 {
			i, _ := wamp.AsInt64(args[0])
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
				return nil
			}
			sendCount++
		}
		// If there is any leftover data, send it.
		if toSend != 0 {
			chunk = chunk[:toSend]
			e := callee.SendProgress(ctx, wamp.List{string(chunk)}, nil)
			if e != nil {
				// If send failed, return an error saying the call canceled.
				return nil
			}
			sendCount++
		}
		// Send total length as final result.
		return &client.InvokeResult{Args: wamp.List{dataLen}}
	}

	// Register procedure.
	if err = callee.Register(chunkProc, handler, nil); err != nil {
		t.Fatal("Failed to register procedure:", err)
	}

	// Connect caller session.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer caller.Close()

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
		// Pause on the 13th RESULT or every third call.
		if recvCount == 13 && result.Request%3 == 0 {
			fmt.Println("Caller pausing to process", recvCount, "of result",
				result.Request)
			time.Sleep(time.Millisecond * 250)
		}
	}

	// All results, for all calls, must be recieved by timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 16; i <= 256; i += 16 {
		// Call the example procedure, specifying the size of chunks to send as
		// progressive results.
		result, err := caller.CallProgress(
			ctx, chunkProc, nil, wamp.List{i}, nil, "", progHandler)
		if err != nil {
			t.Fatal(err)
		}

		// As a final result, the callee returns the total length the data.
		totalLen, _ := wamp.AsInt64(result.Arguments[0])

		if sendCount != recvCount {
			t.Error("Caller received", recvCount, "chunks, expected", sendCount)
		}
		// Check if lenth received is correct
		if recvLen != dataLen {
			t.Error("Caller received wrong amount of data")
		}
		if int(totalLen) != dataLen {
			t.Error("Length sent by callee is wrong")
		}
		sendCount = 0
		recvCount = 0
		recvLen = 0
	}
}
