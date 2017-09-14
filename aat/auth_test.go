package aat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/auth"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

func TestJoinRealmWithCRAuth(t *testing.T) {
	defer leaktest.Check(t)()

	cfg := client.ClientConfig{
		Realm: testAuthRealm,
		HelloDetails: wamp.Dict{
			"username": "jdoe",
		},
		AuthHandlers: map[string]client.AuthFunc{
			"testauth": testAuthFunc,
		},
	}

	cli, err := connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	realmDetails := cli.RealmDetails()
	if wamp.OptionString(realmDetails, "authrole") != "user" {
		t.Fatal("missing or incorrect authrole")
	}

	cli.Close()
}

func TestJoinRealmWithCRAuthBad(t *testing.T) {
	defer leaktest.Check(t)()

	cfg := client.ClientConfig{
		Realm: testAuthRealm,
		HelloDetails: wamp.Dict{
			"username": "malory",
		},
		AuthHandlers: map[string]client.AuthFunc{
			"testauth": testAuthFunc,
		},
	}

	cli, err := connectClientCfg(cfg)
	if err == nil {
		t.Fatal("expected error with bad username")
	}
	if !strings.HasSuffix(err.Error(), "invalid signature") {
		t.Fatal("wrong error message:", err)
	}
	if cli != nil {
		t.Fatal("Client should be nil on connect error")
	}
}

func TestAuthz(t *testing.T) {
	defer leaktest.Check(t)()

	cfg := client.ClientConfig{
		Realm: testAuthRealm,
	}

	// Connect subscriber session.
	subscriber, err := connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Connect caller.
	caller, err := connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Check that subscriber does not have special info provided by authorizer.
	ctx := context.Background()
	args := wamp.List{subscriber.ID()}
	result, err := caller.Call(ctx, metaGet, nil, args, nil, "")
	if err != nil {
		t.Fatal("Call error:", err)
	}
	dict, _ := wamp.AsDict(result.Arguments[0])
	if wamp.OptionString(dict, "foobar") != "" {
		t.Fatal("Should not have special info in session")
	}

	// Subscribe to event.
	gotEvent := make(chan struct{})
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		arg, _ := wamp.AsString(args[0])
		if arg != "hi" {
			return
		}
		close(gotEvent)
	}
	err = subscriber.Subscribe("nexus.interceptor", evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Check that authorizer modified session with special info.
	result, err = caller.Call(ctx, metaGet, nil, args, nil, "")
	if err != nil {
		t.Fatal("Call error:", err)
	}
	dict, _ = wamp.AsDict(result.Arguments[0])
	if wamp.OptionString(dict, "foobar") != "baz" {
		t.Fatal("Missing special info in session")
	}

	// Publish an event to something that matches by wildcard.
	caller.Publish("nexus.interceptor.foobar.baz", nil, wamp.List{"hi"}, nil)
	// Make sure the event was received.
	select {
	case <-gotEvent:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get published event")
	}

	// Have caller call a procedure that will fail authz.
	_, err = caller.Call(ctx, "need.ldap.auth",
		wamp.Dict{wamp.OptAcknowledge: true}, nil, nil, "")
	if err == nil {
		t.Fatal("Expected error")
	}

	subscriber.Close()
	caller.Close()
}

func testAuthFunc(d wamp.Dict, c wamp.Dict) (string, wamp.Dict) {
	ch := wamp.OptionString(c, "challenge")
	return testCRSign(ch), wamp.Dict{}
}

type testCRAuthenticator struct{}

// pendingTestAuth implements the PendingCRAuth interface.
type pendingTestAuth struct {
	authID string
	secret string
	role   string
}

func (t *testCRAuthenticator) Challenge(details wamp.Dict) (auth.PendingCRAuth, error) {
	username := wamp.OptionString(details, "username")
	if username == "" {
		return nil, errors.New("no username given")
	}
	var secret string
	if username == "jdoe" {
		secret = testCRSign(username)
	}

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
		Extra:      wamp.Dict{"challenge": p.authID},
	}
}

func (p *pendingTestAuth) Timeout() time.Duration { return time.Second }

func (p *pendingTestAuth) Authenticate(msg *wamp.Authenticate) (*wamp.Welcome, error) {

	if p.secret == "" || p.secret != msg.Signature {
		return nil, errors.New("invalid signature")
	}

	// Create welcome details containing auth info.
	details := wamp.Dict{
		"authid":     p.authID,
		"authmethod": "testauth",
		"authrole":   p.role,
	}

	return &wamp.Welcome{Details: details}, nil
}
