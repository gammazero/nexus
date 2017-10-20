package router

import (
	"errors"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/wamp"
)

const (
	testRealm       = wamp.URI("nexus.test.realm")
	testProcedure   = wamp.URI("nexus.test.endpoint")
	testProcedureWC = wamp.URI("nexus..endpoint")
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
	config := &RouterConfig{
		RealmConfigs: []*RealmConfig{
			{
				URI:           testRealm,
				StrictURI:     false,
				AnonymousAuth: true,
				AllowDisclose: false,
			},
		},
		Debug: debug,
	}
	return NewRouter(config, logger)
}

func testClient(r Router) (*wamp.Session, error) {
	client, server := transport.LinkedPeers(logger)
	// Run as goroutine since Send will block until message read by router, if
	// client uses unbuffered channel.
	go client.Send(&wamp.Hello{Realm: testRealm, Details: clientRoles})
	err := r.Attach(server)
	if err != nil {
		return nil, err
	}

	var sid wamp.ID
	select {
	case <-time.After(time.Second):
		return nil, errors.New("timed out waiting for welcome")
	case msg := <-client.Recv():
		if msg.MessageType() != wamp.WELCOME {
			return nil, fmt.Errorf("expected %v, got %v", wamp.WELCOME,
				msg.MessageType())
		}
		sid = msg.(*wamp.Welcome).ID
	}
	return &wamp.Session{
		Peer: client,
		ID:   sid,
	}, nil
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
	select {
	case <-time.After(time.Second):
		t.Fatal("no goodbye message after sending goodbye")
	case msg := <-cli.Recv():
		if _, ok := msg.(*wamp.Goodbye); !ok {
			t.Fatal("expected GOODBYE, received:", msg.MessageType())
		}
	}
}

func TestHandshakeBadRealm(t *testing.T) {
	defer leaktest.Check(t)()
	r, err := newTestRouter()
	if err != nil {
		t.Error(err)
	}
	defer r.Close()

	client, server := transport.LinkedPeers(logger)
	go client.Send(&wamp.Hello{Realm: "does.not.exist"})
	err = r.Attach(server)
	if err == nil {
		t.Fatal("expected error")
	}

	select {
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response to HELLO")
	case msg := <-client.Recv():
		if _, ok := msg.(*wamp.Abort); !ok {
			t.Error("Expected ABORT after bad handshake")
		}
	}
}

func TestRouterSubscribe(t *testing.T) {
	defer leaktest.Check(t)()
	const testTopic = wamp.URI("some.uri")
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

	var subscriptionID wamp.ID
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for SUBSCRIBED")
	case msg := <-sub.Recv():
		subMsg, ok := msg.(*wamp.Subscribed)
		if !ok {
			t.Fatal("Expected SUBSCRIBED, got:", msg.MessageType())
		}
		if subMsg.Request != subscribeID {
			t.Fatal("wrong request ID")
		}
		subscriptionID = subMsg.Subscription
	}

	pub, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}
	pubID := wamp.GlobalID()
	pub.Send(&wamp.Publish{Request: pubID, Topic: testTopic})

	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for EVENT")
	case msg := <-sub.Recv():
		event, ok := msg.(*wamp.Event)
		if !ok {
			t.Fatal("Expected EVENT, got:", msg.MessageType())
		}
		if event.Subscription != subscriptionID {
			t.Fatal("wrong subscription ID")
		}
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

	select {
	case <-time.After(time.Second):
		t.Fatal("sent acknowledge=true, timed out waiting for PUBLISHED")
	case msg := <-client.Recv():
		pub, ok := msg.(*wamp.Published)
		if !ok {
			t.Fatal("sent acknowledge=true, expected PUBLISHED, got:",
				msg.MessageType())
		}
		if pub.Request != id {
			t.Fatal("wrong request id")
		}
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

	select {
	case <-time.After(200 * time.Millisecond):
	case msg := <-client.Recv():
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
	select {
	case <-time.After(200 * time.Millisecond):
	case msg := <-client.Recv():
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

	var registrationID wamp.ID
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for REGISTERED")
	case msg := <-callee.Recv():
		registered, ok := msg.(*wamp.Registered)
		if !ok {
			t.Fatal("expected REGISTERED,got:", msg.MessageType())
		}
		if registered.Request != registerID {
			t.Fatal("wrong request ID")
		}
		registrationID = registered.Registration
	}

	caller, err := testClient(r)
	if err != nil {
		t.Fatal("Error connecting caller:", err)
	}
	callID := wamp.GlobalID()
	// Call remote procedure
	caller.Send(&wamp.Call{Request: callID, Procedure: testProcedure})

	var invocationID wamp.ID
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for INVOCATION")
	case msg := <-callee.Recv():
		invocation, ok := msg.(*wamp.Invocation)
		if !ok {
			t.Fatal("expected INVOCATION, got:", msg.MessageType())
		}
		if invocation.Registration != registrationID {
			t.Fatal("wrong registration id")
		}
		invocationID = invocation.Request
	}

	// Returns result of remove procedure
	callee.Send(&wamp.Yield{Request: invocationID})

	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok := msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected RESULT, got", msg.MessageType())
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
	}
}

