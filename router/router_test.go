package router

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

const (
	testRealm       = wamp.URI("nexus.test.realm")
	testRealm2      = wamp.URI("nexus.test.realm2")
	testProcedure   = wamp.URI("nexus.test.endpoint")
	testProcedureWC = wamp.URI("nexus..endpoint")
	testTopic       = wamp.URI("nexus.test.event")
	testTopicPfx    = wamp.URI("nexus.test")
	testTopicWC     = wamp.URI("nexus..event")
)

var (
	debug  bool
	logger stdlog.StdLog
)

func init() {
	debug = false
	logger = log.New(os.Stdout, "", log.LstdFlags)
}

var clientRoles = wamp.Dict{
	"roles": wamp.Dict{
		"subscriber": wamp.Dict{
			"features": wamp.Dict{
				"publisher_identification": true,
			},
		},
		"publisher": wamp.Dict{
			"features": wamp.Dict{
				"subscriber_blackwhite_listing": true,
			},
		},
		"callee": wamp.Dict{},
		"caller": wamp.Dict{
			"features": wamp.Dict{
				"call_timeout": true,
			},
		},
	},
	"authmethods": wamp.List{"anonymous", "ticket"},
}

func checkGoLeaks(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
}

func newTestRouter(t *testing.T) Router {
	config := &Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:              testRealm,
				StrictURI:        false,
				AnonymousAuth:    true,
				AllowDisclose:    false,
				EnableMetaKill:   true,
				EnableMetaModify: true,
			},
		},
		Debug: debug,
	}

	r, err := NewRouter(config, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		r.Close()
	})
	return r
}

func testClientInRealm(t *testing.T, r Router, realm wamp.URI) *wamp.Session {
	client, server := transport.LinkedPeers()
	// Run as goroutine since Send will block until message read by router, if
	// client uses unbuffered channel.
	details := clientRoles
	details["authid"] = "user1"
	details["xyzzy"] = "plugh"
	//go client.Send(&wamp.Hello{Realm: realm, Details: clientRoles})
	go client.Send(&wamp.Hello{Realm: realm, Details: details})
	err := r.Attach(server)
	require.NoError(t, err)

	msg, err := wamp.RecvTimeout(client, time.Second)
	require.NoError(t, err, "error waiting for welcome")
	welcome, ok := msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME")

	return &wamp.Session{
		Peer:    client,
		ID:      welcome.ID,
		Details: welcome.Details,
	}
}

func testClient(t *testing.T, r Router) *wamp.Session {
	return testClientInRealm(t, r, testRealm)
}

func TestHandshake(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)

	cli := testClient(t, r)
	cli.Send(&wamp.Goodbye{})
	msg, err := wamp.RecvTimeout(cli, time.Second)
	require.NoError(t, err, "no goodbye message after sending goodbye")
	_, ok := msg.(*wamp.Goodbye)
	require.True(t, ok, "expected GOODBYE")
}

func TestHandshakeBadRealm(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)
	client, server := transport.LinkedPeers()
	go client.Send(&wamp.Hello{Realm: "does.not.exist"})
	err := r.Attach(server)
	require.Error(t, err)

	msg, err := wamp.RecvTimeout(client, time.Second)
	require.NoError(t, err, "timed out waiting for response to HELLO")
	_, ok := msg.(*wamp.Abort)
	require.True(t, ok, "Expected ABORT after bad handshake")
}

// Test for protocol violation.
func TestProtocolViolation(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)
	cli := testClient(t, r)

	// Send HELLO message after session established.
	cli.Send(&wamp.Hello{Realm: testRealm, Details: clientRoles})
	msg, err := wamp.RecvTimeout(cli, time.Second)
	require.NoError(t, err, "timed out waiting for ABORT")
	abort, ok := msg.(*wamp.Abort)
	require.True(t, ok, "expected ABORT")
	require.Equal(t, wamp.ErrProtocolViolation, abort.Reason)

	// Send SUBSCRIBE before session established.
	client, server := transport.LinkedPeers()
	// Run as goroutine since Send will block until message read by router, if
	// client uses unbuffered channel.
	go client.Send(&wamp.Subscribe{Request: wamp.GlobalID(), Topic: wamp.URI("some.uri")})
	err = r.Attach(server)
	require.Error(t, err)

	msg, err = wamp.RecvTimeout(client, time.Second)
	require.NoError(t, err, "timed out waiting for ABORT")
	abort, ok = msg.(*wamp.Abort)
	require.True(t, ok, "expected ABORT")
	require.Equal(t, wamp.ErrProtocolViolation, abort.Reason)
}

