package router

import (
	"github.com/gammazero/nexus/auth"
	"github.com/gammazero/nexus/wamp"
)

type RealmConfig struct {
	URI            wamp.URI
	StrictURI      bool
	AnonymousAuth  bool
	AllowDisclose  bool
	Authenticators map[string]auth.Authenticator
	Broker         bool
	Dealer         bool
}
