package router //nolint:testpackage

import (
	"fmt"
	"maps"
	"math"
	"sync"
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

// blockingAuthz blocks Authorize for REGISTER of blockProc until unblock is
// called, signalling entered once it is blocked. When it is unblocked, it sets
// the "authorized" detail. Everything else is allowed without changes.
type blockingAuthz struct {
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

const blockProc = wamp.URI("slow.proc")

func newBlockingAuthz() *blockingAuthz {
	return &blockingAuthz{entered: make(chan struct{}), release: make(chan struct{})}
}

func (a *blockingAuthz) unblock() {
	a.releaseOnce.Do(func() { close(a.release) })
}

func (a *blockingAuthz) Authorize(sess *wamp.Session, msg wamp.Message) (bool, error) {
	if m, ok := msg.(*wamp.Register); ok && m.Procedure == blockProc {
		close(a.entered)
		<-a.release
		sess.Details["authorized"] = true
	}
	return true, nil
}

// newAuthzRouter returns a router with one realm that uses authz for local
// sessions too.
func newAuthzRouter(t *testing.T, authz Authorizer, metaModify bool) Router {
	t.Helper()
	r, err := NewRouter(&Config{
		RealmConfigs: []*RealmConfig{
			{
				URI:               testRealm,
				Authorizer:        authz,
				RequireLocalAuthz: true,
				EnableMetaModify:  metaModify,
			},
		},
		Debug: debug,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(r.Close)
	return r
}

// sessionDetails returns the details of the session with ID sid, as returned
// by wamp.session.get called by cli.
func sessionDetails(t *testing.T, cli *wamp.Session, reqID, sid wamp.ID) wamp.Dict {
	t.Helper()
	cli.Send() <- &wamp.Call{
		Request:   reqID,
		Procedure: wamp.MetaProcSessionGet,
		Arguments: wamp.List{sid},
	}
	result := recvType[*wamp.Result](t, cli)
	details, ok := wamp.AsDict(result.Arguments[0])
	require.True(t, ok, "Result.Arguments[0] was not wamp.Dict")
	return details
}

// TestAuthorizeDoesNotStallBroker checks that a session blocked in Authorize
// does not delay event delivery to other subscribers. The broker locks each
// subscriber's session while applying a publish filter, and handles a realm's
// publishes one at a time, so Authorize must not run with the session locked.
//
// Session A is parked in Authorize, then B publishes two eligible-filtered
// events to a topic that A and C subscribe to. The second event can only reach
// C once the first publish finished filtering every subscriber, including A.
func TestAuthorizeDoesNotStallBroker(t *testing.T) {
	authz := newBlockingAuthz()
	r := newAuthzRouter(t, authz, false)
	// Unblock before the router is closed, since closing it waits for a session
	// blocked in Authorize.
	t.Cleanup(authz.unblock)

	const topic = wamp.URI("some.topic")
	subA := testClient(t, r)
	subC := testClient(t, r)
	pub := testClient(t, r)
	for _, s := range []*wamp.Session{subA, subC} {
		s.Send() <- &wamp.Subscribe{Request: 1, Topic: topic}
		recvType[*wamp.Subscribed](t, s)
	}

	// Park A inside Authorize.
	subA.Send() <- &wamp.Register{Request: 2, Procedure: blockProc}
	<-authz.entered

	eligible := wamp.List{subA.ID, subC.ID}
	for i := 1; i <= 2; i++ {
		pub.Send() <- &wamp.Publish{
			Request:   wamp.ID(10 + i),
			Topic:     topic,
			Arguments: wamp.List{i},
			Options:   wamp.Dict{wamp.WhitelistKey: eligible},
		}
	}

	timeout := time.After(5 * time.Second)
	for {
		select {
		case msg := <-subC.Recv():
			if ev, ok := msg.(*wamp.Event); ok && len(ev.Arguments) == 1 && ev.Arguments[0] == 2 {
				return
			}
		case <-timeout:
			require.FailNow(t, "second event not delivered to C while A was blocked in Authorize")
		}
	}
}

// TestAuthorizeKeepsConcurrentDetailChanges checks that applying the changes
// an Authorizer made does not undo changes made to the session meanwhile. The
// Authorizer runs on a copy of the session details while
// wamp.session.modify_details changes the session itself.
func TestAuthorizeKeepsConcurrentDetailChanges(t *testing.T) {
	authz := newBlockingAuthz()
	r := newAuthzRouter(t, authz, true)
	t.Cleanup(authz.unblock)
	cli := testClient(t, r)
	admin := testClient(t, r)

	// Park cli inside Authorize, after its details were copied.
	cli.Send() <- &wamp.Register{Request: 1, Procedure: blockProc}
	<-authz.entered

	admin.Send() <- &wamp.Call{
		Request:   1,
		Procedure: wamp.MetaProcSessionModifyDetails,
		Arguments: wamp.List{cli.ID, wamp.Dict{"modified": "meta"}},
	}
	recvType[*wamp.Result](t, admin)

	authz.unblock()
	recvType[*wamp.Registered](t, cli)

	details := sessionDetails(t, admin, 2, cli.ID)
	require.Equal(t, true, details["authorized"], "change by Authorizer lost")
	require.Equal(t, "meta", details["modified"], "concurrent change lost")
}

// nestedAuthz counts the messages of a session in a nested dict of its
// details. As Authorizer requires, it replaces the nested dict with a changed
// copy, instead of changing it in place.
type nestedAuthz struct{}

func (nestedAuthz) Authorize(sess *wamp.Session, msg wamp.Message) (bool, error) {
	nested := maps.Clone(wamp.DictChild(sess.Details, "nested"))
	if nested == nil {
		nested = wamp.Dict{}
	}
	n, _ := wamp.AsInt64(nested["count"])
	nested["count"] = n + 1
	sess.Details["nested"] = nested
	return true, nil
}

// TestAuthorizeNestedDetailChanges checks that an Authorizer that replaces a
// nested dict does not race with readers of the session details, and that its
// changes are applied.
func TestAuthorizeNestedDetailChanges(t *testing.T) {
	r := newAuthzRouter(t, nestedAuthz{}, false)
	cli := testClient(t, r)
	reader := testClient(t, r)

	const calls = 50
	readErr := make(chan error, 1)
	go func() {
		for i := range calls {
			reader.Send() <- &wamp.Call{
				Request:   wamp.ID(1 + i),
				Procedure: wamp.MetaProcSessionGet,
				Arguments: wamp.List{cli.ID},
			}
			msg, err := wamp.RecvTimeout(reader, time.Second)
			if err == nil {
				if _, ok := msg.(*wamp.Result); !ok {
					err = fmt.Errorf("expected RESULT, got %T", msg)
				}
			}
			if err != nil {
				readErr <- err
				return
			}
		}
		readErr <- nil
	}()
	for i := range calls {
		cli.Send() <- &wamp.Subscribe{Request: wamp.ID(1 + i), Topic: testTopic}
		recvType[*wamp.Subscribed](t, cli)
	}
	require.NoError(t, <-readErr)

	// The reader's own calls were authorized too, but they changed the reader's
	// session, not cli's.
	nested, ok := wamp.AsDict(sessionDetails(t, reader, 1000, cli.ID)["nested"])
	require.True(t, ok, "nested detail missing")
	count, _ := wamp.AsInt64(nested["count"])
	require.Equal(t, int64(calls), count)
}

// replaceDetailsAuthz assigns new details to the session.
type replaceDetailsAuthz struct{}

func (replaceDetailsAuthz) Authorize(sess *wamp.Session, msg wamp.Message) (bool, error) {
	sess.Details = nil
	return true, nil
}

// TestAuthorizeReplacedDetailsIgnored checks that an Authorizer that assigns a
// new map to the session's Details does not change the session's details.
func TestAuthorizeReplacedDetailsIgnored(t *testing.T) {
	r := newAuthzRouter(t, replaceDetailsAuthz{}, false)
	cli := testClient(t, r)
	reader := testClient(t, r)

	cli.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
	recvType[*wamp.Subscribed](t, cli)

	details := sessionDetails(t, reader, 1, cli.ID)
	require.Equal(t, cli.Details["authid"], details["authid"])
	require.Equal(t, cli.Details["authrole"], details["authrole"])
}

// TestApplyDetailChanges checks which changes from before to after are
// applied, and that the details of the session that were not changed from
// before to after keep their current values.
func TestApplyDetailChanges(t *testing.T) {
	nested := wamp.Dict{"a": 1}
	list := wamp.List{1, 2}
	fn := func() {}
	before := wamp.Dict{
		"same":    "x",
		"nested":  nested,
		"list":    list,
		"fn":      fn,
		"nan":     math.NaN(),
		"changed": 1,
		"removed": true,
		"keep":    "old",
	}
	after := maps.Clone(before)
	after["changed"] = 2
	after["added"] = "new"
	delete(after, "removed")
	after["replaced"] = wamp.Dict{"b": 2}

	sess := &wamp.Session{Details: maps.Clone(before)}
	// Changed meanwhile by someone else, so must be kept.
	sess.Details["keep"] = "meanwhile"
	sess.Details["nan"] = "meanwhile"
	sess.Details["fn"] = "meanwhile"
	sess.Details["nested"] = "meanwhile"
	sess.Details["list"] = "meanwhile"

	applyDetailChanges(sess, before, after)

	require.Equal(t, wamp.Dict{
		"same":     "x",
		"nested":   "meanwhile",
		"list":     "meanwhile",
		"fn":       "meanwhile",
		"nan":      "meanwhile",
		"changed":  2,
		"keep":     "meanwhile",
		"added":    "new",
		"replaced": wamp.Dict{"b": 2},
	}, sess.Details)

	// A nested value replaced with a new one is applied, even if it is equal.
	after = maps.Clone(before)
	after["nested"] = wamp.Dict{"a": 1}
	after["list"] = append(list, 3)
	sess = &wamp.Session{Details: wamp.Dict{}}
	applyDetailChanges(sess, before, after)
	require.Equal(t, wamp.Dict{"nested": wamp.Dict{"a": 1}, "list": wamp.List{1, 2, 3}}, sess.Details)
}
