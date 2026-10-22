package router //nolint:testpackage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
		for i := range 100 {
			pub.Send() <- &wamp.Publish{
				Request: wamp.ID(i),
				Topic:   topic,
				Options: wamp.Dict{"eligible_xyzzy": wamp.List{"plugh", "baz"}},
			}
		}
		done <- struct{}{}
	}()

	go func() {
		for i := range 100 {
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

	for i := range 100 {
		sub.Send() <- &wamp.Publish{
			Request: wamp.ID(500 + i),
			Topic:   topic,
			Options: wamp.Dict{"eligible_xyzzy": wamp.List{"plugh"}},
		}
	}
	<-done
	<-done
}

// blockingAuthz blocks Authorize for REGISTER of blockProc until release is
// closed, signalling entered once it is blocked. Everything else is allowed.
type blockingAuthz struct {
	entered chan struct{}
	release chan struct{}
}

const blockProc = wamp.URI("slow.proc")

func (a *blockingAuthz) Authorize(_ *wamp.Session, msg wamp.Message) (bool, error) {
	if m, ok := msg.(*wamp.Register); ok && m.Procedure == blockProc {
		close(a.entered)
		<-a.release
	}
	return true, nil
}

// runBlockedAuthorizeScenario parks session A inside Authorize, then has B
// publish two eligible-filtered events to a topic that A and C subscribe to.
// It reports whether C received the SECOND event while A was still blocked:
// the broker handles publishes one at a time, so the second event can only
// reach C once the first publish finished filtering every subscriber,
// including A.
func runBlockedAuthorizeScenario(t *testing.T, unlocked bool) bool {
	authz := &blockingAuthz{entered: make(chan struct{}), release: make(chan struct{})}
	config := &Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:               testRealm,
				Authorizer:        authz,
				RequireLocalAuthz: true,
				AuthorizeUnlocked: unlocked,
			},
		},
		Debug: debug,
	}
	r, err := NewRouter(config, logger)
	require.NoError(t, err)
	defer r.Close()

	const topic = wamp.URI("some.topic")
	subA := testClient(t, r)
	subC := testClient(t, r)
	pub := testClient(t, r)
	for _, s := range []*wamp.Session{subA, subC} {
		s.Send() <- &wamp.Subscribe{Request: 1, Topic: topic}
		msg, err := wamp.RecvTimeout(s, time.Second)
		require.NoError(t, err)
		_, ok := msg.(*wamp.Subscribed)
		require.True(t, ok, "expected SUBSCRIBED")
	}

	// Park A inside Authorize.
	subA.Send() <- &wamp.Register{Request: 2, Procedure: blockProc}
	<-authz.entered
	defer close(authz.release)

	eligible := wamp.List{subA.ID, subC.ID}
	for i := 1; i <= 2; i++ {
		pub.Send() <- &wamp.Publish{
			Request:   wamp.ID(10 + i),
			Topic:     topic,
			Arguments: wamp.List{i},
			Options:   wamp.Dict{wamp.WhitelistKey: eligible},
		}
	}

	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case msg := <-subC.Recv():
			if ev, ok := msg.(*wamp.Event); ok && len(ev.Arguments) == 1 && ev.Arguments[0] == 2 {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// With AuthorizeUnlocked, a session blocked in Authorize must not delay event
// delivery to other subscribers.
func TestAuthorizeUnlockedDoesNotStallBroker(t *testing.T) {
	require.True(t, runBlockedAuthorizeScenario(t, true),
		"second event not delivered to C while A was blocked in Authorize")
}

// Negative control for the test above: by default the session is locked
// during Authorize, and the broker's publish filter waits on that lock.
func TestAuthorizeLockedStallsBroker(t *testing.T) {
	require.False(t, runBlockedAuthorizeScenario(t, false),
		"expected the broker to stall on the locked subscriber session")
}

// With AuthorizeUnlocked the Authorizer sees a snapshot: changes it makes to
// the session details are discarded.
func TestAuthorizeUnlockedDiscardsDetailChanges(t *testing.T) {
	config := &Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:               testRealm,
				Authorizer:        &testAuthzMod{},
				RequireLocalAuthz: true,
				AuthorizeUnlocked: true,
			},
		},
		Debug: debug,
	}
	r, err := NewRouter(config, logger)
	require.NoError(t, err)
	defer r.Close()

	cli := testClient(t, r)
	cli.Send() <- &wamp.Call{
		Request:   1,
		Procedure: wamp.MetaProcSessionGet,
		Arguments: wamp.List{cli.ID},
	}
	msg, err := wamp.RecvTimeout(cli, time.Second)
	require.NoError(t, err)
	result, ok := msg.(*wamp.Result)
	require.True(t, ok, "expected wamp.Result")
	details, ok := wamp.AsDict(result.Arguments[0])
	require.True(t, ok, "Result.Arguments[0] was not wamp.Dict")
	_, ok = details["foo"]
	require.False(t, ok, "snapshot change leaked into the session")
}
