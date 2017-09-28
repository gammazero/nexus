package client

import (
	"crypto/tls"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
)

// Define serialization consts in client package so that client code does not
// need to import the serialize package to get the consts.
const (
	JSON    = serialize.JSON
	MSGPACK = serialize.MSGPACK
)

// NewWebsocketClient creates a new websocket client connected to the WAMP
// router at the specified URL, using the requested serialization.  The new
// client joins the realm specified in the ClientConfig.
func NewWebsocketClient(url string, serialization serialize.Serialization, tlscfg *tls.Config, dial transport.DialFunc, cfg ClientConfig, logger stdlog.StdLog) (*Client, error) {
	p, err := transport.ConnectWebsocketPeer(url, serialization, tlscfg, dial,
		logger)
	if err != nil {
		return nil, err
	}
	return NewClient(p, cfg, logger)
}
