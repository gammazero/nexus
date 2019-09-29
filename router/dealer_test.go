package router

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

func newTestDealer() (*dealer, wamp.Peer) {
	d := newDealer(logger, false, true, debug)
	metaClient, rtr := transport.LinkedPeers()
	d.setMetaPeer(rtr)
	return d, metaClient
}

func checkMetaReg(metaClient wamp.Peer, sessID wamp.ID) error {
	select {
	case <-time.After(time.Second):
		return errors.New("timed out waiting for event")
	case msg := <-metaClient.Recv():
		event, ok := msg.(*wamp.Publish)
		if !ok {
			return fmt.Errorf("expected PUBLISH, got %s", msg.MessageType())
		}
		if len(event.Arguments) != 2 {
			return errors.New("expected reg meta event to have 2 args")
		}
		sid, ok := event.Arguments[0].(wamp.ID)
		if !ok {
			return errors.New("wrong type for session ID arg")
		}
		if sid != sessID {
			return errors.New("reg meta returned wrong session ID")
		}
	}
	return nil
}

func TestBasicRegister(t *testing.T) {
	dealer, metaClient := newTestDealer()

	// Register callee
	callee := newTestPeer()
	sess := wamp.NewSession(callee, 0, nil, nil)
	dealer.register(sess, &wamp.Register{Request: 123, Procedure: testProcedure})

	rsp := <-callee.Recv()
	// Test that callee receives a registered message.
	regID := rsp.(*wamp.Registered).Registration
	if regID == 0 {
		t.Fatal("invalid registration ID")
	}

	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	// Check that dealer has the correct endpoint registered.
	reg, ok := dealer.procRegMap[testProcedure]
	if !ok {
		t.Fatal("registration not found")
	}
	if len(reg.callees) != 1 {
		t.Fatal("registration has wrong number of callees")
	}
	if reg.id != regID {
		t.Fatal("dealer reg ID different that what was returned to callee")
	}

	// Check that dealer has correct registration reverse mapping.
	reg, ok = dealer.registrations[regID]
	if !ok {
		t.Fatal("dealer missing regID -> registration")
	}
	if reg.procedure != testProcedure {
		t.Fatal("dealer has different test procedure than registered")
	}

	// Check the procedure cannot be registered more than once.
	dealer.register(sess, &wamp.Register{Request: 456, Procedure: testProcedure})
	rsp = <-callee.Recv()
	errMsg := rsp.(*wamp.Error)
	if errMsg.Error != wamp.ErrProcedureAlreadyExists {
		t.Error("expected error:", wamp.ErrProcedureAlreadyExists)
	}
	if errMsg.Details == nil {
		t.Error("missing expected error details")
	}
}

func TestUnregister(t *testing.T) {
	dealer, metaClient := newTestDealer()

	// Register a procedure.
	callee := newTestPeer()
	sess := wamp.NewSession(callee, 0, nil, nil)
	dealer.register(sess, &wamp.Register{Request: 123, Procedure: testProcedure})
	rsp := <-callee.Recv()
	regID := rsp.(*wamp.Registered).Registration

	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	// Unregister the procedure.
	dealer.unregister(sess, &wamp.Unregister{Request: 124, Registration: regID})

	// Check that callee received UNREGISTERED message.
	rsp = <-callee.Recv()
	unreg, ok := rsp.(*wamp.Unregistered)
	if !ok {
		t.Fatal("received wrong response type:", rsp.MessageType())
	}
	if unreg.Request == 0 {
		t.Fatal("invalid unreg ID")
	}

	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	// Check that dealer does not have registered endpoint
	_, ok = dealer.procRegMap[testProcedure]
	if ok {
		t.Fatal("dealer still has registeration")
	}

	// Check that dealer does not have registration reverse mapping
	_, ok = dealer.registrations[regID]
	if ok {
		t.Fatal("dealer still has regID -> registration")
	}
}

