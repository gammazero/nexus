package client

import (
	"log"
	"os"

	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/wamp"
)

// ConnectTCP creates a new client connected a WAMP router over a TCP socket.
// The new client joins the realm specified in the ClientConfig.
//
// The address parameter specifes a network address (host and port) and has the
// form "host:port".  The host must be a literal IP address, or a host name
// that can be resolved to IP addresses.  The port must be a literal port
// number or a service name.  If the host is a literal IPv6 address it must be
// enclosed in square brackets, as in "[2001:db8::1]:80".  For details, see:
// https://golang.org/pkg/net/#Dial
func ConnectTCP(address string, cfg ClientConfig) (*Client, error) {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "", 0)
	}

	var p wamp.Peer
	var err error
	if cfg.TlsCfg == nil {
		p, err = transport.ConnectRawSocketPeer("tcp", address,
			cfg.Serialization, cfg.Logger, cfg.RecvLimit)
	} else {
		p, err = transport.ConnectTlsRawSocketPeer("tcp", address,
			cfg.Serialization, cfg.TlsCfg, cfg.Logger, cfg.RecvLimit)
	}
	if err != nil {
		return nil, err
	}
	return NewClient(p, cfg)
}

// ConnectUnix creates a new client connected a WAMP router over a Unix
// domain socket.  The new client joins the realm specified in the
// ClientConfig.  Any TLS configuration is ignored for Unix sockets.
//
// The Address parameter specifes a path on the loca file system where the Unix
// socket is created.
func ConnectUnix(address string, cfg ClientConfig) (*Client, error) {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "", 0)
	}

	p, err := transport.ConnectRawSocketPeer("unix", address,
		cfg.Serialization, cfg.Logger, cfg.RecvLimit)
	if err != nil {
		return nil, err
	}
	return NewClient(p, cfg)
}
