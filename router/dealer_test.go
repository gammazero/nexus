package router

import (
	"testing"

	"github.com/gammazero/nexus/wamp"
)

func TestBasicRegister(t *testing.T) {
	dealer := NewDealer(false, true).(*dealer)
	callee := newTestPeer()
	testProcedure := wamp.URI("nexus.test.endpoint")
	sess := &Session{Peer: callee}
	dealer.Submit(sess, &wamp.Register{Request: 123, Procedure: testProcedure})

	rsp := <-callee.Recv()
	// Test that callee receives a registered message.
	reg := rsp.(*wamp.Registered).Registration
	if reg == 0 {
		t.Fatal("invalid registration ID")
	}

	// Check that dealer has the correct endpoint registered.
	regs := dealer.registrations[testProcedure]
	if len(regs) != 1 {
		t.Fatal("dealer has wrong number of registrations")
	}
	if reg != regs[0] {
		t.Fatal("dealer reg ID different that what was returned to callee")
	}

	// Check that dealer has correct procedure registered.
	proc, ok := dealer.procedures[reg]
	if !ok {
		t.Fatal("dealer has no registered procedures")
	}
	if proc.procedure != testProcedure {
		t.Fatal("dealer has different test procedure than registered")
	}

	// Check the procedure cannot be registered more than once.
	dealer.Submit(sess, &wamp.Register{Request: 456, Procedure: testProcedure})
	rsp = <-callee.Recv()
	errMsg := rsp.(*wamp.Error)
	if errMsg.Error != wamp.ErrProcedureAlreadyExists {
		t.Error("expected error: ", wamp.ErrProcedureAlreadyExists)
	}
	if errMsg.Details == nil {
		t.Error("missing expected error details")
	}
}

func TestUnregister(t *testing.T) {
	dealer := NewDealer(false, true).(*dealer)
	callee := newTestPeer()
	testProcedure := wamp.URI("nexus.test.endpoint")

	// Register a procedure.
	sess := &Session{Peer: callee}
	dealer.Submit(sess, &wamp.Register{Request: 123, Procedure: testProcedure})
	rsp := <-callee.Recv()
	reg := rsp.(*wamp.Registered).Registration

	// Unregister the procedure.
	dealer.Submit(sess, &wamp.Unregister{Request: 124, Registration: reg})

	// Check that callee received UNREGISTERED message.
	rsp = <-callee.Recv()
	unreg, ok := rsp.(*wamp.Unregistered)
	if !ok {
		t.Fatal("received wrong response type: ", rsp.MessageType())
	}
	if unreg.Request == 0 {
		t.Fatal("invalid unreg ID")
	}

	// Check that dealer does not have registered endpoint
	_, ok = dealer.registrations[testProcedure]
	if ok {
		t.Fatal("dealer still has registered endpoint")
	}

	// Check that dealer does not have registered procedure
	_, ok = dealer.procedures[reg]
	if ok {
		t.Fatal("dealer still has registered procedure")
	}
}

func TestBasicCall(t *testing.T) {
	dealer := NewDealer(false, true).(*dealer)
	callee := newTestPeer()
	testProcedure := wamp.URI("nexus.test.endpoint")

	// Register a procedure.
	calleeSess := &Session{Peer: callee}
	dealer.Submit(calleeSess,
		&wamp.Register{Request: 123, Procedure: testProcedure})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}

	caller := newTestPeer()
	callerSession := &Session{Peer: caller}

	// Test calling invalid procedure
	dealer.Submit(callerSession,
		&wamp.Call{Request: 124, Procedure: wamp.URI("nexus.test.bad")})
	rsp = <-caller.Recv()
	errMsg, ok := rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR response, got: ", rsp.MessageType())
	}
	if errMsg.Error != wamp.ErrNoSuchProcedure {
		t.Fatal("expected error ", wamp.ErrNoSuchProcedure)
	}
	if errMsg.Details == nil {
		t.Fatal("expected error details")
	}

	// Test calling valid procedure
	dealer.Submit(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	if !ok {
		t.Fatal("expected INVOCATION, got: ", rsp.MessageType())
	}

	// Callee responds with a YIELD message
	dealer.Submit(calleeSess, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got: ", rsp.MessageType())
	}
	if rslt.Request != 125 {
		t.Fatal("wrong request ID in RESULT")
	}

	// Test calling valid procedure, with callee responding with error.
	dealer.Submit(callerSession,
		&wamp.Call{Request: 126, Procedure: testProcedure})
	// callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv = rsp.(*wamp.Invocation)

	// Callee responds with a ERROR message
	dealer.Submit(calleeSess, &wamp.Error{Request: inv.Request})

	// Check that caller received an ERROR message.
	rsp = <-caller.Recv()
	errMsg, ok = rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR response, got: ", rsp.MessageType())
	}
	if errMsg.Request != 126 {
		t.Fatal("wrong request ID in ERROR, should match call ID")
	}
}

func TestRemovePeer(t *testing.T) {
	dealer := NewDealer(false, true).(*dealer)
	callee := newTestPeer()
	testProcedure := wamp.URI("nexus.test.endpoint")

	// Register a procedure.
	sess := &Session{Peer: callee}
	msg := &wamp.Register{Request: 123, Procedure: testProcedure}
	dealer.Submit(sess, msg)
	rsp := <-callee.Recv()
	reg := rsp.(*wamp.Registered).Registration

	if _, ok := dealer.registrations[testProcedure]; !ok {
		t.Fatal("dealer does not have registered procedure")
	}
	if _, ok := dealer.procedures[reg]; !ok {
		t.Fatal("dealer does not have registration")
	}

	// Test that removing the callee session removes the registration.
	dealer.RemoveSession(sess)

	// Register as a way to sync with dealer.
	sess2 := &Session{Peer: callee}
	dealer.Submit(sess2,
		&wamp.Register{Request: 789, Procedure: wamp.URI("nexus.test.p2")})
	rsp = <-callee.Recv()

	if _, ok := dealer.registrations[testProcedure]; ok {
		t.Fatal("dealer still has registered procedure")
	}
	if _, ok := dealer.procedures[reg]; ok {
		t.Fatal("dealer still has registration")
	}

	// Tests that registering the callee again succeeds.
	msg.Request = 124
	dealer.Submit(sess, msg)
	rsp = <-callee.Recv()
	if rsp.MessageType() != wamp.REGISTERED {
		t.Fatal("expected ", wamp.REGISTERED, " got: ", rsp.MessageType())
	}
}

// ----- WAMP v.2 Testing -----

func TestCancelCall(t *testing.T) {
	// Test mode=kill
	// Test mode=killnowait
	// Test mode=skip
}

func TestCallTimeout(t *testing.T) {
}

func TestCallerIdentification(t *testing.T) {
	// Test disclose_caller
	// Test disclose_me
}

func TestPatternBasedRegistration(t *testing.T) {
	// Test match=prefix
	// Test match=wildcard
}

func TestProgressiveCallResults(t *testing.T) {
}

func TestSharedRegistration(t *testing.T) {
	// Test policy=single
	// Test policy=first
	// Test policy=last
	// Test policy=round_robin
	// Test policy=random
}
