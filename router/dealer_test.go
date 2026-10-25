package router //nolint:testpackage

import (
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

func newTestDealer(t *testing.T) (*dealer, wamp.Peer) {
	d := newDealer(logger, false, true, debug)
	metaClient, rtr := transport.LinkedPeers()
	d.setMetaPeer(rtr)
	t.Cleanup(func() {
		d.close()
	})
	return d, metaClient
}

func checkMetaReg(t *testing.T, metaClient wamp.Peer, sessID wamp.ID) {
	select {
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for event")
	case msg := <-metaClient.Recv():
		event, ok := msg.(*wamp.Publish)
		require.True(t, ok, "expected PUBLISH")
		require.Equal(t, 2, len(event.Arguments), "expected reg meta event to have 2 args")
		sid, ok := event.Arguments[0].(wamp.ID)
		require.True(t, ok, "wrong type for session ID arg")
		require.Equal(t, sessID, sid, "reg meta returned wrong session ID")
	}
}

func TestBasicRegister(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

	// Register callee
	callee := newTestPeer()
	sess := wamp.NewSession(callee, 0, nil, nil)
	dealer.register(sess, &wamp.Register{Request: 123, Procedure: testProcedure})

	rsp := <-callee.Recv()
	// Test that callee receives a registered message.
	regID := rsp.(*wamp.Registered).Registration
	if regID == 0 {
		require.FailNow(t, "invalid registration ID")
	}

	checkMetaReg(t, metaClient, sess.ID)
	checkMetaReg(t, metaClient, sess.ID)

	// Check that dealer has the correct endpoint registered.
	reg, ok := dealer.procRegMap[testProcedure]
	require.True(t, ok, "registration not found")
	require.Equal(t, 1, len(reg.callees), "registration has wrong number of callees")
	require.Equal(t, regID, reg.id, "dealer reg ID different that what was returned to callee")

	// Check that dealer has correct registration reverse mapping.
	reg, ok = dealer.registrations[regID]
	require.True(t, ok, "dealer missing regID -> registration")
	require.Equal(t, testProcedure, reg.procedure, "dealer has different test procedure than registered")

	// Check the procedure cannot be registered more than once.
	dealer.register(sess, &wamp.Register{Request: 456, Procedure: testProcedure})
	rsp = <-callee.Recv()
	errMsg := rsp.(*wamp.Error)
	require.Equal(t, wamp.ErrProcedureAlreadyExists, errMsg.Error)
	require.NotNil(t, errMsg.Details, "missing expected error details")
}

func TestUnregister(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

	// Register a procedure.
	callee := newTestPeer()
	sess := wamp.NewSession(callee, 0, nil, nil)
	dealer.register(sess, &wamp.Register{Request: 123, Procedure: testProcedure})
	rsp := <-callee.Recv()
	regID := rsp.(*wamp.Registered).Registration

	checkMetaReg(t, metaClient, sess.ID)
	checkMetaReg(t, metaClient, sess.ID)

	// Unregister the procedure.
	dealer.unregister(sess, &wamp.Unregister{Request: 124, Registration: regID})

	// Check that callee received UNREGISTERED message.
	rsp = <-callee.Recv()
	unreg, ok := rsp.(*wamp.Unregistered)
	require.True(t, ok, "received wrong response type")
	require.NotZero(t, unreg.Request, "invalid unreg ID")

	checkMetaReg(t, metaClient, sess.ID)
	checkMetaReg(t, metaClient, sess.ID)

	// Check that dealer does not have registered endpoint
	_, ok = dealer.procRegMap[testProcedure]
	require.False(t, ok, "dealer still has registeration")

	// Check that dealer does not have registration reverse mapping
	_, ok = dealer.registrations[regID]
	require.False(t, ok, "dealer still has regID -> registration")
}

func TestBasicCall(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

	// Register a procedure.
	callee := newTestPeer()
	calleeSess := wamp.NewSession(callee, 0, nil, nil)
	dealer.register(calleeSess,
		&wamp.Register{Request: 123, Procedure: testProcedure})
	var rsp wamp.Message
	select {
	case rsp = <-callee.Recv():
	case <-time.After(200 * time.Millisecond):
		require.FailNow(t, "timed out waiting for response")
	}
	_, ok := rsp.(*wamp.Registered)
	require.True(t, ok, "did not receive REGISTERED response")
	checkMetaReg(t, metaClient, calleeSess.ID)
	checkMetaReg(t, metaClient, calleeSess.ID)

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling invalid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 124, Procedure: wamp.URI("nexus.test.bad")})
	rsp = <-callerSession.Recv()
	errMsg, ok := rsp.(*wamp.Error)
	require.True(t, ok, "expected ERROR response")
	require.Equal(t, wamp.ErrNoSuchProcedure, errMsg.Error)
	require.NotNil(t, errMsg.Details, "expected error details")

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	require.True(t, ok, "expected INVOCATION")

	// Callee responds with a YIELD message
	dealer.yield(calleeSess, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, wamp.ID(125), rslt.Request, "wrong request ID in RESULT")
	ok, _ = wamp.AsBool(rslt.Details["progress"])
	require.False(t, ok, "progress flag should not be set for response")

	// Test calling valid procedure, with callee responding with error.
	dealer.call(callerSession,
		&wamp.Call{Request: 126, Procedure: testProcedure})
	// callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv = rsp.(*wamp.Invocation)

	// Callee responds with a ERROR message
	dealer.error(calleeSess, &wamp.Error{Request: inv.Request})

	// Check that caller received an ERROR message.
	rsp = <-caller.Recv()
	errMsg, ok = rsp.(*wamp.Error)
	require.True(t, ok, "expected ERROR response")
	require.Equal(t, wamp.ID(126), errMsg.Request, "wrong request ID in ERROR, should match call ID")
}

