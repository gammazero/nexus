package router

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"maps"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

// testSessionDetails returns session details with standard and non-standard
// entries, and a transport detail that has auth.
func testSessionDetails() wamp.Dict {
	return wamp.Dict{
		"session":      wamp.ID(123),
		"authid":       "user1",
		"authrole":     "user",
		"authmethod":   "ticket",
		"authprovider": "static",
		"transport": wamp.Dict{
			"type": "websocket",
			"auth": wamp.Dict{"cookie": "secret"},
		},
		"xyzzy": "plugh",
		"foo":   "bar",
	}
}

func TestCleanSessionDetails(t *testing.T) {
	r := &realm{}
	details := testSessionDetails()

	// All details are kept, except transport.auth.
	clean := r.cleanSessionDetails(details)
	expect := testSessionDetails()
	expect["transport"] = wamp.Dict{"type": "websocket"}
	require.Equal(t, expect, clean)

	// The session details are not changed.
	require.Equal(t, testSessionDetails(), details)

	// The result is a copy, so changing it or the details does not change the
	// other.
	clean["added"] = true
	delete(clean, "xyzzy")
	clean["transport"].(wamp.Dict)["added"] = true
	require.Equal(t, testSessionDetails(), details)
	details["later"] = true
	details["transport"].(wamp.Dict)["later"] = true
	require.NotContains(t, clean, "later")
	require.NotContains(t, clean["transport"], "later")
}

// TestCleanSessionDetailsDeepCopy checks that dicts and lists in the details
// are copied too, so that a later change to the session's details does not
// change the result.
func TestCleanSessionDetailsDeepCopy(t *testing.T) {
	r := &realm{}
	authextra := wamp.Dict{"pubkey": "abc"}
	roles := wamp.List{"caller", wamp.Dict{"callee": true}}
	transport := wamp.Dict{"type": "rawsocket", "info": wamp.Dict{"x": 1}}
	details := wamp.Dict{
		"authextra": authextra,
		"roles":     roles,
		"transport": transport,
	}

	clean := r.cleanSessionDetails(details)
	require.Equal(t, details, clean)

	authextra["pubkey"] = "changed"
	roles[1].(wamp.Dict)["callee"] = false
	transport["type"] = "websocket"
	transport["info"].(wamp.Dict)["x"] = 2
	require.Equal(t, wamp.Dict{
		"authextra": wamp.Dict{"pubkey": "abc"},
		"roles":     wamp.List{"caller", wamp.Dict{"callee": true}},
		"transport": wamp.Dict{"type": "rawsocket", "info": wamp.Dict{"x": 1}},
	}, clean)
}

func TestCleanSessionDetailsStrict(t *testing.T) {
	r := &realm{
		metaStrict:     true,
		metaIncDetails: []string{"xyzzy", "missing"},
	}
	details := testSessionDetails()

	// Only the standard details and the included details are kept, and
	// transport.auth is removed. Included details that the session does not have
	// are not added.
	clean := r.cleanSessionDetails(details)
	require.Equal(t, wamp.Dict{
		"session":      wamp.ID(123),
		"authid":       "user1",
		"authrole":     "user",
		"authmethod":   "ticket",
		"authprovider": "static",
		"transport":    wamp.Dict{"type": "websocket"},
		"xyzzy":        "plugh",
	}, clean)
	require.Equal(t, testSessionDetails(), details)

	// Standard details that the session does not have are not added.
	clean = r.cleanSessionDetails(wamp.Dict{"authid": "user1", "foo": "bar"})
	require.Equal(t, wamp.Dict{"authid": "user1"}, clean)
}