func TestBasicCall(t *testing.T) {
	dealer, metaClient := newTestDealer()

	// Register a procedure.
	callee := newTestPeer()
	calleeSess := wamp.NewSession(callee, 0, nil, nil)
	dealer.register(calleeSess,
		&wamp.Register{Request: 123, Procedure: testProcedure})
	var rsp wamp.Message
	select {
	case rsp = <-callee.Recv():
	case <-time.After(time.Millisecond):
		t.Fatal("timed out waiting for response")
	}
	_, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}
	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling invalid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 124, Procedure: wamp.URI("nexus.test.bad")})
	rsp = <-callerSession.Recv()
	errMsg, ok := rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR response, got:", rsp.MessageType())
	}
	if errMsg.Error != wamp.ErrNoSuchProcedure {
		t.Fatal("expected error", wamp.ErrNoSuchProcedure)
	}
	if errMsg.Details == nil {
		t.Fatal("expected error details")
	}

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	if !ok {
		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got:", rsp.MessageType())
	}
	if rslt.Request != 125 {
		t.Fatal("wrong request ID in RESULT")
	}
	if ok, _ = wamp.AsBool(rslt.Details["progress"]); ok {
		t.Fatal("progress flag should not be set for response")
	}

	// Test calling valid procedure, with callee responding with error.
	dealer.call(callerSession,
		&wamp.Call{Request: 126, Procedure: testProcedure})
	// callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv = rsp.(*wamp.Invocation)

	// Callee responds with a ERROR message
	dealer.error(&wamp.Error{Request: inv.Request})

	// Check that caller received an ERROR message.
	rsp = <-caller.Recv()
	errMsg, ok = rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR response, got:", rsp.MessageType())
	}
	if errMsg.Request != 126 {
		t.Fatal("wrong request ID in ERROR, should match call ID")
	}
}

func TestRemovePeer(t *testing.T) {
	dealer, metaClient := newTestDealer()

	// Register a procedure.
	callee := newTestPeer()
	sess := wamp.NewSession(callee, 0, nil, nil)
	msg := &wamp.Register{Request: 123, Procedure: testProcedure}
	dealer.register(sess, msg)
	rsp := <-callee.Recv()
	regID := rsp.(*wamp.Registered).Registration

	if _, ok := dealer.procRegMap[testProcedure]; !ok {
		t.Fatal("dealer does not have registered procedure")
	}
	if _, ok := dealer.registrations[regID]; !ok {
		t.Fatal("dealer does not have registration")
	}

	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	// Test that removing the callee session removes the registration.
	dealer.removeSession(sess)

	// Register as a way to sync with dealer.
	sess2 := wamp.NewSession(callee, 0, nil, nil)
	dealer.register(sess2,
		&wamp.Register{Request: 789, Procedure: wamp.URI("nexus.test.p2")})
	rsp = <-callee.Recv()

	if _, ok := dealer.procRegMap[testProcedure]; ok {
		t.Fatal("dealer still has registered procedure")
	}
	if _, ok := dealer.registrations[regID]; ok {
		t.Fatal("dealer still has registration")
	}

	// Tests that registering the callee again succeeds.
	msg.Request = 124
	dealer.register(sess, msg)
	rsp = <-callee.Recv()
	if rsp.MessageType() != wamp.REGISTERED {
		t.Fatal("expected", wamp.REGISTERED, "got:", rsp.MessageType())
	}
}

// ----- WAMP v.2 Testing -----

func TestCancelCallModeKill(t *testing.T) {
	dealer, metaClient := newTestDealer()

	calleeRoles := wamp.Dict{
		"roles": wamp.Dict{
			"callee": wamp.Dict{
				"features": wamp.Dict{
					"call_canceling": true,
				},
			},
		},
	}

	// Register a procedure.
	callee := newTestPeer()
	calleeSess := wamp.NewSession(callee, 0, nil, calleeRoles)
	dealer.register(calleeSess,
		&wamp.Register{Request: 123, Procedure: testProcedure})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}

	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	if !ok {
		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
	}

	// Test caller cancelling call. mode=kill
	opts := wamp.SetOption(nil, "mode", "kill")
	dealer.cancel(callerSession, &wamp.Cancel{Request: 125, Options: opts})

	// callee should receive an INTERRUPT request
	rsp = <-callee.Recv()
	interrupt, ok := rsp.(*wamp.Interrupt)
	if !ok {
		t.Fatal("callee expected INTERRUPT, got:", rsp.MessageType())
	}
	if interrupt.Request != inv.Request {
		t.Fatal("INTERRUPT request ID does not match INVOCATION request ID")
	}

	// callee responds with ERROR message
	dealer.error(&wamp.Error{
		Type:    wamp.INVOCATION,
		Request: inv.Request,
		Error:   wamp.ErrCanceled,
		Details: wamp.Dict{"reason": "callee canceled"},
	})

	// Check that caller receives the ERROR message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR, got:", rsp.MessageType())
	}
	if rslt.Error != wamp.ErrCanceled {
		t.Fatal("wrong error, want", wamp.ErrCanceled, "got", rslt.Error)
	}
	if len(rslt.Details) == 0 {
		t.Fatal("expected details in message")
	}
	if s, _ := wamp.AsString(rslt.Details["reason"]); s != "callee canceled" {
		t.Fatal("Did not get error message from caller")
	}
}