// Ensure INVOCATION.Request IDs are incremented by 1 and scoped to the
// Callee's session.
func TestInvocationSessionSequentialIDs(t *testing.T) {
	dealer, _ := newTestDealer(t)

	// Set up 2 Callees with 2 unique registered procedure names
	const numCallee = 2
	const numProcNames = 2
	var procNames [numCallee][numProcNames]wamp.URI
	var callee [numCallee]*testPeer
	var calleeSess [numCallee]*wamp.Session

	routerScopeID := 1
	for i := range numCallee {
		callee[i] = newTestPeer()
		calleeSess[i] = wamp.NewSession(callee[i], wamp.ID(9990+i), nil, nil)
		for n := range numProcNames {
			procNames[i][n] = wamp.URI(fmt.Sprintf("nexus.test.callee%d.proc%d", i, n))
			dealer.register(calleeSess[i],
				&wamp.Register{
					Request:   wamp.ID(n),
					Procedure: procNames[i][n],
				})

			var rsp wamp.Message
			select {
			case rsp = <-callee[i].Recv():
			case <-time.After(200 * time.Millisecond):
				require.FailNowf(t, "timed out waiting for response", "callee[%d]", i)
			}
			switch rsp := rsp.(type) {
			case *wamp.Registered:
				// Tests that Dealer is using router scoped IDs for proc reg
				require.Equal(t, wamp.ID(routerScopeID),
					rsp.Registration)
				routerScopeID++
			case *wamp.Error:
				require.FailNowf(t, "error registering procedure",
					"callee[%d] err=%v", i, rsp)
			default:
				require.FailNowf(t, "did not receive REGISTERED response",
					"callee[%d] got=%v", i, rsp.MessageType())

			}
		}
	}

	// Create a Callers to test invocation of each procedure
	const numCallers = 2
	var caller [numCallers]*testPeer
	var callerSess [numCallers]*wamp.Session
	for i := range numCallers {
		caller[i] = newTestPeer()
		callerSess[i] = wamp.NewSession(caller[i], wamp.ID(2240+i), nil, nil)
	}

	// Call procName and ensure callee got expectedID for INVOCATION.request
	callAndCheckInvocationRequestID := func(callerIdx int, calleeSess *wamp.Session, procName wamp.URI, expectedID int) {
		t.Log("calling", procName)
		dealer.call(
			callerSess[callerIdx],
			&wamp.Call{Request: callerSess[callerIdx].IDGen.Next(), Procedure: procName})

		rsp, err := wamp.RecvTimeout(calleeSess, time.Second)
		require.NoError(t, err)
		werr, ok := rsp.(*wamp.Error)
		require.Falsef(t, ok, "unexpected error from callee %v", werr)

		inv, ok := rsp.(*wamp.Invocation)
		require.Truef(t, ok, "expected INVOCATION; Got: %s", rsp.MessageType().String())
		require.Equalf(t, wamp.ID(expectedID), inv.Request, "invocation request ID should be %d", expectedID)
	}

	// Caller 0 invoke Callee 0 and 1 with both procedures
	callAndCheckInvocationRequestID(0, calleeSess[1], procNames[1][0], 1)
	callAndCheckInvocationRequestID(0, calleeSess[1], procNames[1][1], 2)
	callAndCheckInvocationRequestID(0, calleeSess[0], procNames[0][0], 1)
	callAndCheckInvocationRequestID(0, calleeSess[0], procNames[0][1], 2)

	callAndCheckInvocationRequestID(0, calleeSess[1], procNames[1][1], 3)
	callAndCheckInvocationRequestID(0, calleeSess[0], procNames[0][0], 3)

	// Caller 1 invoke Callee 0 with both procedures
	// Because it is the same Callee (even with different Caller) the IDs
	// continue to increment
	callAndCheckInvocationRequestID(1, calleeSess[0], procNames[0][1], 4)
	callAndCheckInvocationRequestID(1, calleeSess[0], procNames[0][0], 5)

	// Weave Callers on Callee 1
	callAndCheckInvocationRequestID(1, calleeSess[1], procNames[1][1], 4)
	callAndCheckInvocationRequestID(0, calleeSess[1], procNames[1][0], 5)
	callAndCheckInvocationRequestID(1, calleeSess[1], procNames[1][0], 6)
	callAndCheckInvocationRequestID(0, calleeSess[1], procNames[1][1], 7)
}

