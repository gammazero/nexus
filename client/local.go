package client

import (
	"time"

	"github.com/gammazero/nexus/logger"
	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/transport"
)

// NewLocalClient creates a new client directly connected to embedded router
//
// JoinRealm must be called before other client functions.
func NewLocalClient(router router.Router, responseTimeout time.Duration, logger logger.Logger) (*Client, error) {
	localSide, routerSide := transport.LinkedPeers(logger)

	go func() {
		if err := router.Attach(routerSide); err != nil {
			logger.Print(err)
		}
	}()

	return NewClient(localSide, responseTimeout, logger), nil
}
