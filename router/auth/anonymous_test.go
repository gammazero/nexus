package auth

import (
	"testing"

	"github.com/gammazero/nexus/v3/wamp"
)

func TestAnonAuth(t *testing.T) {
	anonAuth := AnonymousAuth{
		AuthRole: "guest",
	}

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

	if s, _ := wamp.AsString(welcome.Details["authmethod"]); s != "anonymous" {
		t.Fatal("invalid authmethod in welcome details")
	}
	if s, _ := wamp.AsString(welcome.Details["authrole"]); s != "guest" {
		t.Fatal("incorrect authrole in welcome details")
	}
}