func TestRemovePeer(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

	// Register a procedure.
	callee := newTestPeer()
	sess := wamp.NewSession(callee, 0, nil, nil)
	msg := &wamp.Register{Request: 123, Procedure: testProcedure}
	dealer.register(sess, msg)
	rsp := <-callee.Recv()
	regID := rsp.(*wamp.Registered).Registration

	_, ok := dealer.procRegMap[testProcedure]
	require.True(t, ok, "dealer does not have registered procedure")
	_, ok = dealer.registrations[regID]
	require.True(t, ok, "dealer does not have registration")

	checkMetaReg(t, metaClient, sess.ID)
	checkMetaReg(t, metaClient, sess.ID)

	// Test that removing the callee session removes the registration.
	dealer.removeSession(sess, true)

	// Register as a way to sync with dealer.
	sess2 := wamp.NewSession(callee, 0, nil, nil)
	dealer.register(sess2,
		&wamp.Register{Request: 789, Procedure: wamp.URI("nexus.test.p2")})
	<-callee.Recv()

	_, ok = dealer.procRegMap[testProcedure]
	require.False(t, ok, "dealer still has registered procedure")
	_, ok = dealer.registrations[regID]
	require.False(t, ok, "dealer still has registration")

	// Tests that registering the callee again succeeds.
	msg.Request = 124
	dealer.register(sess, msg)
	rsp = <-callee.Recv()
	require.Equal(t, wamp.REGISTERED, rsp.MessageType())
}

func TestCancelOnCalleeGone(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

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
	require.True(t, ok, "did not receive REGISTERED response")

	checkMetaReg(t, metaClient, calleeSess.ID)

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	_, ok = rsp.(*wamp.Invocation)
	require.True(t, ok, "expected INVOCATION")

	callee.Close()
	dealer.removeSession(calleeSess, true)

	// Check that caller receives the ERROR message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Error)
	require.True(t, ok, "expected ERROR")
	require.Equal(t, wamp.ErrCanceled, rslt.Error)
	require.NotZero(t, len(rslt.Arguments), "expected response argument")
	s, _ := wamp.AsString(rslt.Arguments[0])
	require.Equal(t, "callee gone", s, "Did not get error message from caller")
}

// ----- WAMP v.2 Testing -----

func TestCallTimeoutOnRouter(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

	callTimeout := 800

	// Register a procedure.
	callee := newTestPeer()
	calleeSess := wamp.NewSession(callee, 0, nil, nil)
	dealer.register(calleeSess,
		&wamp.Register{Request: 123, Procedure: testProcedure})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	require.True(t, ok, "did not receive REGISTERED response")

	checkMetaReg(t, metaClient, calleeSess.ID)

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Options: wamp.Dict{wamp.OptTimeout: callTimeout}, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	invk, ok := rsp.(*wamp.Invocation)
	require.True(t, ok, "expected INVOCATION")
	_, ok = invk.Details[wamp.OptTimeout]
	require.False(t, ok, "Invocation should not include timeout option")

	select {
	case <-time.After(time.Duration(callTimeout*2) * time.Millisecond):
		require.FailNow(t, "caller not received error message")
	case rsp = <-caller.Recv():
	}

	// Check that caller receives the ERROR message.
	rslt, ok := rsp.(*wamp.Error)
	require.True(t, ok, "expected ERROR")
	require.Equal(t, wamp.ErrTimeout, rslt.Error)
	require.NotZero(t, len(rslt.Arguments), "expected response argument")
	s, _ := wamp.AsString(rslt.Arguments[0])
	require.Equal(t, "call timeout", s, "Did not get error message from caller")
}

func TestCallTimeoutOnClient(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

	callTimeout := 800

	calleeRoles := wamp.Dict{
		"roles": wamp.Dict{
			"callee": wamp.Dict{
				"features": wamp.Dict{
					"call_canceling": true,
					"call_timeout":   true,
				},
			},
		},
	}

	// Register a procedure.
	callee := newTestPeer()
	calleeSess := wamp.NewSession(callee, 0, nil, calleeRoles)
	dealer.register(calleeSess,
		&wamp.Register{Request: 123, Options: wamp.Dict{wamp.OptForwardTimeout: true}, Procedure: testProcedure})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	require.True(t, ok, "did not receive REGISTERED response")

	checkMetaReg(t, metaClient, calleeSess.ID)

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Options: wamp.Dict{wamp.OptTimeout: callTimeout}, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	invk, ok := rsp.(*wamp.Invocation)
	require.True(t, ok, "expected INVOCATION")
	timeOutOpt, ok := invk.Details[wamp.OptTimeout]
	require.True(t, ok, "Invocation should include timeout option")
	require.Equal(t, int64(callTimeout), timeOutOpt)

	errMsg := &wamp.Error{
		Type:      wamp.INVOCATION,
		Request:   invk.Request,
		Details:   wamp.Dict{},
		Error:     wamp.ErrTimeout,
		Arguments: wamp.List{"call timeout"},
	}
	dealer.error(calleeSess, errMsg)

	rsp = <-caller.Recv()

	// Check that caller receives the ERROR message.
	rslt, ok := rsp.(*wamp.Error)
	require.True(t, ok, "expected ERROR")
	require.Equal(t, wamp.ErrTimeout, rslt.Error)
	require.NotZero(t, len(rslt.Arguments), "expected response argument")
	s, _ := wamp.AsString(rslt.Arguments[0])
	require.Equal(t, "call timeout", s, "Did not get error message from caller")
}

