package client

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/wamp"
)

// ConnectNet creates a new client connected a WAMP router over a websocket,
// TCP socket, or unix socket.  The new client joins the realm specified in the
// ClientConfig.
//
// For websocket clients, the routerURL has the form "ws://host:port/" or
// "wss://host:port/", for websocket or websocket with TLS respectively.  The
// host:port portion is the same as for a TCP clients.
//
// For TCP clients, the router URL has the form "tcp://host:port/" or
// "tcps://host:port/", for TCP socket or TCP socket with TLS respectively.
// The host must be a literal IP address, or a host name that can be resolved
// to IP addresses.  The port must be a literal port number or a service name.
// If the host is a literal IPv6 address it must be enclosed in square
// brackets, as in "[2001:db8::1]:80".  For details, see:
// https://golang.org/pkg/net/#Dial
//
// For Unix socket clients, the routerURL has the form "unix://path".  The path
// portion specifies a path on the local file system where the Unix socket is
// created.  TLS is not used for unix socket.
func ConnectNet(routerURL string, cfg ClientConfig) (*Client, error) {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "", 0)
	}

	u, err := url.Parse(routerURL)
	if err != nil {
		return nil, err
	}
	var p wamp.Peer
	switch u.Scheme {
	case "ws", "wss":
		p, err = transport.ConnectWebsocketPeer(routerURL, cfg.Serialization,
			cfg.TlsCfg, cfg.Dial, cfg.Logger)
	case "tcp":
		p, err = transport.ConnectRawSocketPeer(u.Scheme, u.Host,
			cfg.Serialization, cfg.Logger, cfg.RecvLimit)
	case "tcps":
		p, err = transport.ConnectTlsRawSocketPeer("tcp", u.Host,
			cfg.Serialization, cfg.TlsCfg, cfg.Logger, cfg.RecvLimit)
	case "unix":
		path := strings.TrimRight(u.Host+u.Path, "/")
		p, err = transport.ConnectRawSocketPeer(u.Scheme, path,
			cfg.Serialization, cfg.Logger, cfg.RecvLimit)
	default:
		err = fmt.Errorf("invalid url: %s", routerURL)
	}
	if err != nil {
		return nil, err
	}
	return NewClient(p, cfg)
}
