package router

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
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

func newTestRouter() (Router, error) {
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
	return NewRouter(config, logger)
}

func testClientInRealm(r Router, realm wamp.URI) (*wamp.Session, error) {
	client, server := transport.LinkedPeers()
	// Run as goroutine since Send will block until message read by router, if
	// client uses unbuffered channel.
	details := clientRoles
	details["authid"] = "user1"
	details["xyzzy"] = "plugh"
	//go client.Send(&wamp.Hello{Realm: realm, Details: clientRoles})
	go client.Send(&wamp.Hello{Realm: realm, Details: details})
	err := r.Attach(server)
	if err != nil {
		return nil, err
	}

	msg, err := wamp.RecvTimeout(client, time.Second)
	if err != nil {
		return nil, fmt.Errorf("error waiting for welcome: %s", err)
	}
	welcome, ok := msg.(*wamp.Welcome)
	if !ok {
		return nil, fmt.Errorf("expected %v, got %v", wamp.WELCOME,
			msg.MessageType())
	}

	return &wamp.Session{
		Peer:    client,
		ID:      welcome.ID,
		Details: welcome.Details,
	}, nil
}

func testClient(r Router) (*wamp.Session, error) {
	return testClientInRealm(r, testRealm)
}

func TestHandshake(t *testing.T) {
	defer leaktest.Check(t)()
	r, err := newTestRouter()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cli, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	cli.Send(&wamp.Goodbye{})
	msg, err := wamp.RecvTimeout(cli, time.Second)
	if err != nil {
		t.Fatal("no goodbye message after sending goodbye:", err)
	}
	if _, ok := msg.(*wamp.Goodbye); !ok {
		t.Fatal("expected GOODBYE, received:", msg.MessageType())
	}
}

func TestHandshakeBadRealm(t *testing.T) {
	defer leaktest.Check(t)()
	r, err := newTestRouter()
	if err != nil {
		t.Error(err)
	}
	defer r.Close()

	client, server := transport.LinkedPeers()
	go client.Send(&wamp.Hello{Realm: "does.not.exist"})
	err = r.Attach(server)
	if err == nil {
		t.Fatal("expected error")
	}

	msg, err := wamp.RecvTimeout(client, time.Second)
	if err != nil {
		t.Fatal("timed out waiting for response to HELLO")
	}
	if _, ok := msg.(*wamp.Abort); !ok {
		t.Error("Expected ABORT after bad handshake")
	}
}

// Test sending a
func TestProtocolViolation(t *testing.T) {
	defer leaktest.Check(t)()
	r, err := newTestRouter()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cli, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	// Send HELLO message after session established.
	cli.Send(&wamp.Hello{Realm: testRealm, Details: clientRoles})
	msg, err := wamp.RecvTimeout(cli, time.Second)
	if err != nil {
		t.Fatal("timed out waiting for ABORT")
	}
	abort, ok := msg.(*wamp.Abort)
	if !ok {
		t.Fatal("expected ABORT, received:", msg.MessageType())
	}
	if abort.Reason != wamp.ErrProtocolViolation {
		t.Fatal("Expected reason to be", wamp.ErrProtocolViolation)
	}

	// Send SUBSCRIBE before session established.
	client, server := transport.LinkedPeers()
	// Run as goroutine since Send will block until message read by router, if
	// client uses unbuffered channel.
	go client.Send(&wamp.Subscribe{Request: wamp.GlobalID(), Topic: wamp.URI("some.uri")})
	if err = r.Attach(server); err == nil {
		t.Fatal("Expected error from Attach")
	}

	msg, err = wamp.RecvTimeout(client, time.Second)
	if err != nil {
		t.Fatal("timed out waiting for ABORT")
	}
	abort, ok = msg.(*wamp.Abort)
	if !ok {
		t.Fatal("expected ABORT, received:", msg.MessageType())
	}
	if abort.Reason != wamp.ErrProtocolViolation {
		t.Fatal("Expected reason to be", wamp.ErrProtocolViolation)
	}
}