func TestCancelCallModeKill(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

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
	require.True(t, ok, "did not receive REGISTERED response")

	checkMetaReg(t, metaClient, calleeSess.ID)

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	require.True(t, ok, "expected INVOCATION")

	// Test caller cancelling call. mode=kill
	opts := wamp.SetOption(nil, "mode", "kill")
	dealer.cancel(callerSession, &wamp.Cancel{Request: 125, Options: opts})

	// callee should receive an INTERRUPT request
	rsp = <-callee.Recv()
	interrupt, ok := rsp.(*wamp.Interrupt)
	require.True(t, ok, "callee expected INTERRUPT")
	require.Equal(t, inv.Request, interrupt.Request, "INTERRUPT request ID does not match INVOCATION request ID")

	// callee responds with ERROR message
	dealer.error(calleeSess, &wamp.Error{
		Type:    wamp.INVOCATION,
		Request: inv.Request,
		Error:   wamp.ErrCanceled,
		Details: wamp.Dict{"reason": "callee canceled"},
	})

	// Check that caller receives the ERROR message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Error)
	require.True(t, ok, "expected ERROR")
	require.Equal(t, wamp.ErrCanceled, rslt.Error)
	require.NotZero(t, len(rslt.Details), "expected details in message")
	s, _ := wamp.AsString(rslt.Details["reason"])
	require.Equal(t, "callee canceled", s, "Did not get error message from caller")
}

func TestCancelCallModeKillNoWait(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

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
	require.True(t, ok, "did not receive REGISTERED response")

	checkMetaReg(t, metaClient, calleeSess.ID)

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	require.True(t, ok, "expected INVOCATION")

	// Test caller cancelling call. mode=kill
	opts := wamp.SetOption(nil, "mode", "killnowait")
	dealer.cancel(callerSession, &wamp.Cancel{Request: 125, Options: opts})

	// callee should receive an INTERRUPT request
	rsp = <-callee.Recv()
	interrupt, ok := rsp.(*wamp.Interrupt)
	require.True(t, ok, "callee expected INTERRUPT")
	require.Equal(t, inv.Request, interrupt.Request, "INTERRUPT request ID does not match INVOCATION request ID")

	// callee responds with ERROR message
	dealer.error(calleeSess, &wamp.Error{
		Type:    wamp.INVOCATION,
		Request: inv.Request,
		Error:   wamp.ErrCanceled,
		Details: wamp.Dict{"reason": "callee canceled"},
	})

	// Check that caller receives the ERROR message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Error)
	require.True(t, ok, "expected ERROR")
	require.Equal(t, wamp.ErrCanceled, rslt.Error)
	require.Zero(t, len(rslt.Details), "should not have details; result should not be from callee")
}

func TestCancelCallModeSkip(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dealer, metaClient := newTestDealer(t)

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
		require.True(t, ok, "did not receive REGISTERED response")

		checkMetaReg(t, metaClient, calleeSess.ID)

		caller := newTestPeer()
		callerSession := wamp.NewSession(caller, 0, nil, nil)

		// Test calling valid procedure
		dealer.call(callerSession,
			&wamp.Call{Request: 125, Procedure: testProcedure})

		// Test that callee received an INVOCATION message.
		rsp = <-callee.Recv()
		_, ok = rsp.(*wamp.Invocation)
		require.True(t, ok, "expected INVOCATION")

		// Test caller cancelling call. mode=kill
		opts := wamp.SetOption(nil, "mode", "skip")
		dealer.cancel(callerSession, &wamp.Cancel{Request: 125, Options: opts})

		// callee should NOT receive an INTERRUPT request
		synctest.Wait()
		select {
		default:
		case <-callee.Recv():
			require.FailNow(t, "callee received unexpected message")
		}

		// Check that caller receives the ERROR message.
		rsp = <-caller.Recv()
		rslt, ok := rsp.(*wamp.Error)
		require.True(t, ok, "expected ERROR")
		require.Equal(t, wamp.ErrCanceled, rslt.Error)
	})
}

func TestSharedRegistrationRoundRobin(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

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
	require.True(t, ok, "did not receive REGISTERED response")
	regID1 := regMsg.Registration
	checkMetaReg(t, metaClient, calleeSess1.ID)
	checkMetaReg(t, metaClient, calleeSess1.ID)

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
	require.True(t, ok, "did not receive REGISTERED response")
	checkMetaReg(t, metaClient, calleeSess2.ID)
	regID2 := regMsg.Registration

	require.Equal(t, regID2, regID1, "procedures should have same registration")

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
		require.True(t, ok, "expected INVOCATION")
	case <-callee2.Recv():
		require.FailNow(t, "should not have received from callee2")
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess1, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, wamp.ID(125), rslt.Request, "wrong request ID in RESULT")

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 126, Procedure: testProcedure})

	// Test that callee2 received an INVOCATION message.
	select {
	case rsp = <-callee2.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		require.True(t, ok, "expected INVOCATION")
	case <-callee1.Recv():
		require.FailNow(t, "should not have received from callee1")
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess2, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok = rsp.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, wamp.ID(126), rslt.Request, "wrong request ID in RESULT")
}

