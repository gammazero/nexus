package wamp

// Keys for authentication values in the details of HELLO and WELCOME
// messages, and in session details.
const (
	// DetailAuthID is the authentication ID that a client asks to
	// authenticate as in HELLO, and that the router authenticated it as in
	// WELCOME.
	DetailAuthID = "authid"
	// DetailAuthRole is the role that the router assigned to the
	// authenticated session, given in WELCOME.
	DetailAuthRole = "authrole"
	// DetailAuthMethod is the method that the router used to authenticate
	// the session, given in WELCOME.
	DetailAuthMethod = "authmethod"
	// DetailAuthMethods is the list of authentication methods that a client
	// offers in HELLO.
	DetailAuthMethods = "authmethods"
	// DetailAuthProvider is the name of the provider that authenticated the
	// session, given in WELCOME.
	DetailAuthProvider = "authprovider"
	// DetailAuthExtra is a dictionary of additional authentication data, given
	// in HELLO and WELCOME.
	DetailAuthExtra = "authextra"
)

// Keys for session values in the details of HELLO and WELCOME messages, and
// in session details.
const (
	// DetailSession is the ID of the session, in session details.
	DetailSession = "session"
	// DetailTransport is a dictionary that describes the transport of the
	// session, in session details.
	DetailTransport = "transport"
	// DetailRoles is the dictionary of roles that a client announces in
	// HELLO, and that the router announces in WELCOME. Each role maps to a
	// dictionary that may hold a DetailFeatures dictionary.
	DetailRoles = "roles"
	// DetailFeatures is the dictionary of features that a peer supports for a
	// role, within that role's DetailRoles entry.
	DetailFeatures = "features"
)

// Keys for values in the details of EVENT and INVOCATION messages.
const (
	// DetailPublisher is the session ID of the publisher, in EVENT details,
	// when the publisher's identity is disclosed.
	DetailPublisher = "publisher"
	// DetailPublisherAuthID is the authid of the publisher, in EVENT details,
	// when the publisher's identity is disclosed.
	DetailPublisherAuthID = "publisher_authid"
	// DetailPublisherAuthRole is the authrole of the publisher, in EVENT
	// details, when the publisher's identity is disclosed.
	DetailPublisherAuthRole = "publisher_authrole"
	// DetailCaller is the session ID of the caller, in INVOCATION details,
	// when the caller's identity is disclosed.
	DetailCaller = "caller"
	// DetailCallerAuthID is the authid of the caller, in INVOCATION details,
	// when the caller's identity is disclosed.
	DetailCallerAuthID = "caller_authid"
	// DetailCallerAuthRole is the authrole of the caller, in INVOCATION
	// details, when the caller's identity is disclosed.
	DetailCallerAuthRole = "caller_authrole"
	// DetailTopic is the topic that an event was published to, in EVENT
	// details for a pattern-based subscription.
	DetailTopic = "topic"
	// DetailProcedure is the procedure that was called, in INVOCATION
	// details for a pattern-based registration.
	DetailProcedure = "procedure"
)

// Keys for the authentication values that nexus adds to the transport
// details of a websocket session. These are not defined by WAMP. The values
// are found under the DetailAuth key of the DetailTransport dictionary in
// HELLO details, so that authenticators and authorizers can use them.
const (
	// DetailAuth is the dictionary of transport authentication values,
	// within the DetailTransport dictionary.
	DetailAuth = "auth"
	// DetailCookie is the tracking cookie that the client sent in its
	// websocket upgrade request, as an *http.Cookie.
	DetailCookie = "cookie"
	// DetailNextCookie is the tracking cookie that the router sent to the
	// client, to expect when the client returns, as an *http.Cookie.
	DetailNextCookie = "nextcookie"
	// DetailRequest is the websocket upgrade request, as an *http.Request.
	DetailRequest = "request"
)