func TestRouterSubscribe(t *testing.T) {
	checkGoLeaks(t)

	r := newTestRouter(t)
	sub := testClient(t, r)

	subscribeID := wamp.GlobalID()
	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: testTopic})
	msg, err := wamp.RecvTimeout(sub, time.Second)
	require.NoError(t, err, "Timed out waiting for SUBSCRIBED")
	subMsg, ok := msg.(*wamp.Subscribed)
	require.True(t, ok, "Expected SUBSCRIBED")
	require.Equal(t, subscribeID, subMsg.Request, "wrong request ID")
	subscriptionID := subMsg.Subscription

	pub := testClient(t, r)
	pubID := wamp.GlobalID()
	pub.Send(&wamp.Publish{Request: pubID, Topic: testTopic})

	msg, err = wamp.RecvTimeout(sub, time.Second)
	require.NoError(t, err, "Timed out waiting for EVENT")
	event, ok := msg.(*wamp.Event)
	require.True(t, ok, "Expected EVENT")
	require.Equal(t, subscriptionID, event.Subscription, "wrong subscription ID")
}

func TestPublishAcknowledge(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)
	client := testClient(t, r)

	id := wamp.GlobalID()
	client.Send(&wamp.Publish{
		Request: id,
		Options: wamp.Dict{"acknowledge": true},
		Topic:   "some.uri"})

	msg, err := wamp.RecvTimeout(client, time.Second)
	require.NoError(t, err, "sent acknowledge=true, timed out waiting for PUBLISHED")
	pub, ok := msg.(*wamp.Published)
	require.Truef(t, ok, "sent acknowledge=true, expected PUBLISHED, got: %s", msg.MessageType())
	require.Equal(t, id, pub.Request, "wrong request id")
}

func TestPublishFalseAcknowledge(t *testing.T) {
	r := newTestRouter(t)
	client := testClient(t, r)

	id := wamp.GlobalID()
	client.Send(&wamp.Publish{
		Request: id,
		Options: wamp.Dict{"acknowledge": false},
		Topic:   "some.uri"})

	msg, err := wamp.RecvTimeout(client, 200*time.Millisecond)
	if err == nil {
		_, ok := msg.(*wamp.Published)
		require.Falsef(t, ok, "Sent acknowledge=false, but received PUBLISHED: %s", msg.MessageType())
	}
}

func TestPublishNoAcknowledge(t *testing.T) {
	r := newTestRouter(t)
	client := testClient(t, r)

	id := wamp.GlobalID()
	client.Send(&wamp.Publish{Request: id, Topic: "some.uri"})
	msg, err := wamp.RecvTimeout(client, 200*time.Millisecond)
	if err == nil {
		_, ok := msg.(*wamp.Published)
		require.Falsef(t, ok, "Sent acknowledge=false, but received PUBLISHED: %s", msg.MessageType())
	}
}

func TestRouterCall(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)
	callee := testClient(t, r)

	registerID := wamp.GlobalID()
	// Register remote procedure
	callee.Send(&wamp.Register{Request: registerID, Procedure: testProcedure})

	msg, err := wamp.RecvTimeout(callee, time.Second)
	require.NoError(t, err, "Timed out waiting for REGISTERED")
	registered, ok := msg.(*wamp.Registered)
	require.True(t, ok, "expected REGISTERED")
	require.Equal(t, registerID, registered.Request)
	registrationID := registered.Registration

	caller := testClient(t, r)
	callID := wamp.GlobalID()
	// Call remote procedure
	caller.Send(&wamp.Call{Request: callID, Procedure: testProcedure})

	msg, err = wamp.RecvTimeout(callee, time.Second)
	require.NoError(t, err, "Timed out waiting for INVOCATION")
	invocation, ok := msg.(*wamp.Invocation)
	require.True(t, ok, "expected INVOCATION")
	require.Equal(t, registrationID, invocation.Registration)
	invocationID := invocation.Request

	// Returns result of remove procedure
	callee.Send(&wamp.Yield{Request: invocationID})

	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok := msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request)
}

