/*
Package auth provides interfaces for implementing authentication logic that the
WAMP router can use.

In addition in authentication and challenge-response authentication interface,
this package provides default implementations for the following authentication
methods: "wampcra", ticket", "anonymous".

*/
package auth

import (
	"time"

	"github.com/gammazero/nexus/wamp"
)

const defaultCRAuthTimeout = time.Minute

// Authenticator is implemented by a type that handles authentication using
// only the HELLO message.
type Authenticator interface {
	// Authenticate takes HELLO details and returns a WELCOME message if
	// successful, otherwise it returns an error.
	Authenticate(sid wamp.ID, details wamp.Dict, client wamp.Peer) (*wamp.Welcome, error)

	// AuthMethod returns a string describing the authentication methiod.
	AuthMethod() string
}

// KeyStore is used to retrieve keys and information about a user.
type KeyStore interface {
	// AuthKey returns the user's key appropriate for the specified authmethod.
	AuthKey(authid, authmethod string) ([]byte, error)

	// PasswordInfo returns salting info for the user's password.  This
	// information must be available when using keys computed with PBKDF2.
	PasswordInfo(authid string) (string, int, int)

	// Returns the authrole for the user.
	AuthRole(authid string) (string, error)

	// Returns name of this KeyStore instnace.
	Provider() string
}
