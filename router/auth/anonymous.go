package auth

import (
	"strconv"

	"github.com/gammazero/nexus/v3/wamp"
)

// AnonymousAuth implements Authenticator interface.
//
// To use anonymous authentication, supply an anstance of AnonymousAuth with
// the AuthRole of choice to the RealmConfig:
//
//     RealmConfigs: []*router.RealmConfig{
//         {
//             Authenticators:  []auth.Authenticator{
//                 &auth.AnonymousAuth{ AuthRole: "guest" },
//             },
//             ...
//         },
//
// Or, set AnonymousAuth=ture in the RealmConfig and let the router create an
// instance with the AuthRole of "anonymous".
type AnonymousAuth struct {
	AuthRole string
}

// AuthMethod retruns description of authentication method.
func (a *AnonymousAuth) AuthMethod() string {
	return "anonymous"
}

// Authenticate an anonymous client.  This always succeeds, and provides the
// authmethod and authrole for the WELCOME message.
func (a *AnonymousAuth) Authenticate(sid wamp.ID, details wamp.Dict, client wamp.Peer) (*wamp.Welcome, error) {
	// Create welcome details containing auth info.
	return &wamp.Welcome{
		Details: wamp.Dict{
			"authid":       strconv.FormatInt(int64(wamp.GlobalID()), 16),
			"authrole":     a.AuthRole,
			"authprovider": "static",
			"authmethod":   a.AuthMethod(),
		},
	}, nil
}
