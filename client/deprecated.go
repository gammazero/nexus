package client

import (
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/gammazero/nexus"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
)

// DEPRECATED - use ConnectLocal
func NewLocalClient(router nexus.Router, cfg ClientConfig, logger stdlog.StdLog) (*Client, error) {
	cfg.Logger = logger
	return ConnectLocal(router, cfg)
}

// DEPRECATED - use ConnectWebsocket
func NewWebsocketClient(url string, serialization serialize.Serialization, tlscfg *tls.Config, dial transport.DialFunc, cfg ClientConfig, logger stdlog.StdLog) (*Client, error) {
	cfg.Serialization = serialization
	cfg.Logger = logger
	cfg.TlsCfg = tlscfg
	return ConnectWebsocket(strings.TrimLeft(url, "ws:/"), cfg)
}

// DEPRECATED - use ConnectTCP or ConnectUnix
func NewRawSocketClient(network, address string, serialization serialize.Serialization, cfg ClientConfig, logger stdlog.StdLog, recvLimit int) (*Client, error) {
	cfg.Serialization = serialization
	cfg.Logger = logger
	cfg.RecvLimit = recvLimit
	cfg.TlsCfg = nil
	return connectRaw(network, address, cfg)
}

// DEPRECATED - use ConnectTCP or ConnectUnix
func NewTlsRawSocketClient(network, address string, serialization serialize.Serialization, tlscfg *tls.Config, cfg ClientConfig, logger stdlog.StdLog, recvLimit int) (*Client, error) {
	cfg.Serialization = serialization
	cfg.Logger = logger
	cfg.RecvLimit = recvLimit
	if tlscfg == nil {
		tlscfg = &tls.Config{}
	}
	cfg.TlsCfg = tlscfg
	return connectRaw(network, address, cfg)
}

func connectRaw(network, address string, cfg ClientConfig) (*Client, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
		return ConnectTCP(address, cfg)
	case "unix":
		return ConnectUnix(address, cfg)
	default:
		return nil, fmt.Errorf("unsupported network: %s", network)
	}
}