func TestCleanSessionDetailsTransport(t *testing.T) {
	r := &realm{}

	// No transport detail.
	clean := r.cleanSessionDetails(wamp.Dict{"authid": "user1"})
	require.Equal(t, wamp.Dict{"authid": "user1"}, clean)

	// A transport detail without auth is kept.
	clean = r.cleanSessionDetails(wamp.Dict{
		"transport": wamp.Dict{"type": "rawsocket"},
	})
	require.Equal(t, wamp.Dict{"transport": wamp.Dict{"type": "rawsocket"}}, clean)

	// An auth that is not a dict is removed too.
	clean = r.cleanSessionDetails(wamp.Dict{
		"transport": wamp.Dict{"type": "rawsocket", "auth": "secret"},
	})
	require.Equal(t, wamp.Dict{"transport": wamp.Dict{"type": "rawsocket"}}, clean)

	// A transport detail of another map type has auth removed.
	details := wamp.Dict{
		"transport": map[string]any{
			"type": "websocket",
			"auth": map[string]any{"cookie": "secret"},
		},
	}
	clean = r.cleanSessionDetails(details)
	require.Equal(t, wamp.Dict{"type": "websocket"}, clean["transport"])
	require.Contains(t, details["transport"], "auth")

	// A transport detail that has only auth is left out.
	clean = r.cleanSessionDetails(wamp.Dict{
		"authid":    "user1",
		"transport": wamp.Dict{"auth": wamp.Dict{"cookie": "secret"}},
	})
	require.Equal(t, wamp.Dict{"authid": "user1"}, clean)

	// The same in strict mode.
	strict := &realm{metaStrict: true}
	clean = strict.cleanSessionDetails(wamp.Dict{
		"authid":    "user1",
		"transport": wamp.Dict{"auth": wamp.Dict{"cookie": "secret"}},
	})
	require.Equal(t, wamp.Dict{"authid": "user1"}, clean)
}

// syncBuffer is a bytes.Buffer that is safe to write from several goroutines,
// as the router's logger does.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestRealmDebug checks that RealmConfig.Debug enables debug logging for only
// its realm, and that Config.Debug enables it for every realm.
func TestRealmDebug(t *testing.T) {
	for _, routerDebug := range []bool{false, true} {
		var logs syncBuffer
		config := &Config{
			RealmConfigs: []*RealmConfig{
				{URI: testRealm, AnonymousAuth: true, Debug: true},
				{URI: testRealm2, AnonymousAuth: true},
			},
			Debug: routerDebug,
		}
		r, err := NewRouter(config, log.New(&logs, "", 0))
		require.NoError(t, err)

		for uri, want := range map[wamp.URI]bool{testRealm: true, testRealm2: routerDebug} {
			rlm := r.(*router).realms[uri]
			require.Equal(t, want, rlm.debug, "realm %s, router debug %v", uri, routerDebug)
			require.Equal(t, want, rlm.broker.debug, "broker %s, router debug %v", uri, routerDebug)
			require.Equal(t, want, rlm.dealer.debug, "dealer %s, router debug %v", uri, routerDebug)

			// The realm's debug logging shows in the router's log.
			cli := testClientInRealm(t, r, uri)
			attached := fmt.Sprintf("Finished attaching session: %d", cli.ID)
			require.Equal(t, want, strings.Contains(logs.String(), attached),
				"debug log for realm %s, router debug %v", uri, routerDebug)
			cli.Close()
		}
		// Config.Debug does not change the caller's realm configs.
		require.False(t, config.RealmConfigs[1].Debug)
		r.Close()
	}
}

// requireAbortedLast reads the messages for cli until the router closes it,
// and fails unless the last one is ABORT wamp.error.protocol_violation.
func requireAbortedLast(t *testing.T, cli *wamp.Session) {
	t.Helper()
	var last wamp.Message
	for msg := range cli.Recv() {
		last = msg
	}
	abort, ok := last.(*wamp.Abort)
	require.True(t, ok, "last message is %T, not ABORT", last)
	require.Equal(t, wamp.ErrProtocolViolation, abort.Reason)
}

