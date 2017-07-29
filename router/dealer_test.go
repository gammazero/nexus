package router

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gammazero/nexus/wamp"
)

func checkMetaReg(metaClient wamp.Peer, sessID wamp.ID) error {
	select {
	case <-time.After(time.Millisecond):
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
	metaClient, rtr := LinkedPeers()
	dealer := NewDealer(false, true, rtr).(*dealer)
	callee := newTestPeer()
	testProcedure := wamp.URI("nexus.test.endpoint")
	sess := &Session{Peer: callee}
	dealer.Submit(sess, &wamp.Register{Request: 123, Procedure: testProcedure})

	rsp := <-callee.Recv()
	// Test that callee receives a registered message.
	regID := rsp.(*wamp.Registered).Registration
	if regID == 0 {
		t.Fatal("invalid registration ID")
	}

	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail: ", err)
	}
	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail: ", err)
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
	metaClient, rtr := LinkedPeers()
	dealer := NewDealer(false, true, rtr).(*dealer)
	callee := newTestPeer()
	testProcedure := wamp.URI("nexus.test.endpoint")

	// Register a procedure.
	sess := &Session{Peer: callee}
	dealer.Submit(sess, &wamp.Register{Request: 123, Procedure: testProcedure})
	rsp := <-callee.Recv()
	regID := rsp.(*wamp.Registered).Registration

	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail: ", err)
	}
	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail: ", err)
	}

	// Unregister the procedure.
	dealer.Submit(sess, &wamp.Unregister{Request: 124, Registration: regID})

	// Check that callee received UNREGISTERED message.
	rsp = <-callee.Recv()
	unreg, ok := rsp.(*wamp.Unregistered)
	if !ok {
		t.Fatal("received wrong response type: ", rsp.MessageType())
	}
	if unreg.Request == 0 {
		t.Fatal("invalid unreg ID")
	}

	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail: ", err)
	}
	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail: ", err)
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
	metaClient, rtr := LinkedPeers()
	dealer := NewDealer(false, true, rtr).(*dealer)
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

	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail: ", err)
	}
	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail: ", err)
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
	metaClient, rtr := LinkedPeers()
	dealer := NewDealer(false, true, rtr).(*dealer)
	callee := newTestPeer()
	testProcedure := wamp.URI("nexus.test.endpoint")

	// Register a procedure.
	sess := &Session{Peer: callee}
	msg := &wamp.Register{Request: 123, Procedure: testProcedure}
	dealer.Submit(sess, msg)
	rsp := <-callee.Recv()
	regID := rsp.(*wamp.Registered).Registration

	if _, ok := dealer.procRegMap[testProcedure]; !ok {
		t.Fatal("dealer does not have registered procedure")
	}
	if _, ok := dealer.registrations[regID]; !ok {
		t.Fatal("dealer does not have registration")
	}

	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail: ", err)
	}
	if err := checkMetaReg(metaClient, sess.ID); err != nil {
		t.Fatal("Registration meta event fail: ", err)
	}

	// Test that removing the callee session removes the registration.
	dealer.RemoveSession(sess)

	// Register as a way to sync with dealer.
	sess2 := &Session{Peer: callee}
	dealer.Submit(sess2,
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
	metaClient, rtr := LinkedPeers()
	dealer := NewDealer(false, true, rtr).(*dealer)
	callee := newTestPeer()
	testProcedure := wamp.URI("nexus.test.endpoint")

	// Register a procedure.

	calleeRoles := map[string]interface{}{
		"roles": map[string]interface{}{
			"callee": map[string]interface{}{
				"features": map[string]interface{}{
					"call_canceling": true,
				},
			},
		},
	}

	calleeSess := &Session{Peer: callee, Details: calleeRoles}
	dealer.Submit(calleeSess,
		&wamp.Register{Request: 123, Procedure: testProcedure})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	if !ok {
		t.Fatal("did not receive REGISTERED response")
	}

	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
		t.Fatal("Registration meta event fail: ", err)
	}

	caller := newTestPeer()
	callerSession := &Session{Peer: caller}

	// Test calling valid procedure
	dealer.Submit(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	if !ok {
		t.Fatal("expected INVOCATION, got: ", rsp.MessageType())
	}

	// Test caller cancelling call. mode=kill
	opts := wamp.SetOption(nil, "mode", "kill")
	dealer.Submit(callerSession, &wamp.Cancel{Request: 125, Options: opts})

	// callee should receive an INTERRUPT request
	rsp = <-callee.Recv()
	interrupt, ok := rsp.(*wamp.Interrupt)
	if !ok {
		t.Fatal("callee expected INTERRUPT, got: ", rsp.MessageType())
	}
	if interrupt.Request != inv.Request {
		t.Fatal("INTERRUPT request ID does not match INVOCATION request ID")
	}

	// callee responds with ERROR message
	dealer.Submit(calleeSess, &wamp.Error{
		Type:    wamp.INVOCATION,
		Request: inv.Request,
		Error:   wamp.ErrCanceled,
		Details: map[string]interface{}{},
	})

	// Check that caller receives the ERROR message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR, got: ", rsp.MessageType())
	}
	if rslt.Error != wamp.ErrCanceled {
		t.Fatal("wrong error, want ", wamp.ErrCanceled, " got ", rslt.Error)
	}
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