func TestCancelCallModeKillNoWait(t *testing.T) {
	dealer, metaClient := newTestDealer()

	calleeRoles := wamp.Dict{
		"roles": wamp.Dict{
			"callee": wamp.Dict{
				"features": wamp.Dict{
					"call_canceling": true,
				},
			},
		},
	}

	// Register a procedure.
	callee := newTestPeer()
	calleeSess := wamp.NewSession(callee, 0, nil, calleeRoles)
	dealer.register(calleeSess,
		&wamp.Register{Request: 123, Procedure: testProcedure})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}

	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	if !ok {
		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
	}

	// Test caller cancelling call. mode=kill
	opts := wamp.SetOption(nil, "mode", "killnowait")
	dealer.cancel(callerSession, &wamp.Cancel{Request: 125, Options: opts})

	// callee should receive an INTERRUPT request
	rsp = <-callee.Recv()
	interrupt, ok := rsp.(*wamp.Interrupt)
	if !ok {
		t.Fatal("callee expected INTERRUPT, got:", rsp.MessageType())
	}
	if interrupt.Request != inv.Request {
		t.Fatal("INTERRUPT request ID does not match INVOCATION request ID")
	}

	// callee responds with ERROR message
	dealer.error(&wamp.Error{
		Type:    wamp.INVOCATION,
		Request: inv.Request,
		Error:   wamp.ErrCanceled,
		Details: wamp.Dict{"reason": "callee canceled"},
	})

	// Check that caller receives the ERROR message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR, got:", rsp.MessageType())
	}
	if rslt.Error != wamp.ErrCanceled {
		t.Fatal("wrong error, want", wamp.ErrCanceled, "got", rslt.Error)
	}
	if len(rslt.Details) != 0 {
		t.Fatal("should not have details; result should not be from callee")
	}
}

func TestCancelCallModeSkip(t *testing.T) {
	dealer, metaClient := newTestDealer()

	// Register a procedure.
	callee := newTestPeer()
	calleeRoles := wamp.Dict{
		"roles": wamp.Dict{
			"callee": wamp.Dict{
				"features": wamp.Dict{
					"call_canceling": true,
				},
			},
		},
	}

	calleeSess := wamp.NewSession(callee, 0, nil, calleeRoles)
	dealer.register(calleeSess,
		&wamp.Register{Request: 123, Procedure: testProcedure})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}

	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	_, ok = rsp.(*wamp.Invocation)
	if !ok {
		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
	}

	// Test caller cancelling call. mode=kill
	opts := wamp.SetOption(nil, "mode", "skip")
	dealer.cancel(callerSession, &wamp.Cancel{Request: 125, Options: opts})

	// callee should NOT receive an INTERRUPT request
	select {
	case <-time.After(200 * time.Millisecond):
	case msg := <-callee.Recv():
		t.Fatal("callee received unexpected message:", msg.MessageType())
	}

	// Check that caller receives the ERROR message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR, got:", rsp.MessageType())
	}
	if rslt.Error != wamp.ErrCanceled {
		t.Fatal("wrong error, want", wamp.ErrCanceled, "got", rslt.Error)
	}
}

