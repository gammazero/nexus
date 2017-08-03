package client

import (
	"crypto/tls"
	"time"

	"github.com/gammazero/nexus/logger"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
)

// NewWebsocketClient creates a new websocket client connected to the specified
// URL and using the specified serialization.
//
// JoinRealm must be called before other client functions.
func NewWebsocketClient(url string, serialization serialize.Serialization, tlscfg *tls.Config, dial transport.DialFunc, responseTimeout time.Duration, logger logger.Logger) (*Client, error) {
	p, err := transport.ConnectWebsocketPeer(url, serialization, tlscfg, dial, 0, logger)
	if err != nil {
		return nil, err
	}
	return NewClient(p, responseTimeout, logger), nil
}