func TestSharedRegistrationFirst(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

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
	require.True(t, ok, "did not receive REGISTERED response")
	regID1 := regMsg.Registration
	checkMetaReg(t, metaClient, calleeSess1.ID)
	checkMetaReg(t, metaClient, calleeSess1.ID)

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
	require.True(t, ok, "expected ERROR response")

	// Register callee2 with "first" shared registration
	dealer.register(calleeSess2, &wamp.Register{
		Request:   124,
		Procedure: testProcedure,
		Options:   wamp.SetOption(nil, "invoke", "first"),
	})
	select {
	case rsp = <-callee2.Recv():
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for REGISTERED")
	}

	regMsg, ok = rsp.(*wamp.Registered)
	require.True(t, ok, "did not receive REGISTERED response")
	regID2 := regMsg.Registration
	checkMetaReg(t, metaClient, calleeSess2.ID)

	require.Equal(t, regID2, regID1, "procedures should have same registration")

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
		require.True(t, ok, "expected INVOCATION")
	case rsp = <-callee2.Recv():
		require.FailNow(t, "should not have received from callee2")
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for INVOCATION")
	}

	// Callee1 responds with a YIELD message
	dealer.yield(calleeSess1, &wamp.Yield{Request: inv.Request})

	// Check that caller received a RESULT message.
	select {
	case rsp = <-caller.Recv():
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for RESULT")
	}
	rslt, ok := rsp.(*wamp.Result)
	if !ok {
		require.FailNow(t, "expected RESULT")
	}
	if rslt.Request != 125 {
		require.FailNow(t, "wrong request ID in RESULT")
	}

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 126, Procedure: testProcedure})

	// Test that callee1 received an INVOCATION message.
	select {
	case rsp = <-callee1.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		require.True(t, ok, "expected INVOCATION")
	case rsp = <-callee2.Recv():
		require.FailNow(t, "should not have received from callee2")
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess1, &wamp.Yield{Request: inv.Request})

	// Check that caller received a RESULT message.
	select {
	case rsp = <-caller.Recv():
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for RESULT")
	}
	rslt, ok = rsp.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, wamp.ID(126), rslt.Request, "wrong request ID in RESULT")

	// Remove callee1
	dealer.removeSession(calleeSess1, true)
	checkMetaReg(t, metaClient, calleeSess1.ID)

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 127, Procedure: testProcedure})

	// Test that callee2 received an INVOCATION message.
	select {
	case rsp = <-callee2.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		require.True(t, ok, "expected INVOCATION")
	case rsp = <-callee1.Recv():
		require.FailNow(t, "should not have received from callee1")
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess2, &wamp.Yield{Request: inv.Request})

	// Check that caller received a RESULT message.
	select {
	case rsp = <-caller.Recv():
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for RESULT")
	}

	rslt, ok = rsp.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, wamp.ID(127), rslt.Request, "wrong request ID in RESULT")
}

func TestSharedRegistrationLast(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

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
	require.True(t, ok, "did not receive REGISTERED response")
	checkMetaReg(t, metaClient, calleeSess1.ID)
	checkMetaReg(t, metaClient, calleeSess1.ID)

	// Register callee2 with last shared registration
	callee2 := newTestPeer()
	calleeSess2 := wamp.NewSession(callee2, 0, nil, calleeRoles)
	dealer.register(calleeSess2, &wamp.Register{
		Request:   124,
		Procedure: testProcedure,
		Options:   wamp.SetOption(nil, "invoke", "last"),
	})
	rsp = <-callee2.Recv()
	_, ok = rsp.(*wamp.Registered)
	require.True(t, ok, "did not receive REGISTERED response")
	checkMetaReg(t, metaClient, calleeSess2.ID)

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
		require.True(t, ok, "expected INVOCATION")
	case <-callee1.Recv():
		require.FailNow(t, "should not have received from callee1")
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess2, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, wamp.ID(125), rslt.Request, "wrong request ID in RESULT")

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 126, Procedure: testProcedure})

	// Test that callee2 received an INVOCATION message.
	select {
	case rsp = <-callee2.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		require.True(t, ok, "expected INVOCATION")
	case <-callee1.Recv():
		require.FailNow(t, "should not have received from callee1")
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess2, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok = rsp.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, wamp.ID(126), rslt.Request, "wrong request ID in RESULT")

	// Remove callee2
	dealer.removeSession(calleeSess2, true)
	checkMetaReg(t, metaClient, calleeSess2.ID)

	// Test calling valid procedure
	dealer.call(callerSession,
		&wamp.Call{Request: 127, Procedure: testProcedure})

	// Test that callee1 received an INVOCATION message.
	select {
	case rsp = <-callee1.Recv():
		inv, ok = rsp.(*wamp.Invocation)
		require.True(t, ok, "expected INVOCATION")
	case <-callee2.Recv():
		require.FailNow(t, "should not have received from callee2")
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for INVOCATION")
	}

	// Callee responds with a YIELD message
	dealer.yield(calleeSess1, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok = rsp.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, wamp.ID(127), rslt.Request, "wrong request ID in RESULT")
}

func TestPatternBasedRegistration(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

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
	require.True(t, ok, "did not receive REGISTERED response")
	checkMetaReg(t, metaClient, calleeSess.ID)
	checkMetaReg(t, metaClient, calleeSess.ID)

	caller := newTestPeer()
	callerSession := wamp.NewSession(caller, 0, nil, nil)

	// Test calling valid procedure with full name. Wildcard should match.
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	require.True(t, ok, "expected INVOCATION")
	details, ok := wamp.AsDict(inv.Details)
	require.True(t, ok, "INVOCATION missing details")
	proc, _ := wamp.AsURI(details[wamp.OptProcedure])
	require.Equal(t, testProcedure, proc, "INVOCATION has missing or incorrect procedure detail")

	// Callee responds with a YIELD message
	dealer.yield(calleeSess, &wamp.Yield{Request: inv.Request})
	// Check that caller received a RESULT message.
	rsp = <-caller.Recv()
	rslt, ok := rsp.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, wamp.ID(125), rslt.Request, "wrong request ID in RESULT")
}