func TestSessionCountMetaProcedure(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)

	caller := testClient(t, r)

	// Call wamp.MetaProcSessionCount
	req := &wamp.Call{Request: wamp.GlobalID(), Procedure: wamp.MetaProcSessionCount}
	caller.Send(req)
	msg, err := wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err)
	result, ok := msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, req.Request, result.Request)
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	count, ok := result.Arguments[0].(int)
	require.True(t, ok, "expected int arguemnt")
	require.Equal(t, 1, count, "wrong session count")

	// Call wamp.MetaProcSessionCount with invalid argument
	req = &wamp.Call{
		Request:   wamp.GlobalID(),
		Procedure: wamp.MetaProcSessionCount,
		Arguments: wamp.List{"should-be-a-list"},
	}
	caller.Send(req)
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err)
	errResult, ok := msg.(*wamp.Error)
	require.True(t, ok, "expected ERROR")
	require.Equal(t, req.Request, errResult.Request)

	// Call wamp.MetaProcSessionCount with non-matching filter
	filter := wamp.List{"user", "def"}
	req = &wamp.Call{
		Request:   wamp.GlobalID(),
		Procedure: wamp.MetaProcSessionCount,
		Arguments: wamp.List{filter},
	}
	caller.Send(req)
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err)
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, req.Request, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	count = result.Arguments[0].(int)
	require.Zero(t, count, "wrong session count")

	// Call wamp.MetaProcSessionCount with matching filter
	filter = wamp.List{"trusted", "user", "def"}
	req = &wamp.Call{
		Request:   wamp.GlobalID(),
		Procedure: wamp.MetaProcSessionCount,
		Arguments: wamp.List{filter},
	}
	caller.Send(req)
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err)
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, req.Request, result.Request)
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	count = result.Arguments[0].(int)
	require.Equal(t, 1, count, "wrong session count")
}

func TestListSessionMetaProcedures(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)

	caller := testClient(t, r)
	sessID := caller.ID

	// Call wamp.MetaProcSessionList to get session list.
	req := &wamp.Call{
		Request:   wamp.GlobalID(),
		Procedure: wamp.MetaProcSessionList,
	}
	caller.Send(req)
	msg, err := wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err)
	result, ok := msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, req.Request, result.Request)
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	ids, ok := result.Arguments[0].([]wamp.ID)
	require.True(t, ok, "wrong arg type")
	require.Equal(t, 1, len(ids), "wrong number of session IDs")
	require.Equal(t, ids[0], sessID, "wrong session ID")

	// Call wamp.MetaProcSessionList with matching filter
	filter := wamp.List{"trusted"}
	req = &wamp.Call{
		Request:   wamp.GlobalID(),
		Procedure: wamp.MetaProcSessionList,
		Arguments: wamp.List{filter},
	}
	caller.Send(req)
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err)
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, req.Request, result.Request)
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	ids, ok = result.Arguments[0].([]wamp.ID)
	require.True(t, ok, "wrong arg type")
	require.Equal(t, 1, len(ids), "wrong number of session IDs")
	require.Equal(t, ids[0], sessID, "wrong session ID")
}

func TestGetSessionMetaProcedures(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)

	caller := testClient(t, r)
	sessID := caller.ID

	// Call session meta-procedure with bad session ID
	callID := wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSessionGet,
		Arguments: wamp.List{wamp.ID(123456789)},
	})
	msg, err := wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err)
	errRsp, ok := msg.(*wamp.Error)
	require.True(t, ok, "expected ERROR")
	require.Equal(t, wamp.ErrNoSuchSession, errRsp.Error, "wrong error value")

	// Call session meta-procedure to get session information.
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSessionGet,
		Arguments: wamp.List{sessID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err)
	result, ok := msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	details, ok := result.Arguments[0].(wamp.Dict)
	require.True(t, ok, "expected dict type arg")
	sid, _ := wamp.AsID(details["session"])
	require.Equal(t, sessID, sid, "wrong session ID")
}

