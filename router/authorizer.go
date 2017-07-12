package router

import "github.com/gammazero/nexus/wamp"

// Authorizer is the interface implemented by a type that provides the ability
// to authroize request messages.
type Authorizer interface {
	// Authorize returns true if the request is authorized or not.  An error is
	// returned if there is a failure to determine authorization.
	Authorize(sess *Session, msg wamp.Message) (bool, error)
}

// authorizer is the default implementation that always returns authorized.
type authorizer struct{}

// NewAuthorizer returns the default authorizer.
func NewAuthorizer() Authorizer {
	return &authorizer{}
}

// Authorize default implementation authorizes any session for all roles.
func (a *authorizer) Authorize(sess *Session, msg wamp.Message) (bool, error) {
	return true, nil
}
