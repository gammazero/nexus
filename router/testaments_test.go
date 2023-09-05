package router

import (
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

func TestSessionTestaments(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)

	sub := testClient(t, r)
	subscribeID := wamp.GlobalID()
	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: "testament.test1"})
	msg, err := wamp.RecvTimeout(sub, time.Second)
	require.NoError(t, err)
	_, ok := msg.(*wamp.Subscribed)
	require.True(t, ok, "expected RESULT")

	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: "testament.test2"})
	msg, err = wamp.RecvTimeout(sub, time.Second)
	require.NoError(t, err)

	caller1 := testClient(t, r)
	caller2 := testClient(t, r)

	callID := wamp.GlobalID()
	caller1.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSessionAddTestament,
		Arguments: wamp.List{
			"testament.test1",
			wamp.List{"foo"},
			wamp.Dict{},
		},
	})

	msg, err = wamp.RecvTimeout(caller1, time.Second)
	require.NoError(t, err)
	result, ok := msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")

	caller2.Send(&wamp.Call{
		Request:   wamp.GlobalID(),
		Procedure: wamp.MetaProcSessionAddTestament,
		Arguments: wamp.List{
			"testament.test2",
			wamp.List{"foo"},
			wamp.Dict{},
		},
	})

	msg, err = wamp.RecvTimeout(caller2, time.Second)
	require.NoError(t, err)
	result, ok = msg.(*wamp.Result)
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
}
