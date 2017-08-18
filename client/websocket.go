package client

import (
	"crypto/tls"
	"time"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
)

const (
	JSON    = serialize.JSON
	MSGPACK = serialize.MSGPACK
)

// NewWebsocketClient creates a new websocket client connected to the specified
// URL and using the specified serialization.
//
// JoinRealm must be called before other client functions.
func NewWebsocketClient(url string, serialization serialize.Serialization, tlscfg *tls.Config, dial transport.DialFunc, responseTimeout time.Duration, logger stdlog.StdLog) (*Client, error) {
	p, err := transport.ConnectWebsocketPeer(url, serialization, tlscfg, dial, logger)
	if err != nil {
		return nil, err
	}
	return NewClient(p, responseTimeout, logger), nil
}
