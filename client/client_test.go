package client

import (
	"errors"
	stdlog "log"
	"os"
	"testing"
	"time"

	"github.com/gammazero/nexus/auth"
	"github.com/gammazero/nexus/logger"
	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/wamp"
)

const (
	autoRealm = false
	strictURI = true
	anonAuth  = true
	disclose  = true

	testRealm = "nexus.test"
)

var log logger.Logger

func init() {
	log = stdlog.New(os.Stdout, "", stdlog.LstdFlags)
	router.SetLogger(log)
}

func getTestPeer(r router.Router) wamp.Peer {
	cli, rtr := router.LinkedPeers()
	go r.Attach(rtr)
	return cli
}

func connectedTestClients() (*Client, *Client, error) {
	r := router.NewRouter(autoRealm, strictURI)
	r.AddRealm(wamp.URI(testRealm), anonAuth, disclose)
	peer1 := getTestPeer(r)
	peer2 := getTestPeer(r)
	c1, err := newTestClient(peer1)
	if err != nil {
		return nil, nil, err
	}
	c2, err := newTestClient(peer2)
	if err != nil {
		return nil, nil, err
	}
	return c1, c2, nil
}

func newTestClient(p wamp.Peer) (*Client, error) {
	client := NewClient(p, 200*time.Millisecond, log)
	_, err := client.JoinRealm(testRealm, nil, nil)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func TestJoinRealm(t *testing.T) {
	r := router.NewRouter(autoRealm, strictURI)
	r.AddRealm(wamp.URI(testRealm), anonAuth, disclose)

	// Test that client can join realm.
	client := NewClient(getTestPeer(r), 0, log)
	_, err := client.JoinRealm("nexus.test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Test that client cannot join realm when anonymous auth is disabled.
	r.AddRealm("nexus.testnoanon", false, false)
	client = NewClient(getTestPeer(r), 0, log)
	if _, err = client.JoinRealm("nexus.testnoanon", nil, nil); err == nil {
		t.Fatal("expected error due to no anonymous authentication")
	}
}

func TestJoinRealmWithCRAuth(t *testing.T) {
	r := router.NewRouter(autoRealm, strictURI)
	realm, err := r.AddRealm(wamp.URI("nexus.test.auth"), false, false)
	if err != nil {
		t.Fatal(err)
	}
	crAuth := &testCRAuthenticator{}
	realm.AddCRAuthenticator("testauth", crAuth)

	peer := getTestPeer(r)
	client := NewClient(peer, 0, log)

	details := map[string]interface{}{
		"username": "jdoe", "authmethods": []string{"testauth"}}
	authMap := map[string]AuthFunc{"testauth": testAuthFunc}
	_, err = client.JoinRealm("nexus.test.auth", details, authMap)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSubscribe(t *testing.T) {
	// Connect to clients to the same server
	sub, pub, err := connectedTestClients()
	if err != nil {
		t.Fatal("failed to connect test clients: ", err)
	}

	testTopic := "nexus.test.topic"
	errChan := make(chan error)
	evtHandler := func(args []interface{}, kwargs map[string]interface{}, details map[string]interface{}) {
		if args[0].(string) != "hello world" {
			errChan <- errors.New("event missing or bad args")
			return
		}
		origTopic, ok := details["topic"]
		if !ok {
			errChan <- errors.New("missing original topic")
			return
		}

		var otopic string
		switch origTopic := origTopic.(type) {
		case wamp.URI:
			otopic = string(origTopic)
		case string:
			otopic = origTopic
		default:
			errChan <- errors.New("topic detail not a URI or string")
			return
		}
		if otopic != testTopic {
			errChan <- errors.New("wrong original topic")
			return
		}
		errChan <- nil
	}

	// Expect invalid URI error if not setting match option.
	wcTopic := "nexus..topic"
	err = sub.Subscribe(wcTopic, evtHandler, nil)
	if err == nil {
		t.Fatal("expected invalid uri error")
	}

	// Subscribe should work with match set. wildcard
	err = sub.Subscribe(wcTopic, evtHandler, wamp.SetOption(nil, "match", "wildcard"))
	if err != nil {
		t.Fatal("subscribe error: ", err)
	}

	// Publish an event to something that matches by wildcard.
	pub.Publish(testTopic, nil, []interface{}{"hello world"}, nil)

	// Make sure the event was received.
	select {
	case err = <-errChan:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get published event")
	}
	if err != nil {
		t.Fatal(err)
	}
	err = sub.Unsubscribe(wcTopic)
	if err != nil {
		t.Fatal("unsubscribe error: ", err)
	}
}

func TestRemoteProcedureCall(t *testing.T) {
	// Connect to clients to the same server
	callee, caller, err := connectedTestClients()
	if err != nil {
		t.Fatal("failed to connect test clients: ", err)
	}

	// Test unregister invalid procedure.
	if err = callee.Unregister("invalidmethod"); err == nil {
		t.Fatal("expected error unregistering invalid procedure")
	}

	// Test registering a valid procedure.
	handler := func(args []interface{}, kwargs, details map[string]interface{}, cancel <-chan struct{}) *InvokeResult {
		return &InvokeResult{Args: []interface{}{args[0].(int) * 37}}
	}
	procName := "myproc"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure: ", err)
	}

	// Test calling the procedure.
	callArgs := []interface{}{73}
	result, err := caller.Call(procName, nil, callArgs, nil, 0, "")
	if err != nil {
		t.Fatal("failed to call procedure: ", err)
	}
	if result.Arguments[0] != 2701 {
		t.Fatal("wrong result: ", result.Arguments)
	}

	// Test unregister.
	if err = callee.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure: ", err)
	}

	// Test calling unregistered procedure.
	callArgs = []interface{}{555}
	result, err = caller.Call(procName, nil, callArgs, nil, 0, "")
	if err == nil {
		t.Fatal("expected error calling unregistered procedure")
	}
	if result != nil {
		t.Fatal("result should be nil on error")
	}
}

func TestCancelRemoteProcedureCall(t *testing.T) {
	// Connect to clients to the same server
	callee, caller, err := connectedTestClients()
	if err != nil {
		t.Fatal("failed to connect test clients: ", err)
	}

	// Test registering a valid procedure.
	handler := func(args []interface{}, kwargs, details map[string]interface{}, cancel <-chan struct{}) *InvokeResult {
		<-cancel // handler will block forever until canceled.
		return &InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure: ", err)
	}

	errChan := make(chan error)

	// Calling the procedure, should block.
	go func() {
		callArgs := []interface{}{73}
		_, err := caller.Call(procName, nil, callArgs, nil, time.Second, "killnowait")
		errChan <- err
	}()

	// Make sure the call is blocked.
	select {
	case err = <-errChan:
		t.Fatal("call should have been blocked")
	case <-time.After(200 * time.Millisecond):
	}

	// Make sure the call is canceled.
	select {
	case err = <-errChan:
	case <-time.After(2 * time.Second):
		t.Fatal("call should have been canceled")
	}

	rpcError, ok := err.(RPCError)
	if !ok {
		t.Fatal("expected RPCError type of error")
	}
	if rpcError.Err.Error != wamp.ErrCanceled {
		t.Fatal("expected canceled error, got:", err)
	}
	if err = callee.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure: ", err)
	}
}