// TestAbortedLocalSession checks that a local session that the broker or
// dealer aborts for a protocol violation is ended by the realm, so that it is
// removed from the broker and dealer before its peer is closed. The broker
// and dealer used to close the peer themselves while the session was still
// subscribed, so that the next event to it panicked the router with "send on
// closed channel". The ABORT must also be the last message the session gets.
func TestAbortedLocalSession(t *testing.T) {
	const proc = wamp.URI("nexus.test.abortproc")
	pptOptions := wamp.Dict{wamp.OptPPTScheme: "x_test", wamp.OptPPTSerializer: "native"}
	pptCallee := wamp.Dict{"roles": wamp.Dict{"callee": wamp.Dict{"features": wamp.Dict{
		wamp.FeaturePayloadPassthruMode: true,
	}}}}
	pptCaller := wamp.Dict{"roles": wamp.Dict{"caller": wamp.Dict{"features": wamp.Dict{
		wamp.FeaturePayloadPassthruMode: true,
	}}}}

	cases := []struct {
		name string
		// violate makes cli violate the protocol, using other clients of r if
		// it needs to.
		violate func(t *testing.T, r Router, cli *wamp.Session)
	}{
		{"publish with undeclared payload passthru", func(t *testing.T, r Router, cli *wamp.Session) {
			cli.Send() <- &wamp.Publish{Request: 2, Topic: "nexus.test.other", Options: pptOptions}
		}},
		{"undeclared progressive call", func(t *testing.T, r Router, cli *wamp.Session) {
			callee := testClient(t, r)
			callee.Send() <- &wamp.Register{Request: 1, Procedure: proc}
			recvType[*wamp.Registered](t, callee)
			cli.Send() <- &wamp.Call{Request: 2, Procedure: proc, Options: wamp.Dict{wamp.OptProgress: true}}
		}},
		{"call with undeclared payload passthru", func(t *testing.T, r Router, cli *wamp.Session) {
			callee := joinWithRoles(t, r, pptCallee)
			callee.Send() <- &wamp.Register{Request: 1, Procedure: proc}
			recvType[*wamp.Registered](t, callee)
			cli.Send() <- &wamp.Call{Request: 2, Procedure: proc, Options: pptOptions}
		}},
		{"yield with undeclared payload passthru", func(t *testing.T, r Router, cli *wamp.Session) {
			cli.Send() <- &wamp.Register{Request: 2, Procedure: proc}
			recvType[*wamp.Registered](t, cli)
			caller := joinWithRoles(t, r, pptCaller)
			caller.Send() <- &wamp.Call{Request: 1, Procedure: proc}
			inv := recvType[*wamp.Invocation](t, cli)
			cli.Send() <- &wamp.Yield{Request: inv.Request, Options: pptOptions}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newTestRouter(t)
				cli := testClient(t, r)
				cli.Send() <- &wamp.Subscribe{Request: 1, Topic: testTopic}
				recvType[*wamp.Subscribed](t, cli)

				tc.violate(t, r, cli)
				synctest.Wait()

				// An event for the aborted session's subscription.
				pub := testClient(t, r)
				pub.Send() <- &wamp.Publish{
					Request: 1,
					Topic:   testTopic,
					Options: wamp.Dict{wamp.OptAcknowledge: true},
				}
				recvType[*wamp.Published](t, pub)

				requireAbortedLast(t, cli)
			})
		})
	}
}

// metaInvocation returns an INVOCATION of a meta procedure, called by the
// session with ID caller, with the arguments.
func metaInvocation(caller wamp.ID, args wamp.List, kwargs wamp.Dict) *wamp.Invocation {
	return &wamp.Invocation{
		Request:     1,
		Details:     wamp.Dict{"caller": caller},
		Arguments:   args,
		ArgumentsKw: kwargs,
	}
}

// requireMetaError checks that rsp is the ERROR, with the error URI, for an
// INVOCATION made by metaInvocation.
func requireMetaError(t *testing.T, rsp wamp.Message, uri wamp.URI) {
	t.Helper()
	errMsg, ok := rsp.(*wamp.Error)
	require.True(t, ok, "expected ERROR, got %#v", rsp)
	require.Equal(t, uri, errMsg.Error)
	require.Equal(t, wamp.INVOCATION, errMsg.Type)
	require.Equal(t, wamp.ID(1), errMsg.Request)
}

// requireYield checks that rsp is the YIELD for an INVOCATION made by
// metaInvocation.
func requireYield(t *testing.T, rsp wamp.Message) {
	t.Helper()
	require.Equal(t, &wamp.Yield{Request: 1}, rsp)
}

func TestKillSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		rlm := r.(*router).realms[testRealm]

		require.EqualError(t, rlm.killSession(wamp.GlobalID(), "", ""),
			"no such session")
		// The meta session is not one of the clients, so it cannot be killed.
		require.EqualError(t, rlm.killSession(rlm.metaSess.ID, "", ""),
			"no such session")

		// The session gets a GOODBYE with the reason and message.
		cli := testClient(t, r)
		require.NoError(t, rlm.killSession(cli.ID, "test.reason", "bye"))
		goodbye := recvType[*wamp.Goodbye](t, cli)
		require.Equal(t, wamp.URI("test.reason"), goodbye.Reason)
		require.Equal(t, wamp.Dict{"message": "bye"}, goodbye.Details)

		// With no reason, the reason is wamp.close.normal, and with no message the
		// details have no message.
		cli = testClient(t, r)
		require.NoError(t, rlm.killSession(cli.ID, "", ""))
		goodbye = recvType[*wamp.Goodbye](t, cli)
		require.Equal(t, wamp.CloseNormal, goodbye.Reason)
		require.Empty(t, goodbye.Details)
	})
}

