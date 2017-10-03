package client

import (
	"log"
	"os"

	"github.com/gammazero/nexus"
	"github.com/gammazero/nexus/transport"
)

// NewLocalClient creates a new client directly connected to the router
// instance.  This is used to connect clients, embedded in the same application
// as the router, to the router.  Doing this eliminates the need for any socket
// of serialization overhead.  The new client joins the realm specified in the
// ClientConfig.
func ConnectLocal(router nexus.Router, cfg ClientConfig) (*Client, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "", 0)
	}
	localSide, routerSide := transport.LinkedPeers(logger)

	go func() {
		if err := router.Attach(routerSide); err != nil {
			logger.Print(err)
		}
	}()

	return NewClient(localSide, cfg, logger)
}
