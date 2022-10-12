package aat

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gammazero/nexus/v3/wamp/crsign"
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

	cfg := client.Config{
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
	if s, _ := wamp.AsString(realmDetails["authrole"]); s != "user" {
		t.Fatal("missing or incorrect authrole")
	}

	cli.Close()
}

func TestJoinRealmWithCRCookieAuth(t *testing.T) {
	cfg := client.Config{
		Realm: testAuthRealm,
		HelloDetails: wamp.Dict{
			"authid": "jdoe",
		},
		AuthHandlers: map[string]client.AuthFunc{
			"wampcra": clientAuthFunc,
		},
		ResponseTimeout: time.Second,
	}

	if scheme == "ws" || scheme == "wss" {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal("failed to create CookieJar:", err)
		}
		cfg.WsCfg.Jar = jar
	}

	cli, err := connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	details := cli.RealmDetails()
	cli.Close()

	// Client should not have be authenticated by cookie first time.
	if ok, _ := wamp.AsBool(details["authbycookie"]); ok {
		t.Fatal("authbycookie set incorrectly to true")
	}

	cli, err = connectClientCfg(cfg)
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	details = cli.RealmDetails()
	cli.Close()

	// If websocket, then should be authenticated by cookie this time.
	if cfg.WsCfg.Jar != nil {
		if ok, _ := wamp.AsBool(details["authbycookie"]); !ok {
			t.Fatal("should have been authenticated by cookie")
		}
	} else {
		if ok, _ := wamp.AsBool(details["authbycookie"]); ok {
			t.Fatal("authbycookie set incorrectly to true")
		}
	}
}

func TestJoinRealmWithCRAuthBad(t *testing.T) {
	defer leaktest.Check(t)()

	cfg := client.Config{
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

	cfg := client.Config{
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
	result, err := caller.Call(ctx, metaGet, nil, args, nil, nil)
	if err != nil {
		t.Fatal("Call error:", err)
	}
	dict, _ := wamp.AsDict(result.Arguments[0])
	if _, ok := dict["foobar"]; ok {
		t.Fatal("Should not have special info in session")
	}

	// Subscribe to event.
	errChan := make(chan error)
	evtHandler := func(ev *wamp.Event) {
		arg, _ := wamp.AsString(ev.Arguments[0])
		if arg != "hi" {
			errChan <- errors.New("wrong argument in event")
			return
		}
		errChan <- nil
	}

	err = subscriber.Subscribe("nexus.interceptor", evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Check that authorizer modified session with special info.
	result, err = caller.Call(ctx, metaGet, nil, args, nil, nil)
	if err != nil {
		t.Fatal("Call error:", err)
	}
	dict, _ = wamp.AsDict(result.Arguments[0])
	if s, _ := wamp.AsString(dict["foobar"]); s != "baz" {
		t.Fatal("Missing special info in session")
	}

	// Publish an event to something that matches by wildcard.
	caller.Publish("nexus.interceptor.foobar.baz", nil, wamp.List{"hi"}, nil)
	// Make sure the event was received.
	select {
	case err = <-errChan:
		if err != nil {
			t.Fatalf("Event error: %s", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get published event")
	}

	// Have caller call a procedure that will fail authz.
	_, err = caller.Call(ctx, "need.ldap.auth",
		wamp.Dict{wamp.OptAcknowledge: true}, nil, nil, nil)
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
	provider     string
	cookie       *http.Cookie
	authByCookie bool
}

func (ks *serverKeyStore) AuthKey(authid, authmethod string) ([]byte, error) {
	if authid != "jdoe" {
		return nil, errors.New("no such user: " + authid)
	}
	switch authmethod {
	case "wampcra":
		// Lookup the user's key.
		secret := []byte(password)
		salt := []byte(pwSalt)
		dk := pbkdf2.Key([]byte(secret), salt, iterations, keylen, sha256.New)
		return []byte(base64.StdEncoding.EncodeToString(dk)), nil
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

func (ks *serverKeyStore) AlreadyAuth(authid string, details wamp.Dict) bool {
	// Verify that the request has been captured.
	_, err := wamp.DictValue(details, []string{"transport", "auth", "request"})
	if err != nil {
		// Not authorized - pretend request is needed for some reason.
		return false
	}

	v, err := wamp.DictValue(details, []string{"transport", "auth", "cookie"})
	if err != nil {
		// No tracking cookie - client not recognized, so not auth.
		return false
	}
	cookie := v.(*http.Cookie)

	// Tracking cookie matches cookie of previously good client.
	if cookie.Value != ks.cookie.Value {
		// Did not have expected tracking cookie.
		fmt.Println("===> wrong cookie value", cookie, "!=", ks.cookie)
		return false
	}

	ks.authByCookie = true
	return true
}

func (ks *serverKeyStore) OnWelcome(authid string, welcome *wamp.Welcome, details wamp.Dict) error {
	v, err := wamp.DictValue(details, []string{"transport", "auth", "nextcookie"})
	if err != nil {
		// Tracking cookie not enabled.
		return nil
	}
	nextcookie := v.(*http.Cookie)

	// Update tracking cookie that will identify this authenticated client.
	ks.cookie = nextcookie

	// Tell the client whether or not it was allowed by its cookie.
	welcome.Details["authbycookie"] = ks.authByCookie
	return nil
}
