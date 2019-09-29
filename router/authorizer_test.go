package router

import (
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/wamp"
)

const (
	denyTopic  = wamp.URI("forbidden.topic")
	allowTopic = wamp.URI("allowed.topic")
)

// testAuthz implements Authorizer interface.
type testAuthz struct{}

func (a *testAuthz) Authorize(session *wamp.Session, msg wamp.Message) (bool, error) {
	m, ok := msg.(*wamp.Subscribe)
	if ok {
		if m.Topic == denyTopic {
			return false, nil
		}
	}
	return true, nil
}

type testAuthzMod struct{}

// Authorize implementation that modifies the session details.
func (a *testAuthzMod) Authorize(sess *wamp.Session, msg wamp.Message) (bool, error) {
	count, _ := wamp.AsInt64(sess.Details["count"])
	count++
	sess.Details["count"] = count
	sess.Details["foo"] = "bar"
	delete(sess.Details, "xyzzy")
	return true, nil
}

// Test that Authorize is being called for messages received by router and is
// able to determine whether or not a message is allowed.
func TestAuthorizer(t *testing.T) {
	config := &Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:               testRealm,
				Authorizer:        &testAuthz{},
				RequireLocalAuthz: true,
			},
		},
		Debug: debug,
	}
	r, err := NewRouter(config, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	sub, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	// Test that authorizer forbids denyTopic.
	subscribeID := wamp.GlobalID()
	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: denyTopic})
	msg := <-sub.Recv()
	_, ok := msg.(*wamp.Error)
	if !ok {
		t.Fatal("Expected ERROR, got:", msg.MessageType())
	}

	// Test that authorizer allows allowTopic.
	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: allowTopic})
	msg = <-sub.Recv()
	if _, ok = msg.(*wamp.Subscribed); !ok {
		t.Fatal("Expected SUBSCRIBED, got:", msg.MessageType())
	}
}

// Test that authorizer is not called with a local session and config does not
// specify RequireLocalAuthz=true.
func TestAuthorizerBypassLocal(t *testing.T) {
	config := &Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:        testRealm,
				Authorizer: &testAuthzMod{},
			},
		},
		Debug: debug,
	}
	r, err := NewRouter(config, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	sub, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	subscribeID := wamp.GlobalID()
	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: denyTopic})
	msg := <-sub.Recv()
	if _, ok := msg.(*wamp.Subscribed); !ok {
		t.Fatal("Expected SUBSCRIBED, got:", msg.MessageType())
	}
}

func TestAuthorizerModify(t *testing.T) {
	config := &Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:               testRealm,
				Authorizer:        &testAuthzMod{},
				RequireLocalAuthz: true,
			},
		},
		Debug: debug,
	}
	r, err := NewRouter(config, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	cli, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10; i++ {
		cli.Send(&wamp.Call{
			Request:   wamp.ID(i),
			Procedure: wamp.MetaProcSessionGet,
			Arguments: wamp.List{cli.ID},
		})
		msg, err := wamp.RecvTimeout(cli, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		result, ok := msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected wamp.Result, got", msg.MessageType())
		}
		details, ok := wamp.AsDict(result.Arguments[0])
		if !ok {
			t.Fatal("Result.Arguments[0] was not wamp.Dict")
		}
		i64, ok := wamp.AsInt64(details["count"])
		if !ok {
			t.Fatal("Session details did not have count")
		}
		if i64 != int64(i) {
			t.Fatal("Expected count to be", i, "got", i64)
		}
		foo, _ := wamp.AsString(details["foo"])
		if foo != "bar" {
			t.Fatal("Wrong value for foo")
		}
		if _, ok = details["xyzzy"]; ok {
			t.Fatal("Should not have xyzzy detail")
		}
	}
}

// Test for races when Authorizer modifies session details.
func TestAuthorizerRace(t *testing.T) {
	config := &Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:               testRealm,
				Authorizer:        &testAuthzMod{},
				RequireLocalAuthz: true,
			},
		},
		Debug: debug,
	}
	r, err := NewRouter(config, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	sub, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	pub, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	cli, err := testClient(r)
	if err != nil {
		t.Fatal(err)
	}

	// Test that authorizer forbids denyTopic.
	subscribeID := wamp.GlobalID()
	const topic = "whatever.topic"
	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: topic})
	msg, err := wamp.RecvTimeout(sub, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := msg.(*wamp.Subscribed); !ok {
		t.Fatal("Expected SUBSCRIBED, got:", msg.MessageType())
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			pub.Send(&wamp.Publish{
				Request: wamp.ID(i),
				Topic:   topic,
				Options: wamp.Dict{"eligible_xyzzy": wamp.List{"plugh", "baz"}},
			})
		}
		done <- struct{}{}
	}()

	go func() {
		for i := 0; i < 100; i++ {
			cli.Send(&wamp.Call{
				Request:   wamp.ID(100 + i),
				Procedure: wamp.MetaProcSessionGet,
				Arguments: wamp.List{pub.ID},
			})
			<-cli.Recv()
			cli.Send(&wamp.Call{
				Request:   wamp.ID(200 + i),
				Procedure: wamp.MetaProcSessionGet,
				Arguments: wamp.List{sub.ID},
			})
			<-cli.Recv()
		}
		done <- struct{}{}
	}()

	for i := 0; i < 100; i++ {
		sub.Send(&wamp.Publish{
			Request: wamp.ID(500 + i),
			Topic:   topic,
			Options: wamp.Dict{"eligible_xyzzy": wamp.List{"plugh"}},
		})
	}
	<-done
	<-done
}