func TestRPCBlockedUnresponsiveCallee(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

	// Register a procedure.
	callee, rtr := transport.LinkedPeers()
	calleeSess := wamp.NewSession(rtr, 0, nil, nil)
	opts := wamp.Dict{}
	dealer.register(calleeSess,
		&wamp.Register{Request: 223, Procedure: testProcedure, Options: opts})
	rsp := <-callee.Recv()
	_, ok := rsp.(*wamp.Registered)
	require.True(t, ok, "did not receive REGISTERED response")

	checkMetaReg(t, metaClient, calleeSess.ID)

	caller, rtr := transport.LinkedPeers()
	callerSession := wamp.NewSession(rtr, 0, nil, nil)

	// Call unresponsive callee until call dropped.
	var i int
sendLoop:
	for {
		i++
		t.Log("Calling", i)
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
	require.True(t, ok, "expected ERROR")
	require.Equal(t, wamp.ErrNetworkFailure, rslt.Error)
}

func TestCallerIdentification(t *testing.T) {
	// Test disclose_caller
	// Test disclose_me
	dealer, metaClient := newTestDealer(t)

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
	require.True(t, ok, "did not receive REGISTERED response")
	checkMetaReg(t, metaClient, calleeSess.ID)
	checkMetaReg(t, metaClient, calleeSess.ID)

	caller := newTestPeer()
	callerID := wamp.ID(11235813)
	callerSession := wamp.NewSession(caller, callerID, nil, nil)

	// Test calling valid procedure with full name. Wildcard should match.
	dealer.call(callerSession,
		&wamp.Call{Request: 125, Procedure: testProcedure})

	// Test that callee received an INVOCATION message.
	rsp = <-callee.Recv()
	inv, ok := rsp.(*wamp.Invocation)
	require.True(t, ok, "expected INVOCATION")

	// Test that invocation contains caller ID.

	id, _ := wamp.AsID(inv.Details["caller"])
	require.Equal(t, callerID, id, "Did not get expected caller ID. Invocation.Details")
}

func TestWrongYielder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dealer := newDealer(logger, false, true, debug)
		t.Cleanup(func() {
			dealer.close()
		})

		// Register a procedure.
		callee := newTestPeer()
		calleeSess := wamp.NewSession(callee, 7777, nil, nil)
		dealer.register(calleeSess,
			&wamp.Register{Request: 4321, Procedure: testProcedure})
		rsp := <-callee.Recv()
		_, ok := rsp.(*wamp.Registered)
		require.True(t, ok, "did not receive REGISTERED response")

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
		require.True(t, ok, "expected INVOCATION")

		// Imposter callee responds with a YIELD message
		dealer.yield(badCalleeSess, &wamp.Yield{Request: inv.Request})

		// Check that caller did not received a RESULT message.
		synctest.Wait()
		select {
		case <-caller.Recv():
			require.FailNow(t, "Caller received response from imposter callee")
		default:
		}
	})
}

// TestRegisterUndeliverableAckCreatesNoRegistration pins the REGISTERED-ack
// integrity invariant: when the ack cannot even be queued on the callee's
// outbound channel, the dealer must not create the registration. A
// registration the callee never learned about receives INVOCATIONs for an
// unknown registration ID, which is a protocol violation that makes some
// clients (autobahn-js) drop the whole connection, re-triggering the register
// burst that overflowed the queue in the first place.
func TestRegisterUndeliverableAckCreatesNoRegistration(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

	// A callee whose single-slot outbound queue is already occupied: the
	// REGISTERED ack cannot be queued.
	blocked := newTestPeer()
	blocked.Send() <- &wamp.Hello{}
	blockedSess := wamp.NewSession(blocked, 0, nil, nil)
	dealer.register(blockedSess, &wamp.Register{Request: 1, Procedure: testProcedure})

	// Dealer-side state must not exist.
	_, ok := dealer.procRegMap[testProcedure]
	require.False(t, ok, "no registration may exist when the REGISTERED ack was dropped")
	_, ok = dealer.calleeRegIDSet[blockedSess]
	require.False(t, ok, "failed register must not be tracked against the callee")

	// The callee's queue still holds only the pre-filled message.
	require.IsType(t, &wamp.Hello{}, <-blocked.Recv())
	select {
	case msg := <-blocked.Recv():
		require.FailNow(t, "blocked callee must not receive anything", "got %T", msg)
	default:
	}

	// The procedure remains registrable by a healthy callee and no ghost
	// single-policy registration blocks it. The first meta events fired belong to
	// that callee (a failed register fires none; checkMetaReg asserts the session
	// ID).
	callee := newTestPeer()
	sess := wamp.NewSession(callee, 0, nil, nil)
	dealer.register(sess, &wamp.Register{Request: 2, Procedure: testProcedure})
	_, ok = (<-callee.Recv()).(*wamp.Registered)
	require.True(t, ok, "procedure must be registrable after the failed register")
	checkMetaReg(t, metaClient, sess.ID) // on_create
	checkMetaReg(t, metaClient, sess.ID) // on_register
}

