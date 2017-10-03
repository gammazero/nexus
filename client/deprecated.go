package client

import (
	"crypto/tls"
	"fmt"

	"github.com/gammazero/nexus"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
)

// DEPRECATED - use ConnectLocal
//
// NewLocalClient creates a new client directly connected to the router
// instance.  This is used to connect clients, embedded in the same application
// as the router, to the router.  Doing this eliminates the need for any socket
// of serialization overhead.  The new client joins the realm specified in the
// ClientConfig.
func NewLocalClient(router nexus.Router, cfg ClientConfig, logger stdlog.StdLog) (*Client, error) {
	localSide, routerSide := transport.LinkedPeers(logger)

	go func() {
		if err := router.Attach(routerSide); err != nil {
			logger.Print(err)
		}
	}()

	return NewClient(localSide, cfg, logger)
}

// DEPRECATED - use ConnectWebsocket
//
// NewWebsocketClient creates a new websocket client connected to the WAMP
// router at the specified URL, using the requested serialization.  The new
// client joins the realm specified in the ClientConfig.
func NewWebsocketClient(url string, serialization serialize.Serialization, tlscfg *tls.Config, dial transport.DialFunc, cfg ClientConfig, logger stdlog.StdLog) (*Client, error) {
	cfg.Serialization = serialization
	cfg.Logger = logger
	cfg.TlsCfg = tlscfg
	p, err := transport.ConnectWebsocketPeer(url, serialization, tlscfg, dial,
		logger)
	if err != nil {
		return nil, err
	}
	return NewClient(p, cfg, logger)
}

// DEPRECATED - use ConnectTCP or ConnectUnix
//
// NewRawSocketClient creates a new rawsocket client connected to the WAMP
// router at network and address, using the requested serialization.  The new
// client joins the realm specified in the ClientConfig.
//
// The network must be "tcp", "tcp4", "tcp6", or "unix".  The address has the
// form "host:port".  The host must be a literal IP address, or a host name
// that can be resolved to IP addresses.  The port must be a literal port
// number or a service name.  If the host is a literal IPv6 address it must be
// enclosed in square brackets, as in "[2001:db8::1]:80".  For details, see:
// https://golang.org/pkg/net/#Dial
func NewRawSocketClient(network, address string, serialization serialize.Serialization, cfg ClientConfig, logger stdlog.StdLog, recvLimit int) (*Client, error) {
	cfg.Serialization = serialization
	cfg.Logger = logger
	cfg.RecvLimit = recvLimit
	cfg.TlsCfg = nil
	switch network {
	case "tcp", "tcp4", "tcp6":
		return ConnectTCP(address, cfg)
	case "unix":
		return ConnectUnix(address, cfg)
	default:
		return nil, fmt.Errorf("unsupported network: %s", network)
	}
}

// DEPRECATED - use ConnectTCP or ConnectUnix
//
// NewTlsRawSocketClient creates a new rawsocket client connected using TLS to
// the WAMP router at network and address, using the requested serialization.
// The new client joins the realm specified in the ClientConfig.
//
// A nil TLS configuration is equivalent to the zero configuration.  This is
// generally sutiable for clients, unless client certificates are required, or
// some other TLS configuration is needed.
//
// NOTE: Although allowed by this function, it is generally not useful to use
// TLS over Unix sockets.
func NewTlsRawSocketClient(network, address string, serialization serialize.Serialization, tlscfg *tls.Config, cfg ClientConfig, logger stdlog.StdLog, recvLimit int) (*Client, error) {
	cfg.Serialization = serialization
	cfg.Logger = logger
	cfg.RecvLimit = recvLimit
	if tlscfg == nil {
		tlscfg = &tls.Config{}
	}
	cfg.TlsCfg = tlscfg

	switch network {
	case "tcp", "tcp4", "tcp6":
		return ConnectTCP(address, cfg)
	case "unix":
		return ConnectUnix(address, cfg)
	default:
		return nil, fmt.Errorf("unsupported network: %s", network)
	}
}
