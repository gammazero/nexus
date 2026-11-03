package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/wamp"
)

func TestAnonAuth(t *testing.T) {
	anonAuth := auth.AnonymousAuth{
		AuthRole: "guest",
	}

	details := wamp.Dict{
		wamp.DetailAuthID:      "someone",
		wamp.DetailAuthMethods: []string{"anonymous"}}
	welcome, err := anonAuth.Authenticate(wamp.ID(101), details, nil)
	require.NoError(t, err)

	require.NotNil(t, welcome, "received nil welcome msg")
	require.Equal(t, wamp.WELCOME, welcome.MessageType())
	s, _ := wamp.AsString(welcome.Details[wamp.DetailAuthMethod])
	require.Equal(t, "anonymous", s, "invalid authmethod in welcome details")
	s, _ = wamp.AsString(welcome.Details[wamp.DetailAuthRole])
	require.Equal(t, "guest", s, "incorrect authrole in welcome details")
}
