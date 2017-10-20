package auth

import "github.com/gammazero/nexus/wamp"

// anonAuth implements Authenticator interface.
type anonymousAuth struct{}

// Static instance of anonAuth.  Used to enable anonymous anutentication.
var AnonymousAuth Authenticator = &anonymousAuth{}

func (a *anonymousAuth) AuthMethod() string { return "anonymous" }

// Authenticate an anonymous client.  This always succeeds, and provides the
// authmethod and authrole for the WELCOME message.
func (a *anonymousAuth) Authenticate(sid wamp.ID, details wamp.Dict, client wamp.Peer) (*wamp.Welcome, error) {
	// Create welcome details containing auth info.
	details = wamp.Dict{
		"authid":       string(wamp.GlobalID()),
		"authmethod":   a.AuthMethod(),
		"authrole":     "anonymous",
		"authprovider": "static",
	}
	return &wamp.Welcome{Details: details}, nil
}