func TestRouterSubscribe(t *testing.T) {
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
	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: testTopic})
	msg, err := wamp.RecvTimeout(sub, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for SUBSCRIBED")
	}
	subMsg, ok := msg.(*wamp.Subscribed)
	if !ok {
		t.Fatal("Expected SUBSCRIBED, got:", msg.MessageType())
	}
	if subMsg.Request != subscribeID {
		t.Fatal("wrong request ID")
	}
	subscriptionID := subMsg.Subscription

	pub, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}
	pubID := wamp.GlobalID()
	pub.Send(&wamp.Publish{Request: pubID, Topic: testTopic})

	msg, err = wamp.RecvTimeout(sub, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for EVENT")
	}
	event, ok := msg.(*wamp.Event)
	if !ok {
		t.Fatal("Expected EVENT, got:", msg.MessageType())
	}
	if event.Subscription != subscriptionID {
		t.Fatal("wrong subscription ID")
	}
}

func TestPublishAcknowledge(t *testing.T) {
	defer leaktest.Check(t)()
	r, err := newTestRouter()
	if err != nil {
		t.Error(err)
	}
	defer r.Close()
	client, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	id := wamp.GlobalID()
	client.Send(&wamp.Publish{
		Request: id,
		Options: wamp.Dict{"acknowledge": true},
		Topic:   "some.uri"})

	msg, err := wamp.RecvTimeout(client, time.Second)
	if err != nil {
		t.Fatal("sent acknowledge=true, timed out waiting for PUBLISHED")
	}
	pub, ok := msg.(*wamp.Published)
	if !ok {
		t.Fatal("sent acknowledge=true, expected PUBLISHED, got:",
			msg.MessageType())
	}
	if pub.Request != id {
		t.Fatal("wrong request id")
	}
}

func TestPublishFalseAcknowledge(t *testing.T) {
	r, err := newTestRouter()
	if err != nil {
		t.Error(err)
	}
	defer r.Close()
	client, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	id := wamp.GlobalID()
	client.Send(&wamp.Publish{
		Request: id,
		Options: wamp.Dict{"acknowledge": false},
		Topic:   "some.uri"})

	msg, err := wamp.RecvTimeout(client, 200*time.Millisecond)
	if err == nil {
		if _, ok := msg.(*wamp.Published); ok {
			t.Fatal("Sent acknowledge=false, but received PUBLISHED:",
				msg.MessageType())
		}
	}
}

func TestPublishNoAcknowledge(t *testing.T) {
	r, err := newTestRouter()
	if err != nil {
		t.Error(err)
	}
	defer r.Close()
	client, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	id := wamp.GlobalID()
	client.Send(&wamp.Publish{Request: id, Topic: "some.uri"})
	msg, err := wamp.RecvTimeout(client, 200*time.Millisecond)
	if err == nil {
		if _, ok := msg.(*wamp.Published); ok {
			t.Fatal("Sent acknowledge=false, but received PUBLISHED:",
				msg.MessageType())
		}
	}
}

func TestRouterCall(t *testing.T) {
	defer leaktest.Check(t)()
	r, err := newTestRouter()
	if err != nil {
		t.Error(err)
	}
	defer r.Close()
	callee, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	registerID := wamp.GlobalID()
	// Register remote procedure
	callee.Send(&wamp.Register{Request: registerID, Procedure: testProcedure})

	msg, err := wamp.RecvTimeout(callee, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for REGISTERED")
	}
	registered, ok := msg.(*wamp.Registered)
	if !ok {
		t.Fatal("expected REGISTERED,got:", msg.MessageType())
	}
	if registered.Request != registerID {
		t.Fatal("wrong request ID")
	}
	registrationID := registered.Registration

	caller, err := testClient(r)
	if err != nil {
		t.Fatal("Error connecting caller:", err)
	}
	callID := wamp.GlobalID()
	// Call remote procedure
	caller.Send(&wamp.Call{Request: callID, Procedure: testProcedure})

	msg, err = wamp.RecvTimeout(callee, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for INVOCATION")
	}
	invocation, ok := msg.(*wamp.Invocation)
	if !ok {
		t.Fatal("expected INVOCATION, got:", msg.MessageType())
	}
	if invocation.Registration != registrationID {
		t.Fatal("wrong registration id")
	}
	invocationID := invocation.Request

	// Returns result of remove procedure
	callee.Send(&wamp.Yield{Request: invocationID})

	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
	}
	result, ok := msg.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}
	if result.Request != callID {
		t.Fatal("wrong result ID")
	}
}