// ---- authentication test stuff ------
func testAuthFunc(d map[string]interface{}, c map[string]interface{}) (string, map[string]interface{}, error) {
	ch := c["challenge"].(string)
	return testCRSign(ch), map[string]interface{}{}, nil
}

type testCRAuthenticator struct{}

// pendingTestAuth implements the PendingCRAuth interface.
type pendingTestAuth struct {
	authID string
	secret string
	role   string
}

func (t *testCRAuthenticator) Challenge(details map[string]interface{}) (auth.PendingCRAuth, error) {
	var username string
	_username, ok := details["username"]
	if ok {
		username = _username.(string)
	}
	if username == "" {
		return nil, errors.New("no username given")
	}

	secret := testCRSign(username)

	return &pendingTestAuth{
		authID: username,
		role:   "user",
		secret: secret,
	}, nil
}

func testCRSign(uname string) string {
	return uname + "123xyz"
}

// Return the test challenge message.
func (p *pendingTestAuth) Msg() *wamp.Challenge {
	return &wamp.Challenge{
		AuthMethod: "testauth",
		Extra:      map[string]interface{}{"challenge": p.authID},
	}
}

func (p *pendingTestAuth) Timeout() time.Duration { return time.Second }

func (p *pendingTestAuth) Authenticate(msg *wamp.Authenticate) (*wamp.Welcome, error) {

	if p.secret != msg.Signature {
		return nil, errors.New("invalid signature")
	}

	// Create welcome details containing auth info.
	details := map[string]interface{}{
		"authid":     p.authID,
		"authmethod": "testauth",
		"authrole":   p.role,
	}

	return &wamp.Welcome{Details: details}, nil
}
