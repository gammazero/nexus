package auth_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gammazero/nexus/v3/wamp/crsign"
	"github.com/stretchr/testify/require"
)

type testKeyStore struct {
	provider string
	secret   string
	ticket   string
	cookie   *http.Cookie

	authByCookie bool
}

const (
	goodTicket = "ticket-X-1234"
	goodSecret = "squeemishosafradge"
)

func (ks *testKeyStore) AuthKey(authid, authmethod string) ([]byte, error) {
	if authid != "jdoe" {
		return nil, errors.New("no such user: " + authid)
	}
	switch authmethod {
	case "wampcra":
		// Lookup the user's key.
		return []byte(ks.secret), nil
	case "ticket":
		return []byte(ks.ticket), nil
	}
	return nil, errors.New("unsupported authmethod")
}

func (ks *testKeyStore) AuthRole(authid string) (string, error) {
	if authid != "jdoe" {
		return "", errors.New("no such user: " + authid)
	}
	return "user", nil
}

func (ks *testKeyStore) PasswordInfo(authid string) (string, int, int) {
	return "", 0, 0
}

func (ks *testKeyStore) Provider() string { return ks.provider }

func (ks *testKeyStore) AlreadyAuth(authid string, details wamp.Dict) bool {
	v, err := wamp.DictValue(details, []string{"transport", "auth", "cookie"})
	if err != nil {
		// No tracking cookie, so not auth.
		return false
	}
	cookie := v.(*http.Cookie)
	// Check if tracking cookie matches cookie of previously good client.
	if cookie.Value == ks.cookie.Value {
		ks.authByCookie = true
		return true
	}
	return false
}

func (ks *testKeyStore) OnWelcome(authid string, welcome *wamp.Welcome, details wamp.Dict) error {
	v, err := wamp.DictValue(details, []string{"transport", "auth", "nextcookie"})
	if err != nil {
		return err
	}
	// Update tracking cookie that will identify this authenticated client.
	ks.cookie = v.(*http.Cookie)

	welcome.Details["authbycookie"] = ks.authByCookie
	return nil
}

var tks = &testKeyStore{
	provider: "static",
	secret:   goodSecret,
	ticket:   goodTicket,
}

func cliRsp(p wamp.Peer) {
	for msg := range p.Recv() {
		ch, ok := msg.(*wamp.Challenge)
		if !ok {
			continue
		}
		signature, authDetails := clientAuthFunc(ch)
		p.Send(&wamp.Authenticate{
			Signature: signature,
			Extra:     authDetails,
		})
	}
}

func clientAuthFunc(c *wamp.Challenge) (string, wamp.Dict) {
	// If the client needed to lookup a user's key, this would require decoding
	// the JSON-encoded ch string and getting the authid. For this example
	// assume that client only operates as one user and knows the key to use.
	var sig string
	switch c.AuthMethod {
	case "ticket":
		sig = goodTicket
	case "wampcra":
		sig = crsign.RespondChallenge(goodSecret, c, nil)
	}
	return sig, wamp.Dict{}
}

