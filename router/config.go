package router

import (
	"fmt"

	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/router/broker"
	"github.com/gammazero/nexus/v3/wamp"
)

// Values for RealmConfig.EventOverflow.
const (
	// EventOverflowClose closes a client for which an EVENT does not fit.
	EventOverflowClose = "close"
	// EventOverflowDrop drops an EVENT that does not fit.
	EventOverflowDrop = "drop"
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
	// Interval in seconds for logging memory stats. 0 to disable. Logs Alloc,
	// Mallocs, Frees, and NumGC. For a description of these, see
	// https://golang.org/pkg/runtime/#MemStats
	MemStatsLogSec int `json:"mem_stats_log_sec"`
}

// RealmConfig configures a single realm in the router. The router
// configuration may specify a list of realms to configure.
type RealmConfig struct {
	// URI that identifies the realm.
	URI wamp.URI
	// Allow anonymous authentication. If an auth.AnonymousAuth Authenticator
	// is not supplied, then router supplies one with AuthRole of "anonymous".
	AnonymousAuth bool `json:"anonymous_auth"`
	// Allow publisher and caller identity disclosure when requested.
	AllowDisclose bool `json:"allow_disclose"`
	// Slice of Authenticator interfaces.
	Authenticators []auth.Authenticator
	// Authorizer called for each message.
	Authorizer Authorizer
	// Debug enables debug logging for this realm, and for its broker and dealer,
	Debug bool `json:"debug"`
	// EnableMetaKill enables the wamp.session.kill* session meta procedures. These
	// are disabled by default to avoid requiring Authorizer logic when it may not
	// be needed otherwise. Go code can kill sessions by authid or authrole with
	// SessionKiller, without enabling these procedures for the realm's clients.
	EnableMetaKill bool `json:"enable_meta_kill"`
	// EnableMetaModify enables the wamp.session.modify_details session meta
	// procedure. This is disabled by default to avoid requiring Authorizer
	// logic when it may not be needed otherwise.
	EnableMetaModify bool `json:"enable_meta_modify"`
	// EventHistory contains the event history configuration for each topic.
	EventHistory []*broker.TopicEventHistoryConfig `json:"event_history"`
	// EventOverflow selects what the router does with an EVENT for a client whose
	// outbound queue and backlog are full. With EventOverflowDrop ("drop"), the
	// default, the router drops the EVENT, and the client stays connected without
	// knowing that it missed it. With EventOverflowClose ("close"), the router
	// closes the client, with a GOODBYE carrying CloseSlowSubscriber if there is
	// room for it, so that the client knows that it fell behind and can
	// reconnect.
	//
	// Use EventOverflowClose for a realm whose clients must not miss EVENTs, and
	// that reconnect and resync when closed. Closing a client ends its whole
	// session, including its registrations and the calls in progress to it, and a
	// client that does not reconnect stops working, so EventOverflowDrop is the
	// default.
	EventOverflow string `json:"event_overflow"`
	// PublishFilterFactory is a function used to create a PublishFilter to
	// check which sessions a publication should be sent to.
	//
	// This value is not set via json config, but is configured when embedding
	// nexus. A value of nil enables the default filtering.
	PublishFilterFactory broker.FilterFactory
	// Require authentication for local clients. Normally local clients are
	// always trusted. Setting this treats local clients the same as remote.
	RequireLocalAuth bool `json:"require_local_auth"`
	// Require authorization for local clients. Normally local clients are
	// always authorized, even when the router has an authorizer. Setting this
	// treats local clients the same as remote.
	RequireLocalAuthz bool `json:"require_local_authz"`
	// When true, only include standard session details in on_join event and
	// session_get response. Standard details include: session, authid, authrole,
	// authmethod, authprovider, transport. When false, all session details are
	// included, except transport.auth.
	MetaStrict bool `json:"meta_strict"`
	// When MetaStrict is true, MetaIncludeSessionDetails specifies session
	// details to include that are in addition to the standard details
	// specified by the WAMP specification. This is a list of the names of
	// additional session details values to include.
	MetaIncludeSessionDetails []string `json:"meta_include_session_details"`
	// SendBacklog is the maximum number of messages that wait for each client, in
	// addition to those in the client's outbound queue, while the client reads
	// them more slowly than the router sends them. These are EVENTs, and the
	// INVOCATIONs, INTERRUPTs and progressive RESULTs that the router sends to the
	// client, which wait in order with the replies to the client's requests, and
	// are delivered once the client reads. A value of zero, the default, uses the
	// size of the client's outbound queue.
	//
	// When the backlog is full, an EVENT is handled as set by EventOverflow, a
	// call whose INVOCATION does not fit fails with wamp.error.network_failure, an
	// INTERRUPT is dropped, and a progressive RESULT is retried until the call is
	// canceled.
	SendBacklog int `json:"Send_backlog"`
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
	// Enforce strict URI format validation.
	StrictURI bool `json:"strict_uri"`
}

// validate returns an error if config is not valid for creating a realm.
func (c *RealmConfig) validate() error {
	if !c.URI.ValidURI(c.StrictURI, "") {
		return fmt.Errorf(
			"invalid realm URI %v (URI strict checking %v)", c.URI, c.StrictURI)
	}

	if c.SendBacklog < 0 {
		return fmt.Errorf("negative event backlog: %d", c.SendBacklog)
	}

	switch c.EventOverflow {
	case "", EventOverflowDrop, EventOverflowClose:
	default:
		return fmt.Errorf("invalid event overflow: %q", c.EventOverflow)
	}

	for _, topicCfg := range c.EventHistory {
		if err := topicCfg.Validate(c.StrictURI); err != nil {
			return err
		}
	}

	return nil
}
