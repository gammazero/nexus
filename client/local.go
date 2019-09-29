package client

import (
	"log"
	"os"

	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/transport"
)

// ConnectLocal creates a new client directly connected to the router instance.
// This is used to connect clients, embedded in the same application as the
// router, to the router.  Doing this eliminates the need for any socket of
// serialization overhead, and does not require authentication.  The new client
// joins the realm specified in the Config.
func ConnectLocal(router router.Router, cfg Config) (*Client, error) {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "", 0)
	}
	localSide, routerSide := transport.LinkedPeers()

	go func() {
		if err := router.Attach(routerSide); err != nil {
			cfg.Logger.Print(err)
		}
	}()

	return NewClient(localSide, cfg)
}
