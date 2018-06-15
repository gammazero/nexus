package router

import (
	"testing"

	"github.com/gammazero/nexus/wamp"
)

const (
	denyTopic  = wamp.URI("forbidden.topic")
	allowTopic = wamp.URI("allowed.topic")
)

// testAuthz implements Authorizer interface.
type testAuthz struct{}

func (a *testAuthz) Authorize(sess *wamp.Session, msg wamp.Message) (bool, error) {
	m, ok := msg.(*wamp.Subscribe)
	if ok {
		if m.Topic == denyTopic {
			return false, nil
		}
	}
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

	// Test that authroizer forbids denyTopic.
	subscribeID := wamp.GlobalID()
	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: denyTopic})
	msg := <-sub.Recv()
	_, ok := msg.(*wamp.Error)
	if !ok {
		t.Fatal("Expected ERROR, got:", msg.MessageType())
	}

	// Test that authroizer allows allowTopic.
	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: allowTopic})
	msg = <-sub.Recv()
	if _, ok = msg.(*wamp.Subscribed); !ok {
		t.Fatal("Expected SUBSCRIBED, got:", msg.MessageType())
	}
}

// Test that authroizer is not called with a local session and config does not
// specify RequireLocalAuthz=true.
func TestAuthorizerBypassLocal(t *testing.T) {
	config := &Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:        testRealm,
				Authorizer: &testAuthz{},
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
