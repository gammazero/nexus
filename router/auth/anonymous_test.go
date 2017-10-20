package auth

import (
	"testing"

	"github.com/gammazero/nexus/wamp"
)

func TestAnonAuth(t *testing.T) {
	anonAuth := AnonymousAuth

	details := wamp.Dict{
		"authid":      "someone",
		"authmethods": []string{"anonymous"}}
	welcome, err := anonAuth.Authenticate(wamp.ID(101), details, nil)
	if err != nil {
		t.Fatal("authenticate failed: ", err.Error())
	}

	if welcome == nil {
		t.Fatal("received nil welcome msg")
	}
	if welcome.MessageType() != wamp.WELCOME {
		t.Fatal("expected WELCOME message, got: ", welcome.MessageType())
	}

	if wamp.OptionString(welcome.Details, "authmethod") != "anonymous" {
		t.Fatal("invalid authmethod in welcome details")
	}
	if wamp.OptionString(welcome.Details, "authrole") != "anonymous" {
		t.Fatal("incorrect authrole in welcome details")
	}
}
