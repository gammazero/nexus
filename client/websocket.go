package client

import (
	"fmt"
	"log"
	"os"

	"github.com/gammazero/nexus/transport"
)

// ConnectWebsocket creates a new websocket client connected to the WAMP
// router at the specified address, using the requested serialization.  The new
// client joins the realm specified in the ClientConfig.
//
// The address parameter specifies a network address (host and port) and has
// the form "host:port".  The host must be a literal IP address, or a host name
// that can be resolved to IP addresses.  The port must be a literal port
// number or a service name.  If the host is a literal IPv6 address it must be
// enclosed in square brackets, as in "[2001:db8::1]:80".  For details, see:
// https://golang.org/pkg/net/#Dial
func ConnectWebsocket(address string, cfg ClientConfig) (*Client, error) {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "", 0)
	}
	p, err := transport.ConnectWebsocketPeer(fmt.Sprintf("ws://%s/", address),
		cfg.Serialization, cfg.TlsCfg, cfg.Dial, cfg.Logger)
	if err != nil {
		return nil, err
	}
	return NewClient(p, cfg)
}
