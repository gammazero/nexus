package client

import (
	"log"
	"os"

	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/transport"
)

// ConnectLocal creates a new client directly connected to the router
// instance.  This is used to connect clients, embedded in the same application
// as the router, to the router.  Doing this eliminates the need for any socket
// of serialization overhead.  The new client joins the realm specified in the
// ClientConfig.
func ConnectLocal(router router.Router, cfg ClientConfig) (*Client, error) {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "", 0)
	}
	localSide, routerSide := transport.LinkedPeers(cfg.Logger)

	go func() {
		if err := router.Attach(routerSide); err != nil {
			cfg.Logger.Print(err)
		}
	}()

	return NewClient(localSide, cfg)
}