func TestTicketAuth(t *testing.T) {
	cp, rp := transport.LinkedPeers()
	defer cp.Close()
	defer rp.Close()
	go cliRsp(cp)

	ticketAuth := auth.NewTicketAuthenticator(tks, time.Second)
	sid := wamp.ID(212)

	// Test with missing authid
	details := wamp.Dict{}
	welcome, err := ticketAuth.Authenticate(sid, details, rp)
	require.Error(t, err, "expected error with missing authid")
	require.Nil(t, welcome)

	// Test with unknown authid.
	details["authid"] = "unknown"
	_, err = ticketAuth.Authenticate(sid, details, rp)
	require.Error(t, err, "expected error from unknown authid")

	// Test with known authid.
	details["authid"] = "jdoe"
	// Provide tracking cookie to ientify this client in the future.
	nextCookie := &http.Cookie{Name: "nexus-wamp-cookie", Value: "a1b2c3"}
	authDict := wamp.Dict{"nextcookie": nextCookie}
	details["transport"] = wamp.Dict{"auth": authDict}

	welcome, err = ticketAuth.Authenticate(sid, details, rp)
	require.NoError(t, err, "challenge failed")
	require.NotNil(t, welcome, "received nil welcome msg")
	require.Equal(t, wamp.WELCOME, welcome.MessageType())
	s, _ := wamp.AsString(welcome.Details["authmethod"])
	require.Equal(t, "ticket", s, "invalid authmethod in welcome details")
	s, _ = wamp.AsString(welcome.Details["authrole"])
	require.Equal(t, "user", s, "incorrect authrole in welcome details")
	ok, _ := wamp.AsBool(welcome.Details["authbycookie"])
	require.False(t, ok, "authbycookie set incorrectly to true")
	tks.ticket = "bad"

	// Test with bad ticket.
	details["authid"] = "jdoe"
	welcome, err = ticketAuth.Authenticate(sid, details, rp)
	require.Error(t, err, "expected error with bad ticket")

	// Supply the previous tracking cookie in transport.auth.  This will
	// identify the previously authenticated client.
	authDict["cookie"] = &http.Cookie{Name: "nexus-wamp-cookie", Value: "a1b2c3"}
	authDict["nextcookie"] = &http.Cookie{Name: "nexus-wamp-cookie", Value: "xyz123"}
	welcome, err = ticketAuth.Authenticate(sid, details, rp)
	// Event though ticket is bad, cookie from previous good session should
	// authenticate client.
	require.NoError(t, err, "challenge failed")
	// Client should be authenticated by cookie.
	ok, _ = wamp.AsBool(welcome.Details["authbycookie"])
	require.True(t, ok, "authbycookie set incorrectly to false")
}

func TestCRAuth(t *testing.T) {
	cp, rp := transport.LinkedPeers()
	defer cp.Close()
	defer rp.Close()
	go cliRsp(cp)

	crAuth := auth.NewCRAuthenticator(tks, time.Second)
	sid := wamp.ID(212)

	// Test with missing authid
	details := wamp.Dict{}
	_, err := crAuth.Authenticate(sid, details, rp)
	require.Error(t, err, "expected error with missing authid")

	// Test with unknown authid.
	details["authid"] = "unknown"
	_, err = crAuth.Authenticate(sid, details, rp)
	require.Error(t, err, "expected error from unknown authid")

	// Test with known authid.
	details["authid"] = "jdoe"
	nextCookie := &http.Cookie{Name: "nexus-wamp-cookie", Value: "a1b2c3"}
	authDict := wamp.Dict{"nextcookie": nextCookie}
	details["transport"] = wamp.Dict{"auth": authDict}

	welcome, err := crAuth.Authenticate(sid, details, rp)
	require.NoError(t, err, "challenge failed")
	require.NotNil(t, welcome, "received nil welcome msg")
	require.Equal(t, wamp.WELCOME, welcome.MessageType(), "expected WELCOME message")
	s, _ := wamp.AsString(welcome.Details["authmethod"])
	require.Equal(t, "wampcra", s, "invalid authmethod in welcome details")
	s, _ = wamp.AsString(welcome.Details["authrole"])
	require.Equal(t, "user", s, "incorrect authrole in welcome details")

	tks.secret = "bad"

	// Test with bad ticket.
	details["authid"] = "jdoe"
	welcome, err = crAuth.Authenticate(sid, details, rp)
	require.Error(t, err, "expected error with bad key")

	authDict["cookie"] = &http.Cookie{Name: "nexus-wamp-cookie", Value: "a1b2c3"}
	authDict["nextcookie"] = &http.Cookie{Name: "nexus-wamp-cookie", Value: "xyz123"}
	welcome, err = crAuth.Authenticate(sid, details, rp)
	require.NoError(t, err, "challenge failed")
}
