package router

import (
	"github.com/gammazero/nexus/auth"
	"github.com/gammazero/nexus/wamp"
)

type RealmConfig struct {
	URI            wamp.URI
	StrictURI      bool `json:"strict_uri"`
	AnonymousAuth  bool `json:"anonymous_auth"`
	AllowDisclose  bool `json:"auto_disclose"`
	Authenticators map[string]auth.Authenticator
	Broker         bool `json:"broker"`
	Dealer         bool `json:"dealer"`
}
