package client

import (
	"crypto/tls"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
)

// NewRawSocketClient creates a new rawsocket client connected to the specified
// network and address and using the specified serialization.  The new client
// joins the realm specified in the ClientConfig.
//
// The network must be "tcp", "tcp4", "tcp6", or "unix".  The address has the
// form "host:port". The host must be a literal IP address, or a host name that
// can be resolved to IP addresses. The port must be a literal port number or a
// service name. If the host is a literal IPv6 address it must be enclosed in
// square brackets, as in "[2001:db8::1]:80".  For details, see:
// https://golang.org/pkg/net/#Dial
func NewRawSocketClient(network, address string, serialization serialize.Serialization, cfg ClientConfig, logger stdlog.StdLog, recvLimit int) (*Client, error) {
	p, err := transport.ConnectRawSocketPeer(network, address, serialization, logger, recvLimit)
	if err != nil {
		return nil, err
	}
	return NewClient(p, cfg, logger)
}

// NewTlsRawSocketClient creates a new rawsocket client connected using TLS to the specified TCP address and using the specified serialization.  The new client joins the realm specified in the ClientConfig.  A nil TLS configuration as equivalent to the zero configuration.
func NewTlsRawSocketClient(network, address string, serialization serialize.Serialization, tlscfg *tls.Config, cfg ClientConfig, logger stdlog.StdLog, recvLimit int) (*Client, error) {
	p, err := transport.ConnectTlsRawSocketPeer(
		network, address, serialization, tlscfg, logger, recvLimit)
	if err != nil {
		return nil, err
	}
	return NewClient(p, cfg, logger)
}
