package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// ConnectNet creates a new client connected a WAMP router over a websocket,
// TCP socket, or unix socket.  The new client joins the realm specified in the
// Config.  The context may be used to cancel or timeout connecting to a router.
//
// For websocket clients, the routerURL has the form "ws://host:port/" or
// "wss://host:port/", for websocket or websocket with TLS respectively.  The
// scheme "http" is interchangeable with "ws", and "https" is interchangeable
// with "wss".  The host:port portion is the same as for a TCP client.
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
// created.  TLS is not used for unix sockets.
func ConnectNet(ctx context.Context, routerURL string, cfg Config) (*Client, error) {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "", 0)
	}

	u, err := url.Parse(routerURL)
	if err != nil {
		return nil, err
	}

	var p wamp.Peer
	switch u.Scheme {
	case "http", "https":
		if u.Scheme == "http" {
			u.Scheme = "ws"
		} else {
			u.Scheme = "wss"
		}
		routerURL = u.String()
		fallthrough
	case "ws", "wss":
		p, err = transport.ConnectWebsocketPeer(ctx, routerURL,
			cfg.Serialization, cfg.TlsCfg, cfg.Logger, &cfg.WsCfg)
	case "tcps", "tcp4s", "tcp6s":
		u.Scheme = u.Scheme[:len(u.Scheme)-1]
		if cfg.TlsCfg == nil {
			cfg.TlsCfg = new(tls.Config)
		}
		fallthrough
	case "tcp", "tcp4", "tcp6":
		p, err = transport.ConnectRawSocketPeer(ctx, u.Scheme, u.Host,
			cfg.Serialization, cfg.TlsCfg, cfg.Logger, cfg.RecvLimit)
	case "unix":
		if cfg.TlsCfg != nil {
			return nil, fmt.Errorf("tls not supported for %s", u.Scheme)
		}
		// If a relative path was specified, u.Host is first part of path.
		addr := path.Clean(u.Host + u.Path)
		p, err = transport.ConnectRawSocketPeer(ctx, u.Scheme, addr,
			cfg.Serialization, nil, cfg.Logger, cfg.RecvLimit)
	default:
		err = fmt.Errorf("invalid url: %s", routerURL)
	}
	if err != nil {
		return nil, err
	}
	return NewClient(p, cfg)
}

// CookieURL takes a websocket URL string and outputs a url.URL that can be
// used to retrieve cookies from a http.CookieJar as may be provided in
// Config.WsCfg.Jar.
func CookieURL(routerURL string) (*url.URL, error) {
	u, err := url.Parse(routerURL)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
		// Ok already; do nothing
	default:
		return nil, fmt.Errorf("scheme not valid for websocket: %s", u.Scheme)
	}

	return u, nil
}
