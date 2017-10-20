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
	return nil, nil
}

func (ks *testKeyStore) PasswordInfo(authid string) (string, int, int) {
	return "", 0, 0
}

func (ks *testKeyStore) Provider() string { return ks.provider }

func (ks *testKeyStore) AuthRole(authid string) (string, error) {
	if authid != "jdoe" {
		return "", errors.New("no such user: " + authid)
	}
	return "user", nil
}

var tks = &testKeyStore{"static", goodSecret, goodTicket}

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
	// assume that client only operate as one user and knows the key to use.
	var sig string
	switch c.AuthMethod {
	case "ticket":
		sig = goodTicket
	case "wampcra":
		chStr := wamp.OptionString(c.Extra, "challenge")
		sig = crsign.SignChallenge(chStr, []byte(goodSecret))
	}
	return sig, wamp.Dict{}
}

func TestTicketAuth(t *testing.T) {
	cp, rp := transport.LinkedPeers(nil)
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

	tks.ticket = "bad"

	// Test with bad ticket.
	details["authid"] = "jdoe"
	welcome, err = ticketAuth.Authenticate(sid, details, rp)
	if err == nil {
		t.Fatal("expected error with bad ticket")
	}
}

func TestCRAuth(t *testing.T) {
	cp, rp := transport.LinkedPeers(nil)
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
}