func TestSharedRegistrationRoundRobin(t *testing.T) {
	dealer, metaClient := newTestDealer()

	calleeRoles := wamp.Dict{
		"roles": wamp.Dict{
			"callee": wamp.Dict{
				"features": wamp.Dict{
					"shared_registration": true,
				},
			},
		},
	}

	// Register callee1 with roundrobin shared registration
	callee1 := newTestPeer()
	calleeSess1 := wamp.NewSession(callee1, 0, nil, calleeRoles)
	dealer.register(calleeSess1, &wamp.Register{
		Request:   123,
		Procedure: testProcedure,
		Options:   wamp.SetOption(nil, "invoke", "roundrobin"),
	})
	rsp := <-callee1.Recv()
	regMsg, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}
	regID1 := regMsg.Registration
	err := checkMetaReg(metaClient, calleeSess1.ID)
	if err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err = checkMetaReg(metaClient, calleeSess1.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	// Register callee2 with roundrobin shared registration
	callee2 := newTestPeer()
	calleeSess2 := wamp.NewSession(callee2, 0, nil, calleeRoles)
	dealer.register(calleeSess2, &wamp.Register{
		Request:   124,
		Procedure: testProcedure,
		Options:   wamp.SetOption(nil, "invoke", "roundrobin"),
	})
	rsp = <-callee2.Recv()
	regMsg, ok = rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}
	if err = checkMetaReg(metaClient, calleeSess2.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	regID2 := regMsg.Registration

	if regID1 != regID2 {
		t.Fatal("procedures should have same registration")
	}

	// Test calling valid procedure
	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee1 received an INVOCATION message.
	var inv *wamp.Invocation
	select {
	case rsp = <-callee1.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		if !ok {
			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
		}
	case rsp = <-callee2.Recv():
		t.Fatal("should not have received from callee2")
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess1, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got:", rsp.MessageType())
	}
	if rslt.Request != 125 {
		t.Fatal("wrong request ID in RESULT")
	}

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 126, Procedure: testProcedure})

	// Test that callee2 received an INVOCATION message.
	select {
	case rsp = <-callee2.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		if !ok {
			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
		}
	case rsp = <-callee1.Recv():
		t.Fatal("should not have received from callee1")
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess2, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok = rsp.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got:", rsp.MessageType())
	}
	if rslt.Request != 126 {
		t.Fatal("wrong request ID in RESULT")
	}
}

func TestSharedRegistrationFirst(t *testing.T) {
	dealer, metaClient := newTestDealer()

	calleeRoles := wamp.Dict{
		"roles": wamp.Dict{
			"callee": wamp.Dict{
				"features": wamp.Dict{
					"shared_registration": true,
				},
			},
		},
	}

	// Register callee1 with first shared registration
	callee1 := newTestPeer()
	calleeSess1 := wamp.NewSession(callee1, 1111, nil, calleeRoles)
	dealer.register(calleeSess1, &wamp.Register{
		Request:   123,
		Procedure: testProcedure,
		Options:   wamp.SetOption(nil, "invoke", "first"),
	})
	rsp := <-callee1.Recv()
	regMsg, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}
	regID1 := regMsg.Registration
	err := checkMetaReg(metaClient, calleeSess1.ID)
	if err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err = checkMetaReg(metaClient, calleeSess1.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	// Register callee2 with roundrobin shared registration
	callee2 := newTestPeer()
	calleeSess2 := wamp.NewSession(callee2, 2222, nil, calleeRoles)
	dealer.register(calleeSess2, &wamp.Register{
		Request:   1233,
		Procedure: testProcedure,
		Options:   wamp.SetOption(nil, "invoke", "roundrobin"),
	})
	rsp = <-callee2.Recv()
	_, ok = rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR response")
	}

	// Register callee2 with "first" shared registration
	dealer.register(calleeSess2, &wamp.Register{
		Request:   124,
		Procedure: testProcedure,
		Options:   wamp.SetOption(nil, "invoke", "first"),
	})
	select {
	case rsp = <-callee2.Recv():
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for REGISTERED")
	}

	if regMsg, ok = rsp.(*wamp.Registered); !ok {
		t.Fatal("did not receive REGISTERED response")
	}
	regID2 := regMsg.Registration
	if err = checkMetaReg(metaClient, calleeSess2.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	if regID1 != regID2 {
		t.Fatal("procedures should have same registration")
	}

	// Test calling valid procedure
	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 333, nil, nil)
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee1 received an INVOCATION message.
	var inv *wamp.Invocation
	select {
	case rsp = <-callee1.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		if !ok {
			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
		}
	case rsp = <-callee2.Recv():
		t.Fatal("should not have received from callee2")
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for INVOCATION")
	}

	// Callee1 responds with a YIELD message
	dealer.yield(calleeSess1, &wamp.Yield{Request: inv.Request})

	// Check that caller received a RESULT message.
	select {
	case rsp = <-caller.Recv():
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	}
	rslt, ok := rsp.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got:", rsp.MessageType())
	}
	if rslt.Request != 125 {
		t.Fatal("wrong request ID in RESULT")
	}

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 126, Procedure: testProcedure})

	// Test that callee1 received an INVOCATION message.
	select {
	case rsp = <-callee1.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		if !ok {
			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
		}
	case rsp = <-callee2.Recv():
		t.Fatal("should not have received from callee2")
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess1, &wamp.Yield{Request: inv.Request})

	// Check that caller received a RESULT message.
	select {
	case rsp = <-caller.Recv():
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	}
	rslt, ok = rsp.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got:", rsp.MessageType())
	}
	if rslt.Request != 126 {
		t.Fatal("wrong request ID in RESULT")
	}

	// Remove callee1
	dealer.removeSession(calleeSess1)
	if err = checkMetaReg(metaClient, calleeSess1.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err = checkMetaReg(metaClient, calleeSess1.ID); err == nil {
		t.Fatal("Expected error")
	}

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 127, Procedure: testProcedure})

	// Test that callee2 received an INVOCATION message.
	select {
	case rsp = <-callee2.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		if !ok {
			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
		}
	case rsp = <-callee1.Recv():
		t.Fatal("should not have received from callee1")
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess2, &wamp.Yield{Request: inv.Request})

	// Check that caller received a RESULT message.
	select {
	case rsp = <-caller.Recv():
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	}

	rslt, ok = rsp.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got:", rsp.MessageType())
	}
	if rslt.Request != 127 {
		t.Fatal("wrong request ID in RESULT")
	}
}