func TestSessionKillArgs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		rlm := r.(*router).realms[testRealm]
		caller := testClient(t, r)
		target := testClient(t, r)

		for name, inv := range map[string]*wamp.Invocation{
			"no session ID":    metaInvocation(caller.ID, nil, nil),
			"bad session ID":   metaInvocation(caller.ID, wamp.List{"x"}, nil),
			"caller's own ID":  metaInvocation(caller.ID, wamp.List{caller.ID}, nil),
			"unknown session":  metaInvocation(caller.ID, wamp.List{wamp.GlobalID()}, nil),
			"meta session":     metaInvocation(caller.ID, wamp.List{rlm.metaSess.ID}, nil),
			"zero session ID":  metaInvocation(caller.ID, wamp.List{0}, nil),
			"negative session": metaInvocation(caller.ID, wamp.List{-1}, nil),
		} {
			requireMetaError(t, rlm.sessionKill(inv), wamp.ErrNoSuchSession)
			synctest.Wait()
			require.Empty(t, target.Recv(), "%s: target killed", name)
		}

		inv := metaInvocation(caller.ID, wamp.List{target.ID},
			wamp.Dict{"reason": "not a valid uri"})
		requireMetaError(t, rlm.sessionKill(inv), wamp.ErrInvalidURI)

		inv = metaInvocation(caller.ID, wamp.List{target.ID},
			wamp.Dict{"reason": "test.reason", "message": "bye"})
		requireYield(t, rlm.sessionKill(inv))
		goodbye := recvType[*wamp.Goodbye](t, target)
		require.Equal(t, wamp.URI("test.reason"), goodbye.Reason)
		require.Equal(t, wamp.Dict{"message": "bye"}, goodbye.Details)
	})
}

func TestSessionModifyDetailsArgs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		rlm := r.(*router).realms[testRealm]
		caller := testClient(t, r)
		target := testClient(t, r)
		delta := wamp.Dict{"color": "red"}

		for name, tc := range map[string]struct {
			args wamp.List
			uri  wamp.URI
		}{
			"no arguments":    {nil, wamp.ErrInvalidArgument},
			"no delta":        {wamp.List{target.ID}, wamp.ErrInvalidArgument},
			"bad session ID":  {wamp.List{"x", delta}, wamp.ErrInvalidArgument},
			"delta not dict":  {wamp.List{target.ID, "red"}, wamp.ErrInvalidArgument},
			"changes session": {wamp.List{target.ID, wamp.Dict{"session": 1}}, wamp.ErrInvalidArgument},
			"meta session":    {wamp.List{rlm.metaSess.ID, delta}, wamp.ErrNoSuchSession},
			"unknown session": {wamp.List{wamp.GlobalID(), delta}, wamp.ErrNoSuchSession},
		} {
			rsp := rlm.sessionModifyDetails(metaInvocation(caller.ID, tc.args, nil))
			requireMetaError(t, rsp, tc.uri)
			details := sessionDetailsOf(rlm, target.ID)
			require.NotContains(t, details, "color", name)
			require.Equal(t, target.ID, details["session"], name)
		}

		rsp := rlm.sessionModifyDetails(
			metaInvocation(caller.ID, wamp.List{target.ID, delta}, nil))
		requireYield(t, rsp)
		require.Equal(t, "red", sessionDetailsOf(rlm, target.ID)["color"])
	})
}

// sessionDetailsOf returns a copy of the details of the realm's session with
// ID sid, or nil if the realm has no such session.
func sessionDetailsOf(rlm *realm, sid wamp.ID) wamp.Dict {
	var details wamp.Dict
	done := make(chan struct{})
	rlm.actionChan <- func() {
		if sess, ok := rlm.clients[sid]; ok {
			sess.Lock()
			details = maps.Clone(sess.Details)
			sess.Unlock()
		}
		close(done)
	}
	<-done
	return details
}