func TestSessionMetaProcedures(t *testing.T) {
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
	var result *wamp.Result
	var ok bool

	// Call session meta-procedure to get session count.
	callID := wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcSessionCount})
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok = msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected RESULT, got", msg.MessageType())
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
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

	// Call session meta-procedure to get session list.
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcSessionList})
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok = msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected RESULT, got", msg.MessageType())
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	ids, ok := result.Arguments[0].([]wamp.ID)
	if !ok {
		t.Fatal("wrong arg type")
	}
	if len(ids) != count {
		t.Fatal("wrong number of session IDs")
	}
	if sessID != ids[0] {
		t.Fatal("wrong session ID")
	}

	// Call session meta-procedure with bad session ID
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSessionGet,
		Arguments: wamp.List{wamp.ID(123456789)},
	})
	var errRsp *wamp.Error
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		errRsp, ok = msg.(*wamp.Error)
		if !ok {
			t.Fatal("expected ERROR, got", msg.MessageType())
		}
		if errRsp.Error != wamp.ErrNoSuchSession {
			t.Fatal("wrong error value")
		}
	}

	// Call session meta-procedure to get session get.
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcSessionGet,
		Arguments: wamp.List{sessID},
	})
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok = msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected RESULT, got", msg.MessageType())
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	dict, ok := result.Arguments[0].(wamp.Dict)
	if !ok {
		t.Fatal("expected dict type arg")
	}
	sid := wamp.ID(wamp.OptionInt64(dict, "session"))
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
	var result *wamp.Result
	var ok bool

	// ----- Test wamp.registration.list meta procedure -----
	callID := wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcRegList})
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok = msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected RESULT, got", msg.MessageType())
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
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

	var registrationID wamp.ID
	var registered *wamp.Registered
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for REGISTERED")
	case msg := <-callee.Recv():
		registered, ok = msg.(*wamp.Registered)
		if !ok {
			t.Fatal("expected REGISTERED, got:", msg.MessageType())
		}
		if registered.Request != registerID {
			t.Fatal("wrong request ID")
		}
		registrationID = registered.Registration
	}

	// Register remote procedure
	callee.Send(&wamp.Register{
		Request:   wamp.GlobalID(),
		Procedure: testProcedureWC,
		Options:   wamp.Dict{"match": "wildcard"},
	})
	msg := <-callee.Recv()
	if _, ok = msg.(*wamp.Registered); !ok {
		t.Fatal("expected REGISTERED, got:", msg.MessageType())
	}

	// Call session meta-procedure to get session count.
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{Request: callID, Procedure: wamp.MetaProcRegList})
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok = msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected RESULT, got", msg.MessageType())
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
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
		t.Fatal("wildcard matches should not have changed")
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
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok = msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected RESULT, got", msg.MessageType())
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
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
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok = msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected RESULT, got", msg.MessageType())
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
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
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok = msg.(*wamp.Result)
		if !ok {
			t.Fatalf("expected RESULT, got %s %+v", msg.MessageType(), msg)
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
	}
	if len(result.Arguments) == 0 {
		t.Fatal("missing expected arguemnt")
	}
	dict, ok = result.Arguments[0].(wamp.Dict)
	if !ok {
		t.Fatal("expected wamp.Dict")
	}
	regID = wamp.OptionID(dict, "id")
	if regID != registrationID {
		t.Fatal("received wrong registration")
	}
	uri := wamp.OptionURI(dict, "uri")
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
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok = msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected RESULT, got", msg.MessageType())
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
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

	// ----- Test wamp.registration.list_callees meta procedure -----
	callID = wamp.GlobalID()
	caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcRegCountCallees,
		Arguments: wamp.List{registrationID},
	})
	select {
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok = msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected RESULT, got", msg.MessageType())
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
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