func TestSessionCountMetaProcedure(t *testing.T) {
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

	// Call wamp.MetaProcSessionCount
	req := &wamp.Call{Request: wamp.GlobalID(), Procedure: wamp.MetaProcSessionCount}
	caller.Send(req)
	msg, err := wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := msg.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}
	if result.Request != req.Request {
		t.Fatal("wrong result ID")
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	count, ok := result.Arguments[0].(int)
	if !ok {
		t.Fatal("expected int arguemnt")
	}
	if count != 1 {
		t.Fatal("wrong session count")
	}

	// Call wamp.MetaProcSessionCount with invalid argument
	req = &wamp.Call{
		Request:   wamp.GlobalID(),
		Procedure: wamp.MetaProcSessionCount,
		Arguments: wamp.List{"should-be-a-list"},
	}
	caller.Send(req)
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	errResult, ok := msg.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR, got", msg.MessageType())
	}
	if errResult.Request != req.Request {
		t.Fatal("wrong result ID")
	}

	// Call wamp.MetaProcSessionCount with non-matching filter
	filter := wamp.List{"user", "def"}
	req = &wamp.Call{
		Request:   wamp.GlobalID(),
		Procedure: wamp.MetaProcSessionCount,
		Arguments: wamp.List{filter},
	}
	caller.Send(req)
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, ok = msg.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}
	if result.Request != req.Request {
		t.Fatal("wrong result ID")
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	count = result.Arguments[0].(int)
	if count != 0 {
		t.Fatal("wrong session count")
	}

	// Call wamp.MetaProcSessionCount with matching filter
	filter = wamp.List{"trusted", "user", "def"}
	req = &wamp.Call{
		Request:   wamp.GlobalID(),
		Procedure: wamp.MetaProcSessionCount,
		Arguments: wamp.List{filter},
	}
	caller.Send(req)
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, ok = msg.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}
	if result.Request != req.Request {
		t.Fatal("wrong result ID")
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	count = result.Arguments[0].(int)
	if count != 1 {
		t.Fatal("wrong session count")
	}
}

func TestListSessionMetaProcedures(t *testing.T) {
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

	// Call wamp.MetaProcSessionList to get session list.
	req := &wamp.Call{
		Request:   wamp.GlobalID(),
		Procedure: wamp.MetaProcSessionList,
	}
	caller.Send(req)
	msg, err := wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := msg.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}
	if result.Request != req.Request {
		t.Fatal("wrong result ID")
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	ids, ok := result.Arguments[0].([]wamp.ID)
	if !ok {
		t.Fatal("wrong arg type")
	}
	if len(ids) != 1 {
		t.Fatal("wrong number of session IDs")
	}
	if sessID != ids[0] {
		t.Fatal("wrong session ID")
	}

	// Call wamp.MetaProcSessionList with matching filter
	filter := wamp.List{"trusted"}
	req = &wamp.Call{
		Request:   wamp.GlobalID(),
		Procedure: wamp.MetaProcSessionList,
		Arguments: wamp.List{filter},
	}
	caller.Send(req)
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, ok = msg.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}
	if result.Request != req.Request {
		t.Fatal("wrong result ID")
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	ids, ok = result.Arguments[0].([]wamp.ID)
	if !ok {
		t.Fatal("wrong arg type")
	}
	if len(ids) != 1 {
		t.Fatal("wrong number of session IDs")
	}
	if sessID != ids[0] {
		t.Fatal("wrong session ID")
	}
}

