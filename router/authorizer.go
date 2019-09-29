package router

import "github.com/gammazero/nexus/v3/wamp"

// Authorizer is the interface implemented by a type that provides the ability
// to authorize sending messages.
type Authorizer interface {
	// Authorize returns true if the sending session is authorized to send the
	// message.  Otherwise, it returns or false if not authorized.  An error is
	// returned if there is a failure to determine authorization.  This error
	// is included in the ERROR response to the client.
	//
	// Since the Authorizer accesses both the sending session and the message
	// through a pointer, the authorizer can alter the content of both the
	// sending session and the message.  This allows the authorizer to also
	// work as an interceptor of messages that can change their content and/or
	// change the sending session based on the intercepted message.  This
	// functionality may be used to set values in the session upon encountering
	// certain messages sent by that session.
	Authorize(*wamp.Session, wamp.Message) (bool, error)
}
