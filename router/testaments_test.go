package router

import (
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/v3/wamp"
)

func TestSessionTestaments(t *testing.T) {
	defer leaktest.Check(t)()
	r, err := newTestRouter()
	if err != nil {
		t.Error(err)
	}
	defer r.Close()

	sub, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}
	subscribeID := wamp.GlobalID()
	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: "testament.test1"})
	msg, err := wamp.RecvTimeout(sub, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := msg.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}

	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: "testament.test2"})
	msg, err = wamp.RecvTimeout(sub, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	caller1, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	caller2, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

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
	if err != nil {
		t.Fatal(err)
	}
	result, ok := msg.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}
	if result.Request != callID {
		t.Fatal("wrong result ID")
	}

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
	if err != nil {
		t.Fatal(err)
	}
	result, ok = msg.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}

	caller1.Close()
	caller2.Close()

	msg, err = wamp.RecvTimeout(sub, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	event, ok := msg.(*wamp.Event)
	if !ok {
		t.Fatal("expected EVENT, got", msg.MessageType())
	}
	val, _ := wamp.AsString(event.Arguments[0])
	if val != "foo" {
		t.Error("Argument value was invalid")
	}

	msg, err = wamp.RecvTimeout(sub, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	event, ok = msg.(*wamp.Event)
	if !ok {
		t.Fatal("expected EVENT, got", msg.MessageType())
	}
	val, _ = wamp.AsString(event.Arguments[0])
	if val != "foo" {
		t.Error("Argument value was invalid")
	}

	sub.Close()
}
