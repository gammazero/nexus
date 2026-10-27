package router //nolint:testpackage

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

const (
	progTimeoutProc = wamp.URI("nexus.test.progtimeout")
	// progTimeout is the CALL timeout, in milliseconds, that the dealer
	// enforces in these tests.
	progTimeout = 1000
)

// joinProgressive joins a callee and a caller that use progressive call
// results and invocations, and registers progTimeoutProc. The callee does not
// support call timeouts, so the dealer enforces them.
func joinProgressive(t *testing.T, r Router) (callee, caller *wamp.Session) {
	t.Helper()
	features := wamp.Dict{
		wamp.FeatureProgCallResults:     true,
		wamp.FeatureProgCallInvocations: true,
		wamp.FeatureCallCanceling:       true,
	}
	callee = joinWithRoles(t, r, wamp.Dict{
		"roles": wamp.Dict{"callee": wamp.Dict{"features": features}},
	})
	caller = joinWithRoles(t, r, wamp.Dict{
		"roles": wamp.Dict{"caller": wamp.Dict{"features": features}},
	})
	callee.Send() <- &wamp.Register{Request: 1, Procedure: progTimeoutProc}
	recvType[*wamp.Registered](t, callee)
	return callee, caller
}

// requireNoTimeout fails if the caller has received a message other than a
// RESULT, such as the ERROR for a timed out call.
func requireNoTimeout(t *testing.T, caller *wamp.Session) {
	t.Helper()
	for {
		select {
		case msg := <-caller.Recv():
			_, ok := msg.(*wamp.Result)
			require.True(t, ok, "call ended early: %+v", msg)
		default:
			return
		}
	}
}

// TestProgressiveResultsResetTimeout checks that, for a call with progressive
// results, the timeout limits the time until the first result and between
// results, not the duration of the whole call. A stream of results that takes
// several times the timeout, with each result within the timeout of the last,
// must not be canceled.
func TestProgressiveResultsResetTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		callee, caller := joinProgressive(t, r)

		caller.Send() <- &wamp.Call{
			Request:   1,
			Procedure: progTimeoutProc,
			Options: wamp.Dict{
				wamp.OptReceiveProgress: true,
				wamp.OptTimeout:         progTimeout,
			},
		}
		inv := recvType[*wamp.Invocation](t, callee)

		for i := range 5 {
			time.Sleep(progTimeout * 3 / 4 * time.Millisecond)
			callee.Send() <- &wamp.Yield{
				Request:   inv.Request,
				Options:   wamp.Dict{wamp.OptProgress: true},
				Arguments: wamp.List{i},
			}
			synctest.Wait()
			requireNoTimeout(t, caller)
		}
		callee.Send() <- &wamp.Yield{Request: inv.Request}
		res := recvType[*wamp.Result](t, caller)
		require.Nil(t, res.Details[wamp.OptProgress], "expected final RESULT")
	})
}

// TestProgressiveResultsTimeout checks that a call with progressive results is
// still canceled with wamp.error.timeout when the time since the last result
// exceeds the timeout.
func TestProgressiveResultsTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		callee, caller := joinProgressive(t, r)

		caller.Send() <- &wamp.Call{
			Request:   1,
			Procedure: progTimeoutProc,
			Options: wamp.Dict{
				wamp.OptReceiveProgress: true,
				wamp.OptTimeout:         progTimeout,
			},
		}
		inv := recvType[*wamp.Invocation](t, callee)
		callee.Send() <- &wamp.Yield{
			Request: inv.Request,
			Options: wamp.Dict{wamp.OptProgress: true},
		}
		recvType[*wamp.Result](t, caller)

		time.Sleep(progTimeout*time.Millisecond + time.Millisecond)
		errMsg := recvType[*wamp.Error](t, caller)
		require.Equal(t, wamp.ErrTimeout, errMsg.Error)
		recvType[*wamp.Interrupt](t, callee)
	})
}

// TestProgressiveInvocationsOneTimer checks that the continuation CALLs of a
// progressive call invocation do not each start another timeout timer. The
// timeout is frozen by the initiating CALL, and only the results reset it, so
// a call whose results keep coming within the timeout must not be canceled
// however long the caller keeps sending.
func TestProgressiveInvocationsOneTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		callee, caller := joinProgressive(t, r)

		call := func(progress bool) *wamp.Call {
			return &wamp.Call{
				Request:   1,
				Procedure: progTimeoutProc,
				Options: wamp.Dict{
					wamp.OptProgress:        progress,
					wamp.OptReceiveProgress: true,
					wamp.OptTimeout:         progTimeout,
				},
			}
		}
		caller.Send() <- call(true)
		inv := recvType[*wamp.Invocation](t, callee)

		// Each chunk the caller sends is answered with a progressive result.
		for i := range 5 {
			time.Sleep(progTimeout * 3 / 4 * time.Millisecond)
			caller.Send() <- call(true)
			recvType[*wamp.Invocation](t, callee)
			callee.Send() <- &wamp.Yield{
				Request:   inv.Request,
				Options:   wamp.Dict{wamp.OptProgress: true},
				Arguments: wamp.List{i},
			}
			synctest.Wait()
			requireNoTimeout(t, caller)
		}
		caller.Send() <- call(false)
		recvType[*wamp.Invocation](t, callee)
		callee.Send() <- &wamp.Yield{Request: inv.Request}
		res := recvType[*wamp.Result](t, caller)
		require.Nil(t, res.Details[wamp.OptProgress], "expected final RESULT")

		// No timer is left to fire after the call has ended.
		time.Sleep(10 * progTimeout * time.Millisecond)
		synctest.Wait()
		select {
		case msg := <-caller.Recv():
			require.FailNowf(t, "unexpected message", "%+v after the call ended", msg)
		default:
		}
	})
}

