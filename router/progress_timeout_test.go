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
