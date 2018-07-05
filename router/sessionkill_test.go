package router

import (
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/wamp"
)

func TestSessionKill(t *testing.T) {
	defer leaktest.Check(t)()
	r, err := newTestRouter()
	if err != nil {
		t.Error(err)
	}
	defer r.Close()

	cli1, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	cli2, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	cli3, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	reason := wamp.URI("foo.bar.baz")
	message := "this is a test"

	// Request that client-3 be killed.
	cli1.Send(&wamp.Call{Request: wamp.GlobalID(), Procedure: wamp.MetaProcSessionKill, Arguments: wamp.List{cli3.ID}, ArgumentsKw: wamp.Dict{"reason": reason, "message": message}})

	msg, err := wamp.RecvTimeout(cli1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := msg.(*wamp.Result)
	if !ok {
		t.Fatal("Expected RESULT, got", msg.MessageType())
	}

	// Check that client-3 received GOODBYE.
	msg, err = wamp.RecvTimeout(cli3, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	g, ok := msg.(*wamp.Goodbye)
	if !ok {
		t.Fatal("expected GOODBYE, got", msg.MessageType())
	}
	if g.Reason != reason {
		t.Error("Wrong GOODBYE.Reason, got", g.Reason, "expected", reason)
	}
	if m, _ := wamp.AsString(g.Details["message"]); m != message {
		t.Error("Wrong message in GOODBYE, got", m, "expected", message)
	}

	// Check that client-2 did not get anything.
	_, err = wamp.RecvTimeout(cli2, time.Millisecond)
	if err == nil {
		t.Fatal("Expected timeout")
	}

	// Test that killing self gets error.
	cli1.Send(&wamp.Call{Request: wamp.GlobalID(), Procedure: wamp.MetaProcSessionKill, Arguments: wamp.List{cli1.ID}, ArgumentsKw: nil})

	msg, err = wamp.RecvTimeout(cli1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := msg.(*wamp.Error)
	if !ok {
		t.Fatal("Expected ERROR, got", msg.MessageType())
	}
	if e.Error != wamp.ErrNoSuchSession {
		t.Error("Wrong error, got", e.Error, "expected", wamp.ErrNoSuchSession)
	}

	cli1.Close()
	cli2.Close()
}

func TestSessionKillAll(t *testing.T) {
	defer leaktest.Check(t)()
	r, err := newTestRouter()
	if err != nil {
		t.Error(err)
	}
	defer r.Close()

	cli1, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	cli2, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	cli3, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	reason := wamp.URI("foo.bar.baz")
	message := "this is a test"

	cli1.Send(&wamp.Call{Request: wamp.GlobalID(), Procedure: wamp.MetaProcSessionKillAll, ArgumentsKw: wamp.Dict{"reason": reason, "message": message}})

	msg, err := wamp.RecvTimeout(cli1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := msg.(*wamp.Result)
	if !ok {
		t.Fatal("Expected RESULT, got", msg.MessageType())
	}

	msg, err = wamp.RecvTimeout(cli2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	g, ok := msg.(*wamp.Goodbye)
	if !ok {
		t.Fatal("expected GOODBYE, got", msg.MessageType())
	}
	if g.Reason != reason {
		t.Error("Wrong GOODBYE.Reason, got", g.Reason, "expected", reason)
	}
	if m, _ := wamp.AsString(g.Details["message"]); m != message {
		t.Error("Wrong message in GOODBYE, got", m, "expected", message)
	}

	msg, err = wamp.RecvTimeout(cli3, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	g, ok = msg.(*wamp.Goodbye)
	if !ok {
		t.Fatal("expected GOODBYE, got", msg.MessageType())
	}
	if g.Reason != reason {
		t.Error("Wrong GOODBYE.Reason, got", g.Reason, "expected", reason)
	}
	if m, _ := wamp.AsString(g.Details["message"]); m != message {
		t.Error("Wrong message in GOODBYE, got", m, "expected", message)
	}

	_, err = wamp.RecvTimeout(cli1, time.Millisecond)
	if err == nil {
		t.Fatal("Expected timeout")
	}

	cli1.Close()
}

func TestSessionKillByAuthid(t *testing.T) {
	defer leaktest.Check(t)()
	r, err := newTestRouter()
	if err != nil {
		t.Error(err)
	}
	defer r.Close()

	cli1, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}
	defer cli1.Close()

	cli2, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}
	defer cli2.Close()

	cli3, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}
	defer cli3.Close()

	reason := wamp.URI("foo.bar.baz")
	message := "this is a test"

	// All clients have the same authid, so killing by authid should kill all
	// except the requesting client.
	cli1.Send(&wamp.Call{Request: wamp.GlobalID(), Procedure: wamp.MetaProcSessionKillByAuthid, Arguments: wamp.List{cli1.Details["authid"]}, ArgumentsKw: wamp.Dict{"reason": reason, "message": message}})

	msg, err := wamp.RecvTimeout(cli1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := msg.(*wamp.Result)
	if !ok {
		t.Fatal("Expected RESULT, got", msg.MessageType())
	}

	// Check that client 2 gets kicked off.
	msg, err = wamp.RecvTimeout(cli2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	g, ok := msg.(*wamp.Goodbye)
	if !ok {
		t.Fatal("expected GOODBYE, got", msg.MessageType())
	}
	if g.Reason != reason {
		t.Error("Wrong GOODBYE.Reason, got", g.Reason, "expected", reason)
	}
	if m, _ := wamp.AsString(g.Details["message"]); m != message {
		t.Error("Wrong message in GOODBYE, got", m, "expected", message)
	}

	// Check that client 3 gets kicked off.
	msg, err = wamp.RecvTimeout(cli3, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	g, ok = msg.(*wamp.Goodbye)
	if !ok {
		t.Fatal("expected GOODBYE, got", msg.MessageType())
	}
	if g.Reason != reason {
		t.Error("Wrong GOODBYE.Reason, got", g.Reason, "expected", reason)
	}
	if m, _ := wamp.AsString(g.Details["message"]); m != message {
		t.Error("Wrong message in GOODBYE, got", m, "expected", message)
	}

	// Check that client 1 is not kicked off.
	_, err = wamp.RecvTimeout(cli1, time.Millisecond)
	if err == nil {
		t.Fatal("Expected timeout")
	}
}

func TestSessionModifyDetails(t *testing.T) {
	defer leaktest.Check(t)()
	r, err := newTestRouter()
	if err != nil {
		t.Error(err)
	}
	defer r.Close()

	caller, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}
	sessID := caller.ID

	// Call session meta-procedure to get session information.
	callID := wamp.GlobalID()
	delta := wamp.Dict{"xyzzy": nil, "pi": 3.14, "authid": "bob"}
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSessionModifyDetails,
		Arguments: wamp.List{caller.ID, delta},
	})
	msg, err := wamp.RecvTimeout(caller, time.Second)
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

	// Call session meta-procedure to get session information.
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSessionGet,
		Arguments: wamp.List{sessID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, ok = msg.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}
	if result.Request != callID {
		t.Fatal("wrong result ID")
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	details, ok := result.Arguments[0].(wamp.Dict)
	if !ok {
		t.Fatal("expected dict type arg")
	}
	authid, _ := wamp.AsString(details["authid"])
	if authid != "bob" {
		t.Error("expected bob, got", authid)
	}
	if _, ok = details["xyzzy"]; ok {
		t.Error("xyzzy should have been delete from details")
	}
	val, _ := wamp.AsFloat64(details["pi"])
	if val != 3.14 {
		t.Error("Did not get correct value for pi")
	}
}