// TestProgressiveInvocationOptionsFrozen checks that the dealer ignores the
// options of the continuation CALLs of a progressive call invocation, except
// for progress, and does not pass them on to the callee. The options of the
// initiating CALL hold for the whole call.
func TestProgressiveInvocationOptionsFrozen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		callee, caller := joinProgressive(t, r)

		caller.Send() <- &wamp.Call{
			Request:   1,
			Procedure: progTimeoutProc,
			Options:   wamp.Dict{wamp.OptProgress: true},
		}
		recvType[*wamp.Invocation](t, callee)

		// A continuation that tries to add a timeout and other options.
		caller.Send() <- &wamp.Call{
			Request:   1,
			Procedure: progTimeoutProc,
			Options: wamp.Dict{
				wamp.OptProgress:        true,
				wamp.OptTimeout:         10,
				wamp.OptReceiveProgress: true,
				wamp.OptDiscloseMe:      true,
				"x_custom":              1,
			},
		}
		inv := recvType[*wamp.Invocation](t, callee)
		require.Equal(t, wamp.Dict{wamp.OptProgress: true}, inv.Details)

		// The timeout from the continuation is ignored.
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		requireNoTimeout(t, caller)
		select {
		case msg := <-callee.Recv():
			require.FailNowf(t, "unexpected message", "callee got %+v", msg)
		default:
		}
	})
}

// TestOneFinalResultPerCall checks that the dealer sends the caller only one
// final RESULT for a call. A callee may send a final YIELD while the caller is
// still sending a progressive call, and the invocation remains until the
// caller finishes sending. Any YIELD after that final result must not reach
// the caller, which has finished the call, or a callee that keeps sending them
// grows the caller's reply backlog without limit.
func TestOneFinalResultPerCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		callee, caller, inv := finalResultMidStream(t, r)

		for i := 1; i < 3; i++ {
			callee.Send() <- &wamp.Yield{Request: inv.Request, Arguments: wamp.List{i}}
		}
		synctest.Wait()
		requireNothing(t, caller)

		// Finishing the progressive call removes the invocation once the callee
		// answers, and still sends the caller nothing more.
		caller.Send() <- &wamp.Call{
			Request:   1,
			Procedure: progTimeoutProc,
			Arguments: wamp.List{2},
		}
		inv = recvType[*wamp.Invocation](t, callee)
		callee.Send() <- &wamp.Yield{Request: inv.Request, Arguments: wamp.List{3}}
		synctest.Wait()
		requireNothing(t, caller)
		requireNoCalls(t, r)
	})
}

// TestCancelAfterFinalResult checks that a CANCEL for a call whose final
// RESULT the caller already got removes the call without sending the caller an
// ERROR or the callee an INTERRUPT. The caller finished the call, and the
// callee answered it.
func TestCancelAfterFinalResult(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		callee, caller, _ := finalResultMidStream(t, r)

		caller.Send() <- &wamp.Cancel{
			Request: 1,
			Options: wamp.Dict{wamp.OptMode: wamp.CancelModeKillNoWait},
		}
		synctest.Wait()
		requireNothing(t, caller)
		requireNothing(t, callee)
		requireNoCalls(t, r)
	})
}

// TestCalleeGoneAfterFinalResult checks that when the callee leaves after
// sending the final RESULT for a call that the caller is still sending, the
// caller gets no ERROR for the call, which it already finished.
func TestCalleeGoneAfterFinalResult(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		callee, caller, _ := finalResultMidStream(t, r)

		callee.Send() <- &wamp.Goodbye{Reason: wamp.CloseNormal, Details: wamp.Dict{}}
		recvType[*wamp.Goodbye](t, callee)
		synctest.Wait()
		requireNothing(t, caller)
		requireNoCalls(t, r)
	})
}

// finalResultMidStream starts a progressive call, has the callee send the
// final YIELD for it while the caller is still sending, and checks that the
// caller gets the final RESULT. It returns the callee, the caller, and the
// INVOCATION for the call, whose request ID is 1.
func finalResultMidStream(t *testing.T, r Router) (callee, caller *wamp.Session, inv *wamp.Invocation) {
	t.Helper()
	callee, caller = joinProgressive(t, r)
	caller.Send() <- &wamp.Call{
		Request:   1,
		Procedure: progTimeoutProc,
		Options:   wamp.Dict{wamp.OptProgress: true},
		Arguments: wamp.List{1},
	}
	inv = recvType[*wamp.Invocation](t, callee)
	callee.Send() <- &wamp.Yield{Request: inv.Request, Arguments: wamp.List{0}}
	synctest.Wait()
	res := recvType[*wamp.Result](t, caller)
	require.Equal(t, 0, res.Arguments[0])
	requireNothing(t, caller)
	return callee, caller, inv
}

// requireNoCalls fails if the dealer of the test realm of r still holds a call
// or an invocation.
func requireNoCalls(t *testing.T, r Router) {
	t.Helper()
	d := r.(*router).realms[testRealm].dealer
	count := make(chan int)
	d.actionChan <- func() {
		count <- len(d.calls) + len(d.invocations) + len(d.invocationByCall)
	}
	require.Zero(t, <-count, "finished call not removed")
}

// requireNothing fails if sess has a message to read.
func requireNothing(t *testing.T, sess *wamp.Session) {
	t.Helper()
	select {
	case msg := <-sess.Recv():
		require.FailNow(t, "unexpected message", "got %T: %+v", msg, msg)
	default:
	}
}
