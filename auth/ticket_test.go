package auth

import (
	"testing"

	"github.com/gammazero/nexus/wamp"
)

func TestTicketAuth(t *testing.T) {
	tAuth := NewTicketAuthenticator(newUserDB()).(*ticketAuthenticator)
	var ticketAuth Challenger
	ticketAuth = tAuth

	// Test with missing authid
	details := wamp.Dict{"authmethod": "ticket"}
	pendingAuth, err := ticketAuth.Challenge(details)
	if err == nil {
		t.Fatal("expected error with missing authid")
	}

	// Test with unknown authid good authmethod.
	details["authid"] = "unknown"
	pendingAuth, err = ticketAuth.Challenge(details)
	if err == nil {
		t.Fatal("expected error from unknown authid")
	}

	// Test with known authid, bad authmethod
	details["authid"] = "jdoe"
	details["authmethod"] = "flounder"
	pendingAuth, err = ticketAuth.Challenge(details)
	if err == nil {
		t.Fatal("expected error from bad authmethod")
	}

	// Test with known authid, good authmethod
	details["authmethod"] = "ticket"
	pendingAuth, err = ticketAuth.Challenge(details)
	if err != nil {
		t.Fatal("challenge failed: ", err.Error())
	}

	// Get the CHALLENGE message.
	chMsg := pendingAuth.Msg()
	if chMsg.MessageType() != wamp.CHALLENGE {
		t.Fatal("expected CHALLENGE message, got: ", chMsg.MessageType())
	}

	// Compose AUTHENTICATE message with bad secret.
	authRsp := &wamp.Authenticate{Signature: "allwrong", Extra: nil}
	welcome, err := pendingAuth.Authenticate(authRsp)
	if err == nil {
		t.Fatal("unexpected error with incorrect secret")
	}

	// Compose a good AUTHENTICATE message.
	authRsp = &wamp.Authenticate{Signature: "password1", Extra: nil}
	welcome, err = pendingAuth.Authenticate(authRsp)
	if err != nil {
		t.Fatal("unexpected auth error: ", err.Error())
	}
	if welcome == nil {
		t.Fatal("received nil welcome msg")
	}
	if welcome.MessageType() != wamp.WELCOME {
		t.Fatal("expected WELCOME message, got: ", welcome.MessageType())
	}

	if welcome.Details["authmethod"].(string) != "ticket" {
		t.Fatal("invalid authmethod in welcome details")
	}
	if welcome.Details["authrole"].(string) != "user" {
		t.Fatal("incorrect authrole in welcome details")
	}
}
