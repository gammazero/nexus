package broker

import (
	"github.com/gammazero/nexus/v3/wamp"
)

type Config struct {
	// AllowDisclose allows publisher identity disclosure when requested.
	AllowDisclose bool `json:"allow_disclose"`
	// Debug enables debug logging for this broker,
	Debug bool `json:"debug"`
	// EventHistory contains the event history configuration for each topic.
	EventHistory []*TopicEventHistoryConfig `json:"event_history"`
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
	PublishFilterFactory FilterFactory
	// StrictURI enforces strict URI format validation when set.
	StrictURI bool `json:"strict_uri"`
}

// Values for RealmConfig.EventOverflow.
const (
	// EventOverflowClose closes a client for which an EVENT does not fit.
	EventOverflowClose = "close"
	// EventOverflowDrop drops an EVENT that does not fit.
	EventOverflowDrop = "drop"
)

type TopicEventHistoryConfig struct {
	Topic       wamp.URI `json:"topic"` // Topic URI
	MatchPolicy string   `json:"match"` // Matching policy (exact|prefix|wildcard)
	Limit       int      `json:"limit"` // Number of most recent events to store
}