func TestGetSessionMetaProcedures(t *testing.T) {
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

	// Call session meta-procedure with bad session ID
	callID := wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSessionGet,
		Arguments: wamp.List{wamp.ID(123456789)},
	})
	msg, err := wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	errRsp, ok := msg.(*wamp.Error)
	if !ok {
		t.Fatal("expected ERROR, got", msg.MessageType())
	}
	if errRsp.Error != wamp.ErrNoSuchSession {
		t.Fatal("wrong error value")
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
	result, ok := msg.(*wamp.Result)
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
	sid, _ := wamp.AsID(details["session"])
	if sid != sessID {
		t.Fatal("wrong session ID")
	}
}

func TestRegistrationMetaProcedures(t *testing.T) {
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

	// ----- Test wamp.registration.list meta procedure -----
	callID := wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcRegList})
	msg, err := wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
	}
	result, ok := msg.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}
	if result.Request != callID {
		t.Fatal("wrong result ID")
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	dict, ok := result.Arguments[0].(wamp.Dict)
	if !ok {
		t.Fatal("expected wamp.Dict")
	}
	exactPrev, ok := dict["exact"].([]wamp.ID)
	if !ok {
		t.Fatal("expected []wamp.ID")
	}
	prefixPrev, ok := dict["prefix"].([]wamp.ID)
	if !ok {
		t.Fatal("expected []wamp.ID")
	}
	wildcardPrev, ok := dict["wildcard"].([]wamp.ID)
	if !ok {
		t.Fatal("expected []wamp.ID")
	}

	callee, err := testClient(r)
	if err != nil {
		t.Fatal("Error connecting client:", err)
	}
	sessID = callee.ID
	// Register remote procedure
	registerID := wamp.GlobalID()
	callee.Send(&wamp.Register{Request: registerID, Procedure: testProcedure})

	msg, err = wamp.RecvTimeout(callee, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for REGISTERED")
	}
	registered, ok := msg.(*wamp.Registered)
	if !ok {
		t.Fatal("expected REGISTERED, got:", msg.MessageType())
	}
	if registered.Request != registerID {
		t.Fatal("wrong request ID")
	}
	registrationID := registered.Registration

	// Register remote procedure
	callee.Send(&wamp.Register{
		Request:   wamp.GlobalID(),
		Procedure: testProcedureWC,
		Options:   wamp.Dict{"match": "wildcard"},
	})
	msg = <-callee.Recv()
	if _, ok = msg.(*wamp.Registered); !ok {
		t.Fatal("expected REGISTERED, got:", msg.MessageType())
	}

	// Call session meta-procedure to get session count.
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcRegList})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
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
	dict, ok = result.Arguments[0].(wamp.Dict)
	if !ok {
		t.Fatal("expected wamp.Dict")
	}
	exact := dict["exact"].([]wamp.ID)
	prefix := dict["prefix"].([]wamp.ID)
	wildcard := dict["wildcard"].([]wamp.ID)

	if len(exact) != len(exactPrev)+1 {
		t.Fatal("expected additional exact match")
	}
	if len(prefix) != len(prefixPrev) {
		t.Fatal("prefix matches should not have changed")
	}
	if len(wildcard) != len(wildcardPrev)+1 {
		t.Fatal("expected additional wildcard match")
	}

	var found bool
	for i := range exact {
		if exact[i] == registrationID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing expected registration ID")
	}

	// ----- Test wamp.registration.lookup meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcRegLookup,
		Arguments: wamp.List{testProcedure},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
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
	regID, ok := result.Arguments[0].(wamp.ID)
	if !ok {
		t.Fatal("expected wamp.ID")
	}
	if regID != registrationID {
		t.Fatal("received wrong registration ID")
	}

	// ----- Test wamp.registration.match meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcRegMatch,
		Arguments: wamp.List{testProcedure},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
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
	regID, ok = wamp.AsID(result.Arguments[0])
	if !ok {
		t.Fatal("expected wamp.ID")
	}
	if regID != registrationID {
		t.Fatal("received wrong registration ID")
	}

	// ----- Test wamp.registration.get meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcRegGet,
		Arguments: wamp.List{registrationID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
	}
	result, ok = msg.(*wamp.Result)
	if !ok {
		t.Fatalf("expected RESULT, got %s %+v", msg.MessageType(), msg)
	}
	if result.Request != callID {
		t.Fatal("wrong result ID")
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	dict, ok = result.Arguments[0].(wamp.Dict)
	if !ok {
		t.Fatal("expected wamp.Dict")
	}
	regID, _ = wamp.AsID(dict["id"])
	if regID != registrationID {
		t.Fatal("received wrong registration")
	}
	uri, _ := wamp.AsURI(dict["uri"])
	if uri != testProcedure {
		t.Fatal("registration has wrong uri:", uri)
	}

	// ----- Test wamp.registration.list_callees meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcRegListCallees,
		Arguments: wamp.List{registrationID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
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
	idList, ok := result.Arguments[0].([]wamp.ID)
	if !ok {
		t.Fatal("Expected []wamp.ID")
	}
	if len(idList) != 1 {
		t.Fatal("Expected 1 callee in list")
	}
	if idList[0] != sessID {
		t.Fatal("Wrong callee session ID")
	}

	// ----- Test wamp.registration.count_callees meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcRegCountCallees,
		Arguments: wamp.List{registrationID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
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
	count, ok := wamp.AsInt64(result.Arguments[0])
	if !ok {
		t.Fatal("Argument is not an int")
	}
	if count != 1 {
		t.Fatal("Wring number of callees")
	}
}

func TestSubscriptionMetaProcedures(t *testing.T) {
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

	// ----- Test wamp.subscription.list meta procedure -----
	callID := wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcSubList})
	msg, err := wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
	}
	result, ok := msg.(*wamp.Result)
	if !ok {
		t.Fatal("expected RESULT, got", msg.MessageType())
	}
	if result.Request != callID {
		t.Fatal("wrong result ID")
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	dict, ok := result.Arguments[0].(wamp.Dict)
	if !ok {
		t.Fatal("expected wamp.Dict")
	}
	exactPrev, ok := dict["exact"].([]wamp.ID)
	if !ok {
		t.Fatal("expected []wamp.ID")
	}
	prefixPrev, ok := dict["prefix"].([]wamp.ID)
	if !ok {
		t.Fatal("expected []wamp.ID")
	}
	wildcardPrev, ok := dict["wildcard"].([]wamp.ID)
	if !ok {
		t.Fatal("expected []wamp.ID")
	}

	subscriber, err := testClient(r)
	if err != nil {
		t.Fatal("Error connecting client:", err)
	}
	sessID = subscriber.ID
	// Subscribe to topic
	reqID := wamp.GlobalID()
	subscriber.Send(&wamp.Subscribe{Request: reqID, Topic: testTopic})
	msg, err = wamp.RecvTimeout(subscriber, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for SUBSCRIBED")
	}
	subscribed, ok := msg.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected SUBSCRIBED, got:", msg.MessageType())
	}
	if subscribed.Request != reqID {
		t.Fatal("wrong request ID")
	}
	subscriptionID := subscribed.Subscription

	// Subscriber to wildcard topic
	subscriber.Send(&wamp.Subscribe{
		Request: wamp.GlobalID(),
		Topic:   testTopicWC,
		Options: wamp.Dict{"match": "wildcard"},
	})
	msg = <-subscriber.Recv()
	if _, ok = msg.(*wamp.Subscribed); !ok {
		t.Fatal("expected SUBSCRIBED, got:", msg.MessageType())
	}

	// Call subscription meta-procedure to get subscriptions.
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcSubList})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
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
	dict, ok = result.Arguments[0].(wamp.Dict)
	if !ok {
		t.Fatal("expected wamp.Dict")
	}
	exact := dict["exact"].([]wamp.ID)
	prefix := dict["prefix"].([]wamp.ID)
	wildcard := dict["wildcard"].([]wamp.ID)

	if len(exact) != len(exactPrev)+1 {
		t.Error("expected additional exact match")
	}
	if len(prefix) != len(prefixPrev) {
		t.Error("prefix matches should not have changed")
	}
	if len(wildcard) != len(wildcardPrev)+1 {
		t.Error("expected additional wildcard match")
	}

	var found bool
	for i := range exact {
		if exact[i] == subscriptionID {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing expected subscription ID")
	}

	// ----- Test wamp.subscription.lookup meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSubLookup,
		Arguments: wamp.List{testTopic},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
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
	subID, ok := result.Arguments[0].(wamp.ID)
	if !ok {
		t.Fatal("expected wamp.ID")
	}
	if subID != subscriptionID {
		t.Fatal("received wrong subscription ID")
	}

	// ----- Test wamp.subscription.match meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSubMatch,
		Arguments: wamp.List{testTopic},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
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
	subIDs, ok := wamp.AsList(result.Arguments[0])
	if !ok {
		t.Fatal("expected wamp.List")
	}
	if len(subIDs) != 2 {
		t.Error("expected 2 subscriptions for wamp.subscription.match, got", len(subIDs))
	}

	// ----- Test wamp.subscription.get meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSubGet,
		Arguments: wamp.List{subscriptionID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
	}
	result, ok = msg.(*wamp.Result)
	if !ok {
		t.Fatalf("expected RESULT, got %s %+v", msg.MessageType(), msg)
	}
	if result.Request != callID {
		t.Fatal("wrong result ID")
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	dict, ok = result.Arguments[0].(wamp.Dict)
	if !ok {
		t.Fatal("expected wamp.Dict")
	}
	subID, _ = wamp.AsID(dict["id"])
	if subID != subscriptionID {
		t.Fatal("received wrong subscription")
	}
	uri, _ := wamp.AsURI(dict["uri"])
	if uri != testTopic {
		t.Fatal("subscription has wrong uri:", uri)
	}

	// ----- Test wamp.subscription.list_subscribers meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSubListSubscribers,
		Arguments: wamp.List{subscriptionID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
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
	idList, ok := result.Arguments[0].([]wamp.ID)
	if !ok {
		t.Fatal("Expected []wamp.ID")
	}
	if len(idList) != 1 {
		t.Fatal("Expected 1 subscriber in list")
	}
	if idList[0] != sessID {
		t.Fatal("Wrong subscriber session ID")
	}

	// ----- Test wamp.subscription.count_subscribers meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSubCountSubscribers,
		Arguments: wamp.List{subscriptionID},
	})
	msg, err = wamp.RecvTimeout(caller, time.Second)
	if err != nil {
		t.Fatal("Timed out waiting for RESULT")
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
	count, ok := wamp.AsInt64(result.Arguments[0])
	if !ok {
		t.Fatal("Argument is not an int")
	}
	if count != 1 {
		t.Fatal("Wring number of subscribers")
	}
}

func TestDynamicRealmChange(t *testing.T) {
	defer leaktest.Check(t)

	dr, err := newTestRouter()
	if err != nil {
		t.Fatal(err)
	}
	defer dr.Close()

	err = dr.AddRealm(&RealmConfig{
		URI:           testRealm2,
		StrictURI:     false,
		AnonymousAuth: true,
		AllowDisclose: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	cli, err := testClientInRealm(dr, testRealm2)
	if err != nil {
		t.Fatal(err)
	}

	cli.Send(&wamp.Goodbye{})
	msg, err := wamp.RecvTimeout(cli, time.Second)
	if err != nil {
		t.Fatal("no goodbye message after sending goodbye")
	}
	if _, ok := msg.(*wamp.Goodbye); !ok {
		t.Fatalf("expected GOODBYE, received %s", msg.MessageType())
	}

	cli, err = testClientInRealm(dr, testRealm2)
	if err != nil {
		t.Fatal(err)
	}

	sync := make(chan wamp.Message)
	go func() {
		sync <- <-cli.Recv()
	}()

	dr.RemoveRealm(testRealm2)

	select {
	case <-time.After(time.Second):
		t.Fatal("expected client to be booted when removing realm")
	case msg := <-sync:
		if _, ok := msg.(*wamp.Goodbye); !ok {
			t.Fatalf("expected GOODBYE, received %s", msg.MessageType())
		}
	}
}