func TestSharedRegistrationLast(t *testing.T) {
	dealer, metaClient := newTestDealer()

	calleeRoles := wamp.Dict{
		"roles": wamp.Dict{
			"callee": wamp.Dict{
				"features": wamp.Dict{
					"shared_registration": true,
				},
			},
		},
	}

	// Register callee1 with last shared registration
	callee1 := newTestPeer()
	calleeSess1 := wamp.NewSession(callee1, 0, nil, calleeRoles)
	dealer.register(calleeSess1, &wamp.Register{
		Request:   123,
		Procedure: testProcedure,
		Options:   wamp.SetOption(nil, "invoke", "last"),
	})
	rsp := <-callee1.Recv()
	_, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}
	err := checkMetaReg(metaClient, calleeSess1.ID)
	if err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err = checkMetaReg(metaClient, calleeSess1.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	// Register callee2 with last shared registration
	callee2 := newTestPeer()
	calleeSess2 := wamp.NewSession(callee2, 0, nil, calleeRoles)
	dealer.register(calleeSess2, &wamp.Register{
		Request:   124,
		Procedure: testProcedure,
		Options:   wamp.SetOption(nil, "invoke", "last"),
	})
	rsp = <-callee2.Recv()
	if _, ok = rsp.(*wamp.Registered); !ok {
		t.Fatal("did not receive REGISTERED response")
	}
	if err = checkMetaReg(metaClient, calleeSess2.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	// Test calling valid procedure
	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee2 received an INVOCATION message.
	var inv *wamp.Invocation
	select {
	case rsp = <-callee2.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		if !ok {
			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
		}
	case rsp = <-callee1.Recv():
		t.Fatal("should not have received from callee1")
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess2, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got:", rsp.MessageType())
	}
	if rslt.Request != 125 {
		t.Fatal("wrong request ID in RESULT")
	}

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 126, Procedure: testProcedure})

	// Test that callee2 received an INVOCATION message.
	select {
	case rsp = <-callee2.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		if !ok {
			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
		}
	case rsp = <-callee1.Recv():
		t.Fatal("should not have received from callee1")
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess2, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok = rsp.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got:", rsp.MessageType())
	}
	if rslt.Request != 126 {
		t.Fatal("wrong request ID in RESULT")
	}

	// Remove callee2
	dealer.removeSession(calleeSess2)
	if err = checkMetaReg(metaClient, calleeSess2.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err = checkMetaReg(metaClient, calleeSess1.ID); err == nil {
		t.Fatal("Expected error")
	}

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 127, Procedure: testProcedure})

	// Test that callee1 received an INVOCATION message.
	select {
	case rsp = <-callee1.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		if !ok {
			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
		}
	case rsp = <-callee2.Recv():
		t.Fatal("should not have received from callee2")
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess1, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok = rsp.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got:", rsp.MessageType())
	}
	if rslt.Request != 127 {
		t.Fatal("wrong request ID in RESULT")
	}
}