func TestRegistrationMetaProcedures(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)
	caller := testClient(t, r)

	// ----- Test wamp.registration.list meta procedure -----
	callID := wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcRegList})
	msg, err := wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok := msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	dict, ok := result.Arguments[0].(wamp.Dict)
	require.True(t, ok, "expected wamp.Dict")
	exactPrev, ok := dict["exact"].([]wamp.ID)
	require.True(t, ok, "expected []wamp.ID")
	prefixPrev, ok := dict["prefix"].([]wamp.ID)
	require.True(t, ok, "expected []wamp.ID")
	wildcardPrev, ok := dict["wildcard"].([]wamp.ID)
	require.True(t, ok, "expected []wamp.ID")

	callee := testClient(t, r)
	sessID := callee.ID
	// Register remote procedure
	registerID := wamp.GlobalID()
	callee.Send(&wamp.Register{Request: registerID, Procedure: testProcedure})

	msg, err = wamp.RecvTimeout(callee, time.Second)
	require.NoError(t, err, "Timed out waiting for REGISTERED")
	registered, ok := msg.(*wamp.Registered)
	require.True(t, ok, "expected REGISTERED")
	require.Equal(t, registerID, registered.Request, "wrong request ID")
	registrationID := registered.Registration

	// Register remote procedure
	callee.Send(&wamp.Register{
		Request:   wamp.GlobalID(),
		Procedure: testProcedureWC,
		Options:   wamp.Dict{"match": "wildcard"},
	})
	msg = <-callee.Recv()
	_, ok = msg.(*wamp.Registered)
	require.True(t, ok, "expected REGISTERED")

	// Call session meta-procedure to get session count.
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcRegList})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	dict, ok = result.Arguments[0].(wamp.Dict)
	require.True(t, ok, "expected wamp.Dict")
	exact := dict["exact"].([]wamp.ID)
	prefix := dict["prefix"].([]wamp.ID)
	wildcard := dict["wildcard"].([]wamp.ID)

	require.Equal(t, len(exactPrev)+1, len(exact), "expected additional exact match")
	require.Equal(t, len(prefixPrev), len(prefix), "prefix matches should not have changed")
	require.Equal(t, len(wildcardPrev)+1, len(wildcard), "expected additional wildcard match")

	var found bool
	for i := range exact {
		if exact[i] == registrationID {
			found = true
			break
		}
	}
	require.True(t, found, "missing expected registration ID")

	// ----- Test wamp.registration.lookup meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcRegLookup,
		Arguments: wamp.List{testProcedure},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	regID, ok := result.Arguments[0].(wamp.ID)
	require.True(t, ok, "expected wamp.ID")
	require.Equal(t, registrationID, regID, "received wrong registration ID")

	// ----- Test wamp.registration.match meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcRegMatch,
		Arguments: wamp.List{testProcedure},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	regID, ok = wamp.AsID(result.Arguments[0])
	require.True(t, ok, "expected wamp.ID")
	require.Equal(t, registrationID, regID, "received wrong registration ID")

	// ----- Test wamp.registration.get meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcRegGet,
		Arguments: wamp.List{registrationID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	dict, ok = result.Arguments[0].(wamp.Dict)
	require.True(t, ok, "expected wamp.Dict")
	regID, _ = wamp.AsID(dict["id"])
	require.Equal(t, registrationID, regID, "received wrong registration")
	uri, _ := wamp.AsURI(dict["uri"])
	require.Equal(t, testProcedure, uri, "registration has wrong uri")

	// ----- Test wamp.registration.list_callees meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcRegListCallees,
		Arguments: wamp.List{registrationID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	idList, ok := result.Arguments[0].([]wamp.ID)
	require.True(t, ok, "Expected []wamp.ID")
	require.Equal(t, 1, len(idList), "Expected 1 callee in list")
	require.Equal(t, sessID, idList[0], "Wrong callee session ID")

	// ----- Test wamp.registration.count_callees meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcRegCountCallees,
		Arguments: wamp.List{registrationID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	count, ok := wamp.AsInt64(result.Arguments[0])
	require.True(t, ok, "Argument is not an int")
	require.Equal(t, int64(1), count, "Wring number of callees")
}

func TestSubscriptionMetaProcedures(t *testing.T) {
	checkGoLeaks(t)
	r := newTestRouter(t)
	caller := testClient(t, r)

	// ----- Test wamp.subscription.list meta procedure -----
	callID := wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcSubList})
	msg, err := wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok := msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	dict, ok := result.Arguments[0].(wamp.Dict)
	require.True(t, ok, "expected wamp.Dict")
	exactPrev, ok := dict["exact"].([]wamp.ID)
	require.True(t, ok, "expected []wamp.ID")
	prefixPrev, ok := dict["prefix"].([]wamp.ID)
	require.True(t, ok, "expected []wamp.ID")
	wildcardPrev, ok := dict["wildcard"].([]wamp.ID)
	require.True(t, ok, "expected []wamp.ID")

	subscriber := testClient(t, r)
	sessID := subscriber.ID
	// Subscribe to topic
	reqID := wamp.GlobalID()
	subscriber.Send(&wamp.Subscribe{Request: reqID, Topic: testTopic})
	msg, err = wamp.RecvTimeout(subscriber, time.Second)
	require.NoError(t, err, "Timed out waiting for SUBSCRIBED")
	subscribed, ok := msg.(*wamp.Subscribed)
	require.True(t, ok, "expected SUBSCRIBED")
	require.Equal(t, reqID, subscribed.Request, "wrong request ID")
	subscriptionID := subscribed.Subscription

	// Subscriber to wildcard topic
	subscriber.Send(&wamp.Subscribe{
		Request: wamp.GlobalID(),
		Topic:   testTopicWC,
		Options: wamp.Dict{"match": "wildcard"},
	})
	msg = <-subscriber.Recv()
	subscribed, ok = msg.(*wamp.Subscribed)
	require.True(t, ok, "expected SUBSCRIBED")
	wcSubscriptionID := subscribed.Subscription

	// Call subscription meta-procedure to get subscriptions.
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcSubList})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	dict, ok = result.Arguments[0].(wamp.Dict)
	require.True(t, ok, "expected wamp.Dict")
	exact := dict["exact"].([]wamp.ID)
	prefix := dict["prefix"].([]wamp.ID)
	wildcard := dict["wildcard"].([]wamp.ID)

	require.Equal(t, len(exactPrev)+1, len(exact), "expected additional exact match")
	require.Equal(t, len(prefixPrev), len(prefix), "prefix matches should not have changed")
	require.Equal(t, len(wildcardPrev)+1, len(wildcard), "expected additional wildcard match")

	var found bool
	for i := range exact {
		if exact[i] == subscriptionID {
			found = true
			break
		}
	}
	require.True(t, found, "missing expected subscription ID")

	// ----- Test wamp.subscription.lookup meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSubLookup,
		Arguments: wamp.List{testTopic},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	subID, ok := result.Arguments[0].(wamp.ID)
	require.True(t, ok, "expected wamp.ID")
	require.Equal(t, subscriptionID, subID, "received wrong subscription ID")

	// ----- Test wamp.subscription.match meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSubMatch,
		Arguments: wamp.List{testTopic},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	subIDs, ok := wamp.AsList(result.Arguments[0])
	require.True(t, ok, "expected wamp.List")
	require.Equal(t, 2, len(subIDs), "expected 2 subscriptions for wamp.subscription.match")
	require.Contains(t, subIDs, subscriptionID, "did not match subscription ID")
	require.Contains(t, subIDs, wcSubscriptionID, "did not match wildcard subscription ID")

	// ----- Test wamp.subscription.get meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSubGet,
		Arguments: wamp.List{subscriptionID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	dict, ok = result.Arguments[0].(wamp.Dict)
	require.True(t, ok, "expected wamp.Dict")
	subID, _ = wamp.AsID(dict["id"])
	require.Equal(t, subscriptionID, subID, "received wrong subscription")
	uri, _ := wamp.AsURI(dict["uri"])
	require.Equal(t, testTopic, uri, "subscription has wrong uri")

	// ----- Test wamp.subscription.list_subscribers meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSubListSubscribers,
		Arguments: wamp.List{subscriptionID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, len(result.Arguments), "missing expected arguemnt")
	idList, ok := result.Arguments[0].([]wamp.ID)
	require.True(t, ok, "Expected []wamp.ID")
	require.Equal(t, 1, len(idList), "Expected 1 subscriber in list")
	require.Equal(t, sessID, idList[0], "Wrong subscriber session ID")

	// ----- Test wamp.subscription.count_subscribers meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSubCountSubscribers,
		Arguments: wamp.List{subscriptionID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	require.NoError(t, err, "Timed out waiting for RESULT")
	result, ok = msg.(*wamp.Result)
	require.True(t, ok, "expected RESULT")
	require.Equal(t, callID, result.Request, "wrong result ID")
	require.NotZero(t, result.Arguments, "missing expected arguemnt")
	count, ok := wamp.AsInt64(result.Arguments[0])
	require.True(t, ok, "Argument is not an int")
	require.Equal(t, int64(1), count, "Wring number of subscribers")
}

func TestDynamicRealmChange(t *testing.T) {
	checkGoLeaks(t)

	dr := newTestRouter(t)
	err := dr.AddRealm(&RealmConfig{
		URI:           testRealm2,
		StrictURI:     false,
		AnonymousAuth: true,
		AllowDisclose: false,
	})
	require.NoError(t, err)

	cli := testClientInRealm(t, dr, testRealm2)
	cli.Send(&wamp.Goodbye{})
	msg, err := wamp.RecvTimeout(cli, time.Second)
	require.NoError(t, err, "no goodbye message after sending goodbye")
	_, ok := msg.(*wamp.Goodbye)
	require.True(t, ok, "expected GOODBYE")

	cli = testClientInRealm(t, dr, testRealm2)
	sync := make(chan wamp.Message)
	go func() {
		sync <- <-cli.Recv()
	}()

	dr.RemoveRealm(testRealm2)

	select {
	case <-time.After(time.Second):
		require.FailNow(t, "expected client to be booted when removing realm")
	case msg := <-sync:
		_, ok := msg.(*wamp.Goodbye)
		require.True(t, ok, "expected GOODBYE")
	}
}
