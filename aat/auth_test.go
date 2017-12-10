package aat

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
	"github.com/gammazero/nexus/wamp/crsign"
	"golang.org/x/crypto/pbkdf2"
)

const (
	password   = "squeemishosafradge"
	pwSalt     = "salt123"
	keylen     = 32
	iterations = 1000
)

func TestJoinRealmWithCRAuth(t *testing.T) {
	defer leaktest.Check(t)()

	cfg := client.ClientConfig{
		Realm: testAuthRealm,
		HelloDetails: wamp.Dict{
			"authid": "jdoe",
		},
		AuthHandlers: map[string]client.AuthFunc{
			"wampcra": clientAuthFunc,
		},
		ResponseTimeout: time.Second,
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
			"authid": "malory",
		},
		AuthHandlers: map[string]client.AuthFunc{
			"wampcra": clientAuthFunc,
		},
		ResponseTimeout: time.Second,
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
		Realm:           testAuthRealm,
		ResponseTimeout: time.Second,
	}

	// Connect subscriber session.
	subscriber, err := connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer subscriber.Close()

	// Connect caller.
	caller, err := connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer caller.Close()

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
	if !strings.HasSuffix(err.Error(), "wamp.error.authorization_failed: Cannot contact LDAP server") {
		t.Fatal("Did not get expected error message with reason, got:", err)
	}
}

func clientAuthFunc(c *wamp.Challenge) (string, wamp.Dict) {
	// Assume that client only operates as one user and knows the key to use.
	// password := askPassword(chStr)
	return crsign.RespondChallenge(password, c, nil), wamp.Dict{}
}

type serverKeyStore struct {
	provider string
}

func (ks *serverKeyStore) AuthKey(authid, authmethod string) ([]byte, error) {
	if authid != "jdoe" {
		return nil, errors.New("no such user: " + authid)
	}
	switch authmethod {
	case "wampcra":
		// Lookup the user'es key.
		pw := []byte(password)
		salt := []byte(pwSalt)
		return pbkdf2.Key(pw, salt, iterations, keylen, sha256.New), nil
	case "ticket":
		// Lookup the user's key.
		return []byte("ticketforjoe1234"), nil
	}
	return nil, nil
}

func (ks *serverKeyStore) PasswordInfo(authid string) (string, int, int) {
	return pwSalt, keylen, iterations
}

func (ks *serverKeyStore) Provider() string { return ks.provider }

func (ks *serverKeyStore) AuthRole(authid string) (string, error) {
	if authid != "jdoe" {
		return "", errors.New("no such user: " + authid)
	}
	return "user", nil
}
