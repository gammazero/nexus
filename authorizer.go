package nexus

import "github.com/gammazero/nexus/wamp"

// Authorizer is the interface implemented by a type that provides the ability
// to authorize request messages.
type Authorizer interface {
	// Authorize returns true if the request is authorized or false if not.  An
	// error is returned if there is a failure to determine authorization. If
	// Authorize returns false, an optional reason string may be returned to
	// provide additional information to the client.
	//
	// Since the Authorizer accesses both the session and the message through a
	// pointer, the authorizer can alter the content of both the session and
	// the message.  This allows the authorizer to also work as an interceptor
	// of messages to change their content or change the sending session based
	// on the intercepted message.
	Authorize(*Session, wamp.Message) (bool, string, error)
}

// authorizer is the default implementation that always returns authorized.
type authorizer struct{}

// NewAuthorizer returns the default authorizer.
func NewAuthorizer() Authorizer {
	return &authorizer{}
}

// Authorize default implementation authorizes any session for all roles.
func (a *authorizer) Authorize(sess *Session, msg wamp.Message) (bool, string, error) {
	return true, "", nil
}