func TestTestamentAddArgs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		rlm := r.(*router).realms[testRealm]
		cli := testClient(t, r)
		args := wamp.List{"testament.topic", wamp.List{"arg"}, wamp.Dict{}}

		for name, inv := range map[string]*wamp.Invocation{
			"no caller":      {Request: 1, Arguments: args},
			"too few args":   metaInvocation(cli.ID, args[:2], nil),
			"bad topic":      metaInvocation(cli.ID, wamp.List{5, wamp.List{}, wamp.Dict{}}, nil),
			"args not list":  metaInvocation(cli.ID, wamp.List{"testament.topic", "arg", wamp.Dict{}}, nil),
			"kwargs not map": metaInvocation(cli.ID, wamp.List{"testament.topic", wamp.List{}, "kw"}, nil),
			"bad scope":      metaInvocation(cli.ID, args, wamp.Dict{"scope": "bogus"}),
		} {
			requireMetaError(t, rlm.testamentAdd(inv), wamp.ErrInvalidArgument)
			_, _, ok := testamentArgs(rlm, cli.ID)
			require.False(t, ok, "%s: testament added", name)
		}

		// With no scope, or an empty one, the testament is for when the session is
		// destroyed. Publish options are kept, and options that are not a dict are
		// replaced by empty options.
		requireYield(t, rlm.testamentAdd(metaInvocation(cli.ID, args,
			wamp.Dict{"publish_options": wamp.Dict{"acknowledge": true}})))
		requireYield(t, rlm.testamentAdd(metaInvocation(cli.ID,
			wamp.List{"testament.topic", wamp.List{"arg2"}, wamp.Dict{}},
			wamp.Dict{"scope": "", "publish_options": "not a dict"})))
		detached, destroyed, ok := testamentArgs(rlm, cli.ID)
		require.True(t, ok)
		require.Nil(t, detached)
		require.Equal(t, []any{"arg", "arg2"}, destroyed)

		var options []wamp.Dict
		done := make(chan struct{})
		rlm.actionChan <- func() {
			for _, tm := range rlm.testaments[cli.ID].destroyed {
				options = append(options, tm.options)
			}
			close(done)
		}
		<-done
		require.Equal(t,
			[]wamp.Dict{{"acknowledge": true}, {}}, options)
	})
}

// TestWaitReplyRoom checks that waitReplyRoom returns at once when the session
// has room for replies, and returns true, without waiting for room, when the
// session's messages are no longer being received.
func TestWaitReplyRoom(t *testing.T) {
	r := &realm{log: logger}
	synctest.Test(t, func(t *testing.T) {
		peer := &testPeer{in: make(chan wamp.Message, 1)}
		sess := wamp.NewSession(peer, 0, nil, nil)
		defer sess.Close()

		// There is room, so it does not wait.
		require.True(t, r.waitReplyRoom(sess, nil))

		// Fill the backlog so there is no room.
		for i := 1; i <= 3; i++ {
			sess.SendReply(&wamp.Result{Request: wamp.ID(i)})
		}
		require.NotNil(t, sess.ReplyRoom())

		// The session stops receiving, so there is no need to wait.
		recvDone := make(chan struct{})
		close(recvDone)
		start := time.Now()
		require.True(t, r.waitReplyRoom(sess, recvDone))
		require.Zero(t, time.Since(start), "waited for room")
	})
}

// resultAuthz is an Authorizer that returns allow and err, and counts its
// calls.
type resultAuthz struct {
	allow bool
	err   error
	calls int
}

func (a *resultAuthz) Authorize(*wamp.Session, wamp.Message) (bool, error) {
	a.calls++
	return a.allow, a.err
}

