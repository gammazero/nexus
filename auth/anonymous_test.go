package auth

import (
	"testing"

	"github.com/gammazero/nexus/wamp"
)

func TestAnonAuth(t *testing.T) {
	anonAuth := AnonymousAuth

	details := map[string]interface{}{
		"authid":      "someone",
		"authmethods": []string{"anonymous"}}
	welcome, err := anonAuth.Authenticate(details, nil)
	if err != nil {
		t.Fatal("authenticate failed: ", err.Error())
	}

	if welcome == nil {
		t.Fatal("received nil welcome msg")
	}
	if welcome.MessageType() != wamp.WELCOME {
		t.Fatal("expected WELCOME message, got: ", welcome.MessageType())
	}

	if welcome.Details["authmethod"].(string) != "anonymous" {
		t.Fatal("invalid authmethod in welcome details")
	}
	if welcome.Details["authrole"].(string) != "anonymous" {
		t.Fatal("incorrect authrole in welcome details")
	}
}
