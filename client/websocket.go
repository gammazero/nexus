package client

import (
	"crypto/tls"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
)

const (
	JSON    = serialize.JSON
	MSGPACK = serialize.MSGPACK
)

// NewWebsocketClient creates a new websocket client connected to the specified
// URL and using the specified serialization.  The new client joins the realm
// specified in the ClientConfig.
func NewWebsocketClient(url string, serialization serialize.Serialization, tlscfg *tls.Config, dial transport.DialFunc, cfg ClientConfig, logger stdlog.StdLog) (*Client, error) {
	p, err := transport.ConnectWebsocketPeer(url, serialization, tlscfg, dial,
		logger)
	if err != nil {
		return nil, err
	}
	return NewClient(p, cfg, logger)
}