func TestAuthzMessage(t *testing.T) {
	newSession := func() (*wamp.Session, *testPeer) {
		peer := &testPeer{in: make(chan wamp.Message, 4)}
		return wamp.NewSession(peer, 1, wamp.Dict{}, nil), peer
	}
	// recvError returns the ERROR sent to the peer, or nil if none was sent.
	recvError := func(t *testing.T, peer *testPeer) *wamp.Error {
		t.Helper()
		synctest.Wait()
		select {
		case msg := <-peer.in:
			errMsg, ok := msg.(*wamp.Error)
			require.True(t, ok, "expected ERROR, got %#v", msg)
			return errMsg
		default:
			return nil
		}
	}

	denied := []wamp.Message{
		&wamp.Publish{Request: 1, Options: wamp.Dict{wamp.OptAcknowledge: true}},
		&wamp.Subscribe{Request: 2},
		&wamp.Unsubscribe{Request: 3},
		&wamp.Register{Request: 4},
		&wamp.Unregister{Request: 5},
		&wamp.Call{Request: 6},
		&wamp.Cancel{Request: 7},
		&wamp.Yield{Request: 8},
	}

	synctest.Test(t, func(t *testing.T) {
		// A message that is not authorized gets an ERROR for its request, with no
		// reason given.
		authz := &resultAuthz{}
		r := &realm{log: logger, authorizer: authz, localAuthz: true}
		for i, msg := range denied {
			sess, peer := newSession()
			require.False(t, r.authzMessage(sess, msg), "%T authorized", msg)
			errMsg := recvError(t, peer)
			require.NotNil(t, errMsg, "no ERROR for %T", msg)
			require.Equal(t, &wamp.Error{
				Type:    msg.MessageType(),
				Request: wamp.ID(i + 1),
				Details: wamp.Dict{},
				Error:   wamp.ErrNotAuthorized,
			}, errMsg)
			sess.Close()
		}

		// An error from the Authorizer is in the ERROR.
		authz.err = errors.New("cannot reach auth service")
		sess, peer := newSession()
		require.False(t, r.authzMessage(sess, &wamp.Call{Request: 9}))
		require.Equal(t, &wamp.Error{
			Type:      wamp.CALL,
			Request:   9,
			Details:   wamp.Dict{},
			Error:     wamp.ErrAuthorizationFailed,
			Arguments: wamp.List{"cannot reach auth service"},
		}, recvError(t, peer))
		sess.Close()

		// A PUBLISH without acknowledge gets no ERROR.
		authz.err = nil
		sess, peer = newSession()
		require.False(t, r.authzMessage(sess, &wamp.Publish{Request: 10}))
		require.Nil(t, recvError(t, peer), "ERROR for PUBLISH without acknowledge")
		sess.Close()

		// An authorized message gets no ERROR.
		authz.allow = true
		sess, peer = newSession()
		require.True(t, r.authzMessage(sess, &wamp.Call{Request: 11}))
		require.Nil(t, recvError(t, peer))
		sess.Close()

		// A local session is not authorized unless the realm requires it.
		authz.allow, authz.calls = false, 0
		r.localAuthz = false
		sess, peer = newSession()
		require.True(t, r.authzMessage(sess, &wamp.Call{Request: 12}))
		require.Zero(t, authz.calls, "local session authorized")
		require.Nil(t, recvError(t, peer))
		sess.Close()

		// A remote session is checked even when local sessions are not.
		remote := wamp.NewSession(remoteTestPeer{&testPeer{in: make(chan wamp.Message, 1)}}, 1, wamp.Dict{}, nil)
		require.False(t, r.authzMessage(remote, &wamp.Call{Request: 13}))
		require.Equal(t, 1, authz.calls)
		remote.Close()
	})
}

func TestAuthClient(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newTestRouter(t)
		rlm := r.(*router).realms[testRealm]
		remote := remoteTestPeer{newTestPeer()}
		roles := wamp.Dict{
			wamp.RoleBroker: rlm.broker.role(),
			wamp.RoleDealer: rlm.dealer.role(),
		}

		// With no authmethods, anonymous is used. Authmethods that are not strings, or
		// are empty, are skipped.
		for _, details := range []wamp.Dict{
			{},
			{"authmethods": wamp.List{}},
			{"authmethods": wamp.List{5, "", "anonymous"}},
		} {
			welcome, err := rlm.authClient(1, remote, details)
			require.NoError(t, err, "details %v", details)
			require.Equal(t, "anonymous", welcome.Details["authmethod"])
			require.Equal(t, roles, welcome.Details["roles"])
		}

		_, err := rlm.authClient(1, remote, wamp.Dict{
			"authmethods": wamp.List{5, ""},
		})
		require.EqualError(t, err, "no authentication supplied")

		_, err = rlm.authClient(1, remote, wamp.Dict{
			"authmethods": wamp.List{"ticket", "wampcra"},
		})
		require.EqualError(t, err, "could not authenticate with any method")

		// A local client is trusted without authentication, unless the realm requires
		// local authentication.
		local := newTestPeer()
		welcome, err := rlm.authClient(1, local, wamp.Dict{
			"authid":      "alice",
			"authmethods": wamp.List{"ticket"},
		})
		require.NoError(t, err)
		require.Equal(t, wamp.Dict{
			"authid":       "alice",
			"authrole":     "trusted",
			"authmethod":   "local",
			"authprovider": "static",
			"roles":        roles,
		}, welcome.Details)

		// A local client without an authid is given one.
		welcome, err = rlm.authClient(1, local, wamp.Dict{})
		require.NoError(t, err)
		authid, _ := wamp.AsString(welcome.Details["authid"])
		require.NotEmpty(t, authid)

		rlm.localAuth = true
		_, err = rlm.authClient(1, local, wamp.Dict{
			"authmethods": wamp.List{"ticket"},
		})
		require.EqualError(t, err, "could not authenticate with any method")
	})
}
