package wamp

// Predefined URIs
//
// http://wamp-proto.org/static/rfc/draft-oberstet-hybi-crossbar-wamp.html#predefined-uris
const (
	// -- Interaction --

	// Peer provided an incorrect URI for any URI-based attribute of WAMP
	// message, such as realm, topic or procedure.
	ErrInvalidURI = URI("wamp.error.invalid_uri")

	// A Dealer could not perform a call, since no procedure is currently
	// registered under the given URI.
	ErrNoSuchProcedure = URI("wamp.error.no_such_procedure")

	// A procedure could not be registered, since a procedure with the given
	// URI is already registered.
	ErrProcedureAlreadyExists = URI("wamp.error.procedure_already_exists")

	// A Dealer could not perform an unregister, since the given registration
	// is not active.
	ErrNoSuchRegistration = URI("wamp.error.no_such_registration")

	// A Broker could not perform an unsubscribe, since the given subscription
	// is not active.
	ErrNoSuchSubscription = URI("wamp.error.no_such_subscription")

	// A call failed, since the given argument types or values are not
	// acceptable to the called procedure - in which case the Callee may throw
	// this error.  Or a Router performing payload validation checked the
	// payload (args / kwargs) of a call, call result, call error or publish,
	// and the payload did not conform - in which case the Router may throw
	// this error.
	ErrInvalidArgument = URI("wamp.error.invalid_argument")

	// -- Session Close --

	CloseNormal = URI("wamp.close.normal")

	// The Peer is shutting down completely - used as a GOODBYE (or ABORT)
	// reason.
	CloseSystemShutdown = URI("wamp.close.system_shutdown")
	ErrSystemShutdown   = CloseSystemShutdown

	// The Peer wants to leave the realm - used as a GOODBYE reason.
	CloseRealm    = URI("wamp.close.close_realm")
	ErrCloseRealm = CloseRealm

	// A Peer acknowledges ending of a session - used as a GOODBYE reply
	// reason.
	CloseGoodbyeAndOut = URI("wamp.close.goodbye_and_out")
	ErrGoodbyeAndOut   = CloseGoodbyeAndOut

	// -- Authorization --

	// A join, call, register, publish or subscribe failed, since the Peer is
	// not authorized to perform the operation.
	ErrNotAuthorized = URI("wamp.error.not_authorized")

	// A Dealer or Broker could not determine if the Peer is authorized to
	// perform a join, call, register, publish or subscribe, since the
	// authorization operation itself failed.  E.g. a custom authorizer ran
	// into an error.
	ErrAuthorizationFailed = URI("wamp.error.authorization_failed")

	// Something failed with the authentication itself, that is, authentication
	// could not run to end. *
	ErrAuthenticationFailed = URI("wamp.error.authentication_failed")

	// Peer wanted to join a non-existing realm (and the Router did not allow
	// to auto-create the realm)
	ErrNoSuchRealm = URI("wamp.error.no_such_realm")

	// A Peer was to be authenticated under a Role that does not (or no longer)
	// exists on the Router.  For example, the Peer was successfully
	// authenticated, but the Role configured does not exists - hence there is
	// some misconfiguration in the Router.
	ErrNoSuchRole = URI("wamp.error.no_such_role")

	// No authentication method the peer offered is available or active. *
	ErrNoAuthMethod = URI("wamp.error.no_auth_method")

	// ----- Advanced Profile -----

	// A Dealer or Callee canceled a call previously issued.
	ErrCanceled = URI("wamp.error.canceled")

	// A Peer requested an interaction with an option that was disallowed by
	// the Router.
	ErrOptionNotAllowed = URI("wamp.error.option_not_allowed")

	// A Dealer could not perform a call, since a procedure with the given URI
	// is registered, but Callee Black- and Whitelisting and/or Caller
	// Exclusion lead to the exclusion of (any) Callee providing the procedure.
	ErrNoEligibleCallee = URI("wamp.error.no_eligible_callee")

	// A Router rejected client request to disclose its identity.
	ErrOptionDisallowedDiscloseMe = URI("wamp.error.option_disallowed.disclose_me")

	// A Router encountered a network failure.
	ErrNetworkFailure = URI("wamp.error.network_failure")

	// A Peer received invalid WAMP protocol message.
	ErrProtocolViolation = URI("wamp.error.protocol_violation")

	// -- Session Meta Events --

	// Fired when a session joins a realm on the router.
	MetaEventSessionOnJoin = URI("wamp.session.on_join")

	// Fired when a session leaves a realm on the router or is disconnected.
	MetaEventSessionOnLeave = URI("wamp.session.on_leave")

	// -- Session Meta Procedures --

	// Obtains the number of sessions currently attached to the realm.
	MetaProcSessionCount = URI("wamp.session.count")

	// Retrieves a list of the session IDs for all sessions currently attached
	// to the realm.
	MetaProcSessionList = URI("wamp.session.list")

	// Retrieves information on a specific session.
	MetaProcSessionGet = URI("wamp.session.get")

	// Kill a single session identified by session ID.
	MetaProcSessionKill = URI("wamp.session.kill")

	// Kill all currently connected sessions that have the specified authid.
	MetaProcSessionKillByAuthid = URI("wamp.session.kill_by_authid")

	// Kill all currently connected sessions that have the specified authrole.
	MetaProcSessionKillByAuthrole = URI("wamp.session.kill_by_authrole")

	// Kill all currently connected sessions in the caller's realm.
	MetaProcSessionKillAll = URI("wamp.session.kill_all")

	// Modify details of session identified by session ID (non-standard).
	MetaProcSessionModifyDetails = URI("wamp.session.modify_details")

	// No session with the given ID exists on the router.
	ErrNoSuchSession = URI("wamp.error.no_such_session")

	// -- Registration Meta Events --

	// Fired when a registration is created through a registration request for
	// an URI which was previously without a registration.
	MetaEventRegOnCreate = URI("wamp.registration.on_create")

	// Fired when a Callee session is added to a registration.
	MetaEventRegOnRegister = URI("wamp.registration.on_register")

	// Fired when a Callee session is removed from a registration.
	MetaEventRegOnUnregister = URI("wamp.registration.on_unregister")

	// Fired when a registration is deleted after the last Callee session
	// attached to it has been removed.
	MetaEventRegOnDelete = URI("wamp.registration.on_delete")

	// -- Registration Meta Procedures --

	// Retrieves registration IDs listed according to match policies
	MetaProcRegList = URI("wamp.registration.list")

	// Obtains the registration (if any) managing a procedure, according to
	// some match policy.
	MetaProcRegLookup = URI("wamp.registration.lookup")

	// Obtains the registration best matching a given procedure URI.
	MetaProcRegMatch = URI("wamp.registration.match")

	// Retrieves information on a particular registration.
	MetaProcRegGet = URI("wamp.registration.get")

	// Retrieves a list of session IDs for sessions currently attached to the
	// registration.
	MetaProcRegListCallees = URI("wamp.registration.list_callees")

	// Obtains the number of sessions currently attached to the registration.
	MetaProcRegCountCallees = URI("wamp.registration.count_callees")

	// -- Subscription Meta Events --

	// Fired when a subscription is created through a subscription request for
	// a topic which was previously without subscribers.
	MetaEventSubOnCreate = URI("wamp.subscription.on_create")

	// Fired when a session is added to a subscription.
	MetaEventSubOnSubscribe = URI("wamp.subscription.on_subscribe")

	// Fired when a session is removed from a subscription.
	MetaEventSubOnUnsubscribe = URI("wamp.subscription.on_unsubscribe")

	// Fired when a subscription is deleted after the last session attached to
	// it has been removed.
	MetaEventSubOnDelete = URI("wamp.subscription.on_delete")

	// -- Subscription Meta Procedures --

	// Retrieves subscription IDs listed according to match policies.
	MetaProcSubList = URI("wamp.subscription.list")

	// Obtains the subscription (if any) managing a topic, according to some
	// match policy.
	MetaProcSubLookup = URI("wamp.subscription.lookup")

	// Retrieves a list of IDs of subscriptions matching a topic URI,
	// irrespective of match policy.
	MetaProcSubMatch = URI("wamp.subscription.match")

	// Retrieves information on a particular subscription.
	MetaProcSubGet = URI("wamp.subscription.get")

	// Retrieves a list of session IDs for sessions currently attached to the
	// subscription.
	MetaProcSubListSubscribers = URI("wamp.subscription.list_subscribers")

	// Obtains the number of sessions currently attached to the subscription.
	MetaProcSubCountSubscribers = URI("wamp.subscription.count_suscribers")

	// -- Testament Meta Procedures --

	// Add a Testament which will be published on a particular topic when the
	// Session is detached or destroyed.
	MetaProcSessionAddTestament = URI("wamp.session.add_testament")

	// Remove the Testaments for that Session, either for when it is detached
	// or destroyed.
	MetaProcSessionFlushTestaments = URI("wamp.session.flush_testaments")
)
