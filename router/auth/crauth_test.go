package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/wamp"
	"github.com/gammazero/nexus/wamp/crsign"
)

type testKeyStore struct {
	provider string
	secret   string
	ticket   string
	cookieid string

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
	v, err := wamp.DictValue(details, []string{"transport", "auth", "cookieid"})
	if err != nil {
		// No tracking cookie, so not auth.
		return false
	}
	cookieid, ok := wamp.AsString(v)
	if ok {
		// Tracking cookie matches cookie of previously good client.
		if cookieid == ks.cookieid {
			ks.authByCookie = true
			return true
		}
	}
	return false
}

func (ks *testKeyStore) OnWelcome(authid string, welcome *wamp.Welcome, details wamp.Dict) error {
	v, err := wamp.DictValue(details, []string{"transport", "auth", "nextcookieid"})
	if err != nil {
		return nil
	}
	nextcookieid, ok := wamp.AsString(v)
	if ok {
		// Update tracking cookie that will identify this authenticated client.
		ks.cookieid = nextcookieid
	}
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

	ticketAuth := NewTicketAuthenticator(tks, time.Second)
	sid := wamp.ID(212)

	// Test with missing authid
	details := wamp.Dict{}
	welcome, err := ticketAuth.Authenticate(sid, details, rp)
	if err == nil {
		t.Fatal("expected error with missing authid")
	}

	// Test with unknown authid.
	details["authid"] = "unknown"
	welcome, err = ticketAuth.Authenticate(sid, details, rp)
	if err == nil {
		t.Fatal("expected error from unknown authid")
	}

	// Test with known authid.
	details["authid"] = "jdoe"
	// Provide tracking cookie to ientify this client in the future.
	authDict := wamp.Dict{"nextcookieid": "a1b2c3"}
	details["transport"] = wamp.Dict{"auth": authDict}

	welcome, err = ticketAuth.Authenticate(sid, details, rp)
	if err != nil {
		t.Fatal("challenge failed: ", err.Error())
	}
	if welcome == nil {
		t.Fatal("received nil welcome msg")
	}
	if welcome.MessageType() != wamp.WELCOME {
		t.Fatal("expected WELCOME message, got: ", welcome.MessageType())
	}
	if wamp.OptionString(welcome.Details, "authmethod") != "ticket" {
		t.Fatal("invalid authmethod in welcome details")
	}
	if wamp.OptionString(welcome.Details, "authrole") != "user" {
		t.Fatal("incorrect authrole in welcome details")
	}
	if wamp.OptionFlag(welcome.Details, "authbycookie") {
		t.Fatal("authbycookie set incorrectly to true")
	}
	tks.ticket = "bad"

	// Test with bad ticket.
	details["authid"] = "jdoe"
	welcome, err = ticketAuth.Authenticate(sid, details, rp)
	if err == nil {
		t.Fatal("expected error with bad ticket")
	}

	// Supply the previous tracking cookie in transport.auth.  This will
	// identify the previously authenticated client.
	authDict["cookieid"] = "a1b2c3"
	authDict["nextcookieid"] = "xyz123"
	welcome, err = ticketAuth.Authenticate(sid, details, rp)
	// Event though ticket is bad, cookie from previous good session should
	// authenticate client.
	if err != nil {
		t.Fatal("challenge failed: ", err.Error())
	}
	// Client should be authenticated by cookie.
	if !wamp.OptionFlag(welcome.Details, "authbycookie") {
		t.Fatal("authbycookie set incorrectly to false")
	}
}

func TestCRAuth(t *testing.T) {
	cp, rp := transport.LinkedPeers()
	defer cp.Close()
	defer rp.Close()
	go cliRsp(cp)

	crAuth := NewCRAuthenticator(tks, time.Second)
	sid := wamp.ID(212)

	// Test with missing authid
	details := wamp.Dict{}
	welcome, err := crAuth.Authenticate(sid, details, rp)
	if err == nil {
		t.Fatal("expected error with missing authid")
	}

	// Test with unknown authid.
	details["authid"] = "unknown"
	welcome, err = crAuth.Authenticate(sid, details, rp)
	if err == nil {
		t.Fatal("expected error from unknown authid")
	}

	// Test with known authid.
	details["authid"] = "jdoe"
	authDict := wamp.Dict{"nextcookieid": "a1b2c3"}
	details["transport"] = wamp.Dict{"auth": authDict}

	welcome, err = crAuth.Authenticate(sid, details, rp)
	if err != nil {
		t.Fatal("challenge failed: ", err.Error())
	}
	if welcome == nil {
		t.Fatal("received nil welcome msg")
	}
	if welcome.MessageType() != wamp.WELCOME {
		t.Fatal("expected WELCOME message, got: ", welcome.MessageType())
	}
	if wamp.OptionString(welcome.Details, "authmethod") != "wampcra" {
		t.Fatal("invalid authmethod in welcome details")
	}
	if wamp.OptionString(welcome.Details, "authrole") != "user" {
		t.Fatal("incorrect authrole in welcome details")
	}

	tks.secret = "bad"

	// Test with bad ticket.
	details["authid"] = "jdoe"
	welcome, err = crAuth.Authenticate(sid, details, rp)
	if err == nil {
		t.Fatal("expected error with bad key")
	}

	authDict["cookieid"] = "a1b2c3"
	authDict["nextcookieid"] = "xyz123"
	welcome, err = crAuth.Authenticate(sid, details, rp)
	if err != nil {
		t.Fatal("challenge failed: ", err.Error())
	}
}
