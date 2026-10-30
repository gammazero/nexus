package router //nolint:testpackage

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

func TestSessionTestaments(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		sub := testClient(t, r)
		subscribeID := wamp.GlobalID()
		sub.Send() <- &wamp.Subscribe{Request: subscribeID, Topic: "testament.test1"}
		msg, err := wamp.RecvTimeout(sub, time.Second)
		require.NoError(t, err)
		_, ok := msg.(*wamp.Subscribed)
		require.True(t, ok, "expected RESULT")

		sub.Send() <- &wamp.Subscribe{Request: subscribeID, Topic: "testament.test2"}
		msg, err = wamp.RecvTimeout(sub, time.Second)
		require.NoError(t, err)
		require.NotNil(t, msg)

		caller1 := testClient(t, r)
		caller2 := testClient(t, r)

		callID := wamp.GlobalID()
		caller1.Send() <- &wamp.Call{
			Request:   callID,
			Procedure: wamp.MetaProcSessionAddTestament,
			Arguments: wamp.List{
				"testament.test1",
				wamp.List{"foo"},
				wamp.Dict{},
			},
		}

		msg, err = wamp.RecvTimeout(caller1, time.Second)
		require.NoError(t, err)
		result, ok := msg.(*wamp.Result)
		require.True(t, ok, "expected RESULT")
		require.Equal(t, callID, result.Request, "wrong result ID")

		caller2.Send() <- &wamp.Call{
			Request:   wamp.GlobalID(),
			Procedure: wamp.MetaProcSessionAddTestament,
			Arguments: wamp.List{
				"testament.test2",
				wamp.List{"foo"},
				wamp.Dict{},
			},
		}

		msg, err = wamp.RecvTimeout(caller2, time.Second)
		require.NoError(t, err)
		_, ok = msg.(*wamp.Result)
		require.True(t, ok, "expected RESULT")

		caller1.Close()
		caller2.Close()

		msg, err = wamp.RecvTimeout(sub, 5*time.Second)
		require.NoError(t, err)
		event, ok := msg.(*wamp.Event)
		require.True(t, ok, "expected EVENT")
		val, _ := wamp.AsString(event.Arguments[0])
		require.Equal(t, "foo", val, "Argument value was invalid")

		msg, err = wamp.RecvTimeout(sub, time.Second)
		require.NoError(t, err)
		event, ok = msg.(*wamp.Event)
		require.True(t, ok, "expected EVENT")
		val, _ = wamp.AsString(event.Arguments[0])
		require.Equal(t, "foo", val, "Argument value was invalid")
	})
}

// callMeta calls the meta procedure proc from cli and returns the reply.
func callMeta(t *testing.T, cli *wamp.Session, proc wamp.URI, args wamp.List, kwargs wamp.Dict) wamp.Message {
	t.Helper()
	req := wamp.GlobalID()
	cli.Send() <- &wamp.Call{
		Request:     req,
		Procedure:   proc,
		Arguments:   args,
		ArgumentsKw: kwargs,
	}
	msg, err := wamp.RecvTimeout(cli, time.Second)
	require.NoError(t, err)
	switch msg := msg.(type) {
	case *wamp.Result:
		require.Equal(t, req, msg.Request, "wrong result ID")
	case *wamp.Error:
		require.Equal(t, req, msg.Request, "wrong error ID")
	default:
		require.FailNowf(t, "expected RESULT or ERROR", "got %s",
			msg.MessageType())
	}
	return msg
}

// addTestament adds a testament for cli that publishes arg to topic.
func addTestament(t *testing.T, cli *wamp.Session, topic wamp.URI, arg, scope string) {
	t.Helper()
	msg := callMeta(t, cli, wamp.MetaProcSessionAddTestament,
		wamp.List{topic, wamp.List{arg}, wamp.Dict{}},
		wamp.Dict{"scope": scope})
	require.IsType(t, &wamp.Result{}, msg)
}

// flushTestaments flushes the testaments for cli, with kwargs as the keyword
// arguments of the call, and returns the reply.
func flushTestaments(t *testing.T, cli *wamp.Session, kwargs wamp.Dict) wamp.Message {
	t.Helper()
	return callMeta(t, cli, wamp.MetaProcSessionFlushTestaments, nil, kwargs)
}

// testamentArgs returns the first argument of each of the session's testaments
// in the bucket for each scope, and whether the realm has a bucket for the
// session.
func testamentArgs(rlm *realm, id wamp.ID) (detached, destroyed []any, ok bool) {
	done := make(chan struct{})
	rlm.actionChan <- func() {
		var bucket testamentBucket
		bucket, ok = rlm.testaments[id]
		for _, tm := range bucket.detached {
			detached = append(detached, tm.args[0])
		}
		for _, tm := range bucket.destroyed {
			destroyed = append(destroyed, tm.args[0])
		}
		close(done)
	}
	<-done
	return detached, destroyed, ok
}

