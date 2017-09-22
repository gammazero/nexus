package client

import (
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
)

// NewRawSocketClient creates a new rawsocket client connected to the specified
// address and using the specified serialization.  The new client joins the
// realm specified in the ClientConfig.
func NewRawSocketClient(network, address string, serialization serialize.Serialization, cfg ClientConfig, logger stdlog.StdLog, recvLimit int) (*Client, error) {
	p, err := transport.ConnectRawSocketPeer(network, address, serialization, logger, recvLimit)
	if err != nil {
		return nil, err
	}
	return NewClient(p, cfg, logger)
}
