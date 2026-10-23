package router

import (
	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/wamp"
)

// Config configures the router with realms, and optionally a template for
// creating new realms.
type Config struct {
	// RealmConfigs defines the configurations for realms within the router.
	RealmConfigs []*RealmConfig `json:"realms"`

	// RealmTemplate, if defined, is used by the router to create new realms
	// when a client requests to join a realm that does not yet exist. If
	// RealmTemplate is nil (the default), then clients must join existing
	// realms.
	//
	// Caution, enabling a realm template that allows anonymous authentication
	// allows unauthenticated clients to create new realms.
	RealmTemplate *RealmConfig `json:"realm_template"`

	// Enable debug logging for router, realm, broker, dealer
	Debug bool
	// Interval in seconds for logging memory stats. O to disable. Logs Alloc,
	// Mallocs, Frees, and NumGC. For a description of these, see
	// https://golang.org/pkg/runtime/#MemStats
	MemStatsLogSec int `json:"mem_stats_log_sec"`
}

// RealmConfig configures a single realm in the router. The router
// configuration may specify a list of realms to configure.
type RealmConfig struct {
	// URI that identifies the realm.
	URI wamp.URI
	// Enforce strict URI format validation.
	StrictURI bool `json:"strict_uri"`
	// Allow anonymous authentication. If an auth.AnonymousAuth Authenticator
	// if not supplied, then router supplies on with AuthRole of "anonymous".
	AnonymousAuth bool `json:"anonymous_auth"`
	// Allow publisher and caller identity disclosure when requested.
	AllowDisclose bool `json:"allow_disclose"`
	// Slice of Authenticator interfaces.
	Authenticators []auth.Authenticator
	// Authorizer called for each message.
	Authorizer Authorizer
	// AuthorizeReadOnly calls the Authorizer with a shallow copy of the
	// session details and WITHOUT holding the session lock. By default the
	// session is write-locked for the duration of Authorize, because an
	// Authorizer may alter the session details (see Authorizer). The broker
	// also takes each subscriber's session lock while applying publish filters
	// (eligible/exclude) on its realm-wide goroutine, so an Authorizer that
	// blocks on the lock, may stalls event delivery for the whole realm.
	//
	// Set AuthorizeReadOnly only when the Authorizer never alters the session
	// details: with it set, changes to the session details are discarded. The
	// message itself may still be altered.
	AuthorizeReadOnly bool `json:"authorize_readonly"`
	// Require authentication for local clients. Normally local clients are
	// always trusted. Setting this treats local clients the same as remote.
	RequireLocalAuth bool `json:"require_local_auth"`
	// Require authorization for local clients. Normally local clients are
	// always authorized, even when the router has an authorizer. Setting this
	// treats local clients the same as remote.
	RequireLocalAuthz bool `json:"require_local_authz"`

	// When true, only include standard session details in on_join event and
	// session_get response. Standard details include: session, authid,
	// authrole, authmethod, transport. When false, all session details are
	// included, except transport.auth.
	MetaStrict bool `json:"meta_strict"`
	// When MetaStrict is true, MetaIncludeSessionDetails specifies session
	// details to include that are in addition to the standard details
	// specified by the WAMP specification. This is a list of the names of
	// additional session details values to include.
	MetaIncludeSessionDetails []string `json:"meta_include_session_details"`

	// EnableMetaKill enables the wamp.session.kill* session meta procedures.
	// These are disabled by default to avoid requiring Authorizer logic when
	// it may not be needed otherwise.
	EnableMetaKill bool `json:"enable_meta_kill"`
	// EnableMetaModify enables the wamp.session.modify_details session meta
	// procedure. This is disabled by default to avoid requiring Authorizer
	// logic when it may not be needed otherwise.
	EnableMetaModify bool `json:"enable_meta_modify"`

	// StrictRequestIDs enforces the WAMP rule for session scope IDs: the Request
	// field of PUBLISH, SUBSCRIBE, UNSUBSCRIBE, REGISTER, UNREGISTER and CALL
	// messages must start at 1 and increment by 1 per session, across all of these
	// message types. A non-sequential ID terminates the session with an ABORT
	// carrying wamp.error.protocol_violation. The CALL messages that continue a
	// progressive call reuse the request ID of the first CALL, and are allowed
	// while that call is in progress.
	//
	// Default is false to preserve the historical relaxed behavior. Clients that
	// allocate random IDs, as some WAMP libraries do, break in strict mode. Enable
	// per realm when all clients allocate sequential IDs.
	StrictRequestIDs bool `json:"strict_request_ids"`

	TopicEventHistoryConfigs []*TopicEventHistoryConfig `json:"event_history"`

	// PublishFilterFactory is a function used to create a PublishFilter to
	// check which sessions a publication should be sent to.
	//
	// This value is not set via json config, but is configured when embedding
	// nexus. A value of nil enables the default filtering.
	PublishFilterFactory FilterFactory
}

type TopicEventHistoryConfig struct {
	Topic       wamp.URI `json:"topic"` // Topic URI
	MatchPolicy string   `json:"match"` // Matching policy (exact|prefix|wildcard)
	Limit       int      `json:"limit"` // Number of most recent events to store
}
