package aat_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/dtegapp/nexus/v3/client"
	"github.com/dtegapp/nexus/v3/wamp"
	"github.com/dtegapp/nexus/v3/wamp/crsign"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/pbkdf2"
)

const (
	password   = "squeemishosafradge"
	pwSalt     = "salt123"
	keylen     = 32
	iterations = 1000
)

func TestJoinRealmWithCRAuth(t *testing.T) {
	checkGoLeaks(t)

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

	cli := connectClientCfg(t, cfg)

	realmDetails := cli.RealmDetails()
	s, _ := wamp.AsString(realmDetails["authrole"])
	require.Equal(t, "user", s, "missing or incorrect authrole")
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
		require.NoError(t, err, "failed to create CookieJar")
		cfg.WsCfg.Jar = jar
	}

	cli := connectClientCfg(t, cfg)
	details := cli.RealmDetails()
	cli.Close()

	// Client should not have be authenticated by cookie first time.
	ok, _ := wamp.AsBool(details["authbycookie"])
	require.False(t, ok, "authbycookie set incorrectly to true")

	cli = connectClientCfg(t, cfg)
	details = cli.RealmDetails()
	cli.Close()

	// If websocket, then should be authenticated by cookie this time.
	if cfg.WsCfg.Jar != nil {
		ok, _ := wamp.AsBool(details["authbycookie"])
		require.True(t, ok, "should have been authenticated by cookie")
	} else {
		ok, _ := wamp.AsBool(details["authbycookie"])
		require.False(t, ok, "authbycookie set incorrectly to true")
	}
}

func TestJoinRealmWithCRAuthBad(t *testing.T) {
	checkGoLeaks(t)

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

	cli, err := connectClientCfgErr(cfg)
	require.Error(t, err, "expected error with bad username")
	require.ErrorContains(t, err, "invalid signature")
	require.Nil(t, cli, "Client should be nil on connect error")
}

func TestAuthz(t *testing.T) {
	checkGoLeaks(t)

	cfg := client.Config{
		Realm:           testAuthRealm,
		ResponseTimeout: time.Second,
	}

	// Connect subscriber session.
	subscriber := connectClientCfg(t, cfg)

	// Connect caller.
	caller := connectClientCfg(t, cfg)

	// Check that subscriber does not have special info provided by authorizer.
	ctx := context.Background()
	args := wamp.List{subscriber.ID()}
	result, err := caller.Call(ctx, metaGet, nil, args, nil, nil)
	require.NoError(t, err)
	dict, _ := wamp.AsDict(result.Arguments[0])
	_, ok := dict["foobar"]
	require.False(t, ok, "Should not have special info in session")

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
	require.NoError(t, err)

	// Check that authorizer modified session with special info.
	result, err = caller.Call(ctx, metaGet, nil, args, nil, nil)
	require.NoError(t, err)
	dict, _ = wamp.AsDict(result.Arguments[0])
	s, _ := wamp.AsString(dict["foobar"])
	require.Equal(t, "baz", s, "Missing special info in session")

	// Publish an event to something that matches by wildcard.
	caller.Publish("nexus.interceptor.foobar.baz", nil, wamp.List{"hi"}, nil)
	// Make sure the event was received.
	select {
	case err = <-errChan:
		require.NoError(t, err, "Event error")
	case <-time.After(200 * time.Millisecond):
		require.FailNow(t, "did not get published event")
	}

	// Have caller call a procedure that will fail authz.
	_, err = caller.Call(ctx, "need.ldap.auth",
		wamp.Dict{wamp.OptAcknowledge: true}, nil, nil, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "wamp.error.authorization_failed: Cannot contact LDAP server")
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
		dk := pbkdf2.Key(secret, salt, iterations, keylen, sha256.New)
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
		// Tracking cookie not enabled, return no error.
		return nil
	}
	nextcookie := v.(*http.Cookie)

	// Update tracking cookie that will identify this authenticated client.
	ks.cookie = nextcookie

	// Tell the client whether or not it was allowed by its cookie.
	welcome.Details["authbycookie"] = ks.authByCookie
	return nil
}