func TestTestamentFlush(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		rlm := r.(*router).realms[testRealm]

		cli := testClient(t, r)
		other := testClient(t, r)
		addTestament(t, other, "testament.other", "other", destroyedScope)

		addAll := func() {
			addTestament(t, cli, "testament.test", "detached", detachedScope)
			addTestament(t, cli, "testament.test", "destroyed1", destroyedScope)
			addTestament(t, cli, "testament.test", "destroyed2", destroyedScope)
		}
		addAll()

		// With no scope, the destroyed testaments are flushed.
		msg := flushTestaments(t, cli, nil)
		require.IsType(t, &wamp.Result{}, msg)
		detached, destroyed, ok := testamentArgs(rlm, cli.ID)
		require.True(t, ok)
		require.Equal(t, []any{"detached"}, detached)
		require.Nil(t, destroyed)

		// Flushing the other scope too removes the session's bucket.
		msg = flushTestaments(t, cli, wamp.Dict{"scope": detachedScope})
		require.IsType(t, &wamp.Result{}, msg)
		_, _, ok = testamentArgs(rlm, cli.ID)
		require.False(t, ok, "bucket not removed when empty")

		// Flushing the detached scope keeps the destroyed testaments.
		addAll()
		msg = flushTestaments(t, cli, wamp.Dict{"scope": detachedScope})
		require.IsType(t, &wamp.Result{}, msg)
		detached, destroyed, ok = testamentArgs(rlm, cli.ID)
		require.True(t, ok)
		require.Nil(t, detached)
		require.Equal(t, []any{"destroyed1", "destroyed2"}, destroyed)

		msg = flushTestaments(t, cli, wamp.Dict{"scope": destroyedScope})
		require.IsType(t, &wamp.Result{}, msg)
		_, _, ok = testamentArgs(rlm, cli.ID)
		require.False(t, ok, "bucket not removed when empty")

		// Flushing when there are no testaments succeeds.
		msg = flushTestaments(t, cli, nil)
		require.IsType(t, &wamp.Result{}, msg)
		_, _, ok = testamentArgs(rlm, cli.ID)
		require.False(t, ok)

		// An invalid scope is an error, and flushes nothing.
		addAll()
		msg = flushTestaments(t, cli, wamp.Dict{"scope": "bogus"})
		errMsg, isErr := msg.(*wamp.Error)
		require.True(t, isErr, "expected ERROR")
		require.Equal(t, wamp.ErrInvalidArgument, errMsg.Error)
		detached, destroyed, ok = testamentArgs(rlm, cli.ID)
		require.True(t, ok)
		require.Equal(t, []any{"detached"}, detached)
		require.Equal(t, []any{"destroyed1", "destroyed2"}, destroyed)

		// Flushing affects only the caller's testaments.
		_, destroyed, ok = testamentArgs(rlm, other.ID)
		require.True(t, ok)
		require.Equal(t, []any{"other"}, destroyed)
	})
}

// TestTestamentFlushNotPublished checks that a flushed testament is not
// published when the session closes, and that a testament that was not flushed
// still is.
func TestTestamentFlushNotPublished(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)

		sub := testClient(t, r)
		sub.Send() <- &wamp.Subscribe{
			Request: wamp.GlobalID(),
			Topic:   "testament.test",
		}
		msg, err := wamp.RecvTimeout(sub, time.Second)
		require.NoError(t, err)
		require.IsType(t, &wamp.Subscribed{}, msg)

		cli := testClient(t, r)
		addTestament(t, cli, "testament.test", "detached", detachedScope)
		addTestament(t, cli, "testament.test", "destroyed", destroyedScope)
		msg = flushTestaments(t, cli, wamp.Dict{"scope": destroyedScope})
		require.IsType(t, &wamp.Result{}, msg)

		cli.Close()

		msg, err = wamp.RecvTimeout(sub, time.Second)
		require.NoError(t, err)
		event, ok := msg.(*wamp.Event)
		require.True(t, ok, "expected EVENT")
		require.Equal(t, wamp.List{"detached"}, event.Arguments)

		synctest.Wait()
		select {
		case msg = <-sub.Recv():
			require.FailNowf(t, "flushed testament was published",
				"got %s %v", msg.MessageType(), msg)
		default:
		}
	})
}
