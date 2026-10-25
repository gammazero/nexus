package router

import "github.com/gammazero/nexus/v3/wamp"

// Authorizer is the interface implemented by a type that provides the ability
// to authorize sending messages.
type Authorizer interface {
	// Authorize returns true if the sending session is authorized to send the
	// message. Otherwise, it returns or false if not authorized. An error is
	// returned if there is a failure to determine authorization. This error is
	// included in the ERROR response to the client.
	//
	// Since the Authorizer accesses both the sending session and the message
	// through a pointer, the authorizer can alter the content of both the
	// sending session and the message. This allows the authorizer to also work
	// as an interceptor of messages that can change their content and/or
	// change the sending session based on the intercepted message. This
	// functionality may be used to set values in the session upon encountering
	// certain messages sent by that session.
	//
	// Authorize is called with a shallow copy of the session details, and
	// without the session locked, so that a slow Authorize does not stall other
	// sessions. The keys it adds, removes, or sets to a new value in the copy
	// are applied to the session after Authorize returns. A change made to
	// another key meanwhile is kept.
	//
	// Nested values, such as dicts and lists, are shared with the session and
	// with readers of the session details. To change a nested value, Authorize
	// must set its key to a new value, for example a changed copy of the dict.
	// It must not change a nested value in place: that change races with the
	// readers, and is not detected as a change. Assigning a new map to the
	// session's Details has no effect on the session.
	Authorize(*wamp.Session, wamp.Message) (bool, error)
}