// TestRegisterSharedJoinUndeliverableAckNotAdded is the shared-registration
// variant of the ack-integrity invariant: a callee joining an existing shared
// registration whose REGISTERED ack cannot be queued must not be added as a
// callee (it would receive round-robin INVOCATIONs for a registration ID it
// never learned).
func TestRegisterSharedJoinUndeliverableAckNotAdded(t *testing.T) {
	dealer, metaClient := newTestDealer(t)

	calleeRoles := wamp.Dict{
		"roles": wamp.Dict{
			"callee": wamp.Dict{
				"features": wamp.Dict{"shared_registration": true},
			},
		},
	}
	c1 := newTestPeer()
	s1 := wamp.NewSession(c1, 0, nil, calleeRoles)
	dealer.register(s1, &wamp.Register{
		Request:   1,
		Procedure: testProcedure,
		Options:   wamp.SetOption(nil, wamp.OptInvoke, wamp.InvokeRoundRobin),
	})
	regID := (<-c1.Recv()).(*wamp.Registered).Registration
	checkMetaReg(t, metaClient, s1.ID) // on_create
	checkMetaReg(t, metaClient, s1.ID) // on_register

	blocked := newTestPeer()
	blocked.Send() <- &wamp.Hello{}
	s2 := wamp.NewSession(blocked, 0, nil, calleeRoles)
	dealer.register(s2, &wamp.Register{
		Request:   2,
		Procedure: testProcedure,
		Options:   wamp.SetOption(nil, wamp.OptInvoke, wamp.InvokeRoundRobin),
	})

	reg, ok := dealer.registrations[regID]
	require.True(t, ok, "existing shared registration must remain")
	require.Equal(t, 1, len(reg.callees), "callee with dropped ack must not join the registration")
	require.Same(t, s1, reg.callees[0])
	_, ok = dealer.calleeRegIDSet[s2]
	require.False(t, ok, "failed join must not be tracked against the callee")
}

// setupBlockedCall registers testProcedure and calls it from a caller whose
// one-message queue is never read by the dealer's goroutine. It returns the
// callee session, the caller session, and the INVOCATION for the call.
func setupBlockedCall(t *testing.T, d *dealer) (*wamp.Session, *wamp.Session, *wamp.Invocation) {
	t.Helper()
	callee := wamp.NewSession(newTestPeer(), 0, nil, nil)
	d.register(callee, &wamp.Register{Request: 1, Procedure: testProcedure})
	_, ok := (<-callee.Recv()).(*wamp.Registered)
	require.True(t, ok, "expected REGISTERED")

	caller := wamp.NewSession(newTestPeer(), 0, nil, nil)
	d.call(caller, &wamp.Call{Request: 100, Procedure: testProcedure})
	inv, ok := (<-callee.Recv()).(*wamp.Invocation)
	require.True(t, ok, "expected INVOCATION")
	return callee, caller, inv
}

func progressYield(inv *wamp.Invocation, arg int) *wamp.Yield {
	return &wamp.Yield{
		Request:   inv.Request,
		Options:   wamp.Dict{wamp.OptProgress: true},
		Arguments: wamp.List{arg},
	}
}

// requireResult fails if the next message for caller is not a RESULT with the
// argument arg.
func requireResult(t *testing.T, caller *wamp.Session, arg int) *wamp.Result {
	t.Helper()
	select {
	case msg := <-caller.Recv():
		res, ok := msg.(*wamp.Result)
		require.True(t, ok, "expected RESULT %d, got %T", arg, msg)
		require.Equal(t, arg, res.Arguments[0], "RESULT out of order")
		return res
	case <-time.After(2 * time.Second):
		require.FailNowf(t, "timed out", "RESULT %d not delivered", arg)
		return nil
	}
}

// TestDealerYieldRetriesOnSlowCaller checks that a YIELD whose RESULT cannot
// be sent to a blocked caller is queued, and is delivered once the caller
// reads its queue.
func TestDealerYieldRetriesOnSlowCaller(t *testing.T) {
	d, _ := newTestDealer(t)
	callee, caller, inv := setupBlockedCall(t, d)

	// The first RESULT fills the caller's queue, and the second is queued.
	d.yield(callee, progressYield(inv, 1))
	d.yield(callee, progressYield(inv, 2))

	requireResult(t, caller, 1)
	requireResult(t, caller, 2)

	// A final YIELD ends the call.
	d.yield(callee, &wamp.Yield{Request: inv.Request, Arguments: wamp.List{3}})
	res := requireResult(t, caller, 3)
	_, hasProgress := res.Details[wamp.OptProgress]
	require.False(t, hasProgress, "final RESULT must not have progress")
}

// TestDealerYieldRetriesPreservesOrder checks that queued YIELDs are delivered
// in the order they arrived.
func TestDealerYieldRetriesPreservesOrder(t *testing.T) {
	d, _ := newTestDealer(t)
	callee, caller, inv := setupBlockedCall(t, d)

	const n = 5
	for i := 1; i <= n; i++ {
		d.yield(callee, progressYield(inv, i))
	}
	for i := 1; i <= n; i++ {
		requireResult(t, caller, i)
	}
}

