package router

import (
	"testing"
	"time"

	"github.com/dtegapp/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
	defer r.Close()

	sub := testClient(t, r)

	// Test that authorizer forbids denyTopic.
	subscribeID := wamp.GlobalID()
	sub.Send() <- &wamp.Subscribe{Request: subscribeID, Topic: denyTopic}
	msg := <-sub.Recv()
	_, ok := msg.(*wamp.Error)
	require.True(t, ok, "Expected ERROR")

	// Test that authorizer allows allowTopic.
	sub.Send() <- &wamp.Subscribe{Request: subscribeID, Topic: allowTopic}
	msg = <-sub.Recv()
	_, ok = msg.(*wamp.Subscribed)
	require.True(t, ok, "Expected SUBSCRIBED")
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
	require.NoError(t, err)
	defer r.Close()

	sub := testClient(t, r)

	subscribeID := wamp.GlobalID()
	sub.Send() <- &wamp.Subscribe{Request: subscribeID, Topic: denyTopic}
	msg := <-sub.Recv()
	_, ok := msg.(*wamp.Subscribed)
	require.True(t, ok, "Expected SUBSCRIBED")
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
	require.NoError(t, err)
	defer r.Close()

	cli := testClient(t, r)
	for i := 1; i <= 10; i++ {
		cli.Send() <- &wamp.Call{
			Request:   wamp.ID(i),
			Procedure: wamp.MetaProcSessionGet,
			Arguments: wamp.List{cli.ID},
		}
		msg, err := wamp.RecvTimeout(cli, time.Second)
		require.NoError(t, err)
		result, ok := msg.(*wamp.Result)
		require.True(t, ok, "expected wamp.Result")
		details, ok := wamp.AsDict(result.Arguments[0])
		require.True(t, ok, "Result.Arguments[0] was not wamp.Dict")
		i64, ok := wamp.AsInt64(details["count"])
		require.True(t, ok, "Session details did not have count")
		require.Equal(t, int64(i), i64)
		foo, _ := wamp.AsString(details["foo"])
		require.Equal(t, "bar", foo)
		_, ok = details["xyzzy"]
		require.False(t, ok, "Should not have xyzzy detail")
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
	require.NoError(t, err)
	defer r.Close()

	sub := testClient(t, r)
	pub := testClient(t, r)
	cli := testClient(t, r)

	// Test that authorizer forbids denyTopic.
	subscribeID := wamp.GlobalID()
	const topic = "whatever.topic"
	sub.Send() <- &wamp.Subscribe{Request: subscribeID, Topic: topic}
	msg, err := wamp.RecvTimeout(sub, time.Second)
	require.NoError(t, err)
	_, ok := msg.(*wamp.Subscribed)
	require.True(t, ok, "Expected SUBSCRIBED")

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			pub.Send() <- &wamp.Publish{
				Request: wamp.ID(i),
				Topic:   topic,
				Options: wamp.Dict{"eligible_xyzzy": wamp.List{"plugh", "baz"}},
			}
		}
		done <- struct{}{}
	}()

	go func() {
		for i := 0; i < 100; i++ {
			cli.Send() <- &wamp.Call{
				Request:   wamp.ID(100 + i),
				Procedure: wamp.MetaProcSessionGet,
				Arguments: wamp.List{pub.ID},
			}
			<-cli.Recv()
			cli.Send() <- &wamp.Call{
				Request:   wamp.ID(200 + i),
				Procedure: wamp.MetaProcSessionGet,
				Arguments: wamp.List{sub.ID},
			}
			<-cli.Recv()
		}
		done <- struct{}{}
	}()

	for i := 0; i < 100; i++ {
		sub.Send() <- &wamp.Publish{
			Request: wamp.ID(500 + i),
			Topic:   topic,
			Options: wamp.Dict{"eligible_xyzzy": wamp.List{"plugh"}},
		}
	}
	<-done
	<-done
}
