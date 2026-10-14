package broker

import (
	"github.com/gammazero/nexus/v3/wamp"
)

type TopicEventHistoryConfig struct {
	Topic       wamp.URI `json:"topic"` // Topic URI
	MatchPolicy string   `json:"match"` // Matching policy (exact|prefix|wildcard)
	Limit       int      `json:"limit"` // Number of most recent events to store
}
