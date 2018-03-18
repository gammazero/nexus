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
	//
	// If the client was a websocket peer, then the websocket upgrade request
	// is available from the client.
	//
	//    httpReq := transport.WSRequest(client)
	//
	// If cookie tracking is also enabled, then a subsequent request from the
	// client will include an expected cookie.  That expected cookie isa
	// available from the client and can be retireved with a helper function:
	//
	//    expectedCookie := transport.WSNextCookie(client)
	//
	// The tracking cookie can be used to tell if a previous clien to the
	// router, and lookup information about that client, such as whether it was
	// previously authenticated.
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
	PasswordInfo(authid string) (salt string, keylen int, iterations int)

	// Returns the authrole for the user.
	AuthRole(authid string) (string, error)

	// Returns name of this KeyStore instnace.
	Provider() string
}

// BypassKeyStore is a KeyStore with additional functionality for looking at
// HELLO/Session details, including transport information, to recognize clients
// that have been previously authenticated.
//
// When used with the provided CR and ticket authenticators, if AlreadyAuth
// returns true, then the normal authentication method is bypassed.
type BypassKeyStore interface {
	KeyStore

	// AlreadyAuth takes information about a HELLO request including transport
	// details.  If the client is a websocket client, then information from the
	// HTTP upgrade request, the tracking cookie ID from the request, and next
	// tracking cookie ID (if enabled) are available in
	// details.transport.auth|Dict.
	//
	// If the client is recognized and already authenticated, then AlreadyAuth
	// returns true.  Otherwise, false is returned if not authenticated.
	AlreadyAuth(authid string, details wamp.Dict) bool

	// OnWelcome is called when a client is successfully authenticated.  This
	// allows the KeyStore to update any information about the authenticated
	// client, to modify the welcome message, and to store the next tracking
	// cookie ID to expect from the client.  The request information and the
	// next cookie ID are available from in details.transport.auth|Dict.
	OnWelcome(authid string, welcome *wamp.Welcome, details wamp.Dict) error
}