// TestDealerYieldQueueBounded checks that once maxPendingYields are queued for
// a call, yield waits for room instead of queuing more, and that all YIELDs
// are delivered, in order, once the caller reads them.
func TestDealerYieldQueueBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, _ := newTestDealer(t)
		callee, caller, inv := setupBlockedCall(t, d)

		// One RESULT fills the caller's queue, then maxPendingYields are
		// queued, and the next yield waits.
		const n = maxPendingYields + 3
		var sent atomic.Int32
		go func() {
			for i := 1; i <= n; i++ {
				d.yield(callee, progressYield(inv, i))
				sent.Add(1)
			}
		}()
		time.Sleep(time.Second)
		synctest.Wait()
		require.Equal(t, int32(maxPendingYields+1), sent.Load(), "yields not held back")
		queued := make(chan int)
		d.actionChan <- func() {
			queued <- len(d.invocations[requestID{callee.ID, inv.Request}].pendingYields)
		}
		require.Equal(t, maxPendingYields, <-queued)

		for i := 1; i <= n; i++ {
			requireResult(t, caller, i)
		}
		synctest.Wait()
		require.Equal(t, int32(n), sent.Load())
	})
}

// TestDealerYieldDeadlineFromLastProgress checks that a caller that reads its
// results slowly but steadily gets every result of a stream that takes longer
// than sendResultDeadline. The deadline only cancels a call whose caller
// reads nothing for that long.
func TestDealerYieldDeadlineFromLastProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, _ := newTestDealer(t)
		callee, caller, inv := setupBlockedCall(t, d)

		const n = 1000
		go func() {
			for i := 1; i <= n; i++ {
				d.yield(callee, progressYield(inv, i))
			}
		}()
		// Reading one result every 100ms takes 100s, longer than the deadline.
		for i := 1; i <= n; i++ {
			time.Sleep(100 * time.Millisecond)
			requireResult(t, caller, i)
		}
	})
}

// TestDealerYieldDeadlineCancels checks that a call whose caller reads none of
// its queued results until sendResultDeadline is canceled, and that the callee
// is told to stop.
func TestDealerYieldDeadlineCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, _ := newTestDealer(t)
		calleeRoles := wamp.Dict{"roles": wamp.Dict{"callee": wamp.Dict{
			"features": wamp.Dict{wamp.FeatureCallCanceling: true},
		}}}
		callee := wamp.NewSession(newTestPeer(), 0, nil, calleeRoles)
		d.register(callee, &wamp.Register{Request: 1, Procedure: testProcedure})
		<-callee.Recv()
		caller := wamp.NewSession(newTestPeer(), 0, nil, nil)
		d.call(caller, &wamp.Call{Request: 100, Procedure: testProcedure})
		inv := (<-callee.Recv()).(*wamp.Invocation)

		d.yield(callee, progressYield(inv, 1))
		d.yield(callee, progressYield(inv, 2))

		time.Sleep(sendResultDeadline + time.Second)
		synctest.Wait()
		msg := <-callee.Recv()
		_, ok := msg.(*wamp.Interrupt)
		require.True(t, ok, "expected INTERRUPT, got %T", msg)
		found := make(chan bool)
		d.actionChan <- func() {
			_, ok := d.invocations[requestID{callee.ID, inv.Request}]
			found <- ok
		}
		require.False(t, <-found, "invocation not removed")
	})
}

// TestDealerDrainExitsOnInvocationCancel checks that canceling a call whose
// YIELDs are queued drops them, and that a later YIELD for the call is not
// delivered.
func TestDealerDrainExitsOnInvocationCancel(t *testing.T) {
	d, _ := newTestDealer(t)
	callee, caller, inv := setupBlockedCall(t, d)

	d.yield(callee, progressYield(inv, 1))
	d.yield(callee, progressYield(inv, 2))

	d.cancel(caller, &wamp.Cancel{
		Request: 100,
		Options: wamp.Dict{wamp.OptMode: wamp.CancelModeKillNoWait},
	})
	requireResult(t, caller, 1)
	// The cancel ERROR, if there was room for it.
	select {
	case msg := <-caller.Recv():
		_, isResult := msg.(*wamp.Result)
		require.False(t, isResult, "queued RESULT delivered after cancel")
	case <-time.After(100 * time.Millisecond):
	}

	d.yield(callee, &wamp.Yield{Request: inv.Request, Arguments: wamp.List{3}})
	select {
	case msg := <-caller.Recv():
		require.FailNowf(t, "unexpected message", "got %T after cancel", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestDealerCloseWhileDeliveringYields checks that closing the dealer while
// queued YIELDs are being delivered neither panics, by sending on the closed
// action channel, nor leaves goroutines running.
func TestDealerCloseWhileDeliveringYields(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	for i := range 500 {
		d := newDealer(logger, false, true, debug)
		_, rtr := transport.LinkedPeers()
		d.setMetaPeer(rtr)
		callee, _, inv := setupBlockedCall(t, d)
		d.yield(callee, progressYield(inv, 1))
		d.yield(callee, progressYield(inv, 2))

		// Close at different points of the delivery goroutine's retries.
		time.Sleep(time.Duration(i%4) * 500 * time.Microsecond)
		d.close()
		rtr.Close()
	}
}