func TestPatternBasedRegistration(t *testing.T) {
	dealer, metaClient := newTestDealer()

	calleeRoles := wamp.Dict{
		"roles": wamp.Dict{
			"callee": wamp.Dict{
				"features": wamp.Dict{
					"shared_registration": true,
				},
			},
		},
	}

	// Register a procedure with wildcard match.
	callee := newTestPeer()
	calleeSess := wamp.NewSession(callee, 0, nil, calleeRoles)
	dealer.register(calleeSess,
		&wamp.Register{
			Request:   123,
			Procedure: testProcedureWC,
			Options: wamp.Dict{
				wamp.OptMatch: wamp.MatchWildcard,
			},
		})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}
	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling valid procedure with full name.  Widlcard should match.
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	if !ok {
		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
	}
	details, ok := wamp.AsDict(inv.Details)
	if !ok {
		t.Fatal("INVOCATION missing details")
	}
	proc, _ := wamp.AsURI(details[wamp.OptProcedure])
	if proc != testProcedure {
		t.Error("INVOCATION has missing or incorrect procedure detail")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got:", rsp.MessageType())
	}
	if rslt.Request != 125 {
		t.Fatal("wrong request ID in RESULT")
	}
}

func TestRPCBlockedUnresponsiveCallee(t *testing.T) {
	const (
		rpcExecTime    = time.Second
		timeoutMs      = 2000
		shortTimeoutMs = 200
	)
	dealer, metaClient := newTestDealer()

	// Register a procedure.
	callee, rtr := transport.LinkedPeers()
	calleeSess := wamp.NewSession(rtr, 0, nil, nil)
	opts := wamp.Dict{wamp.OptTimeout: timeoutMs}
	dealer.register(calleeSess,
		&wamp.Register{Request: 223, Procedure: testProcedure, Options: opts})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}

	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	caller, rtr := transport.LinkedPeers()
	callerSession := wamp.NewSession(rtr, 0, nil, nil)

	// Call unresponsive callee until call dropped.
	var i int
sendLoop:
	for {
		i++
		fmt.Println("Calling", i)
		// Test calling valid procedure
		dealer.call(callerSession, &wamp.Call{
			Request:   wamp.ID(i + 225),
			Procedure: testProcedure,
			Options:   opts,
		})
		select {
		case rsp = <-caller.Recv():
			break sendLoop
		default:
		}
	}

	callee.Close()

	// Test that caller received an ERROR message.
	rslt, ok := rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR, got:", rsp.MessageType())
	}
	if rslt.Error != wamp.ErrNetworkFailure {
		t.Fatal("wrong error, want", wamp.ErrNetworkFailure, "got", rslt.Error)
	}
}

func TestCallerIdentification(t *testing.T) {
	// Test disclose_caller
	// Test disclose_me
	dealer, metaClient := newTestDealer()

	calleeRoles := wamp.Dict{
		"roles": wamp.Dict{
			"callee": wamp.Dict{
				"features": wamp.Dict{
					"caller_identification": true,
				},
			},
		},
	}

	// Register a procedure, set option to request disclosing caller.
	callee := newTestPeer()
	calleeSess := wamp.NewSession(callee, 0, nil, calleeRoles)
	dealer.register(calleeSess,
		&wamp.Register{
			Request:   123,
			Procedure: testProcedure,
			Options:   wamp.Dict{"disclose_caller": true},
		})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}
	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}
	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail:", err)
	}

	caller := newTestPeer()
	callerID := wamp.ID(11235813)
	callerSession := wamp.NewSession(caller, callerID, nil, nil)

	// Test calling valid procedure with full name.  Widlcard should match.
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	if !ok {
		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
	}

	// Test that invocation contains caller ID.
	if id, _ := wamp.AsID(inv.Details["caller"]); id != callerID {
		fmt.Println("===> details:", inv.Details)
		t.Fatal("Did not get expected caller ID")
	}
}

func TestWrongYielder(t *testing.T) {
	dealer := newDealer(logger, false, true, debug)

	// Register a procedure.
	callee := newTestPeer()
	calleeSess := wamp.NewSession(callee, 7777, nil, nil)
	dealer.register(calleeSess,
		&wamp.Register{Request: 4321, Procedure: testProcedure})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}

	// Create caller
	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Create imposter callee
	badCallee := newTestPeer()
	badCalleeSess := wamp.NewSession(badCallee, 1313, nil, nil)

	// Call the procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 4322, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	if !ok {
		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
	}

	// Imposter callee responds with a YIELD message
	dealer.yield(badCalleeSess, &wamp.Yield{Request: inv.Request})

	// Check that caller did not received a RESULT message.
	select {
	case rsp = <-caller.Recv():
		t.Fatal("Caller received response from imposter callee")
	case <-time.After(200 * time.Millisecond):
	}
}
