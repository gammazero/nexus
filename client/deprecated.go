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
func NewLocalClient(router nexus.Router, cfg ClientConfig, logger stdlog.StdLog) (*Client, error) {
	cfg.Logger = logger
	return ConnectLocal(router, cfg)
}

// DEPRECATED - use ConnectNet with "ws://host/" or "wss://host/"
func NewWebsocketClient(url string, serialization serialize.Serialization, tlscfg *tls.Config, dial transport.DialFunc, cfg ClientConfig, logger stdlog.StdLog) (*Client, error) {
	cfg.Serialization = serialization
	cfg.Logger = logger
	cfg.TlsCfg = tlscfg
	return ConnectNet(url, cfg)
}

// DEPRECATED - use ConnectNet with "tcp://address/" or "unix://address"
func NewRawSocketClient(network, address string, serialization serialize.Serialization, cfg ClientConfig, logger stdlog.StdLog, recvLimit int) (*Client, error) {
	cfg.Serialization = serialization
	cfg.Logger = logger
	cfg.RecvLimit = recvLimit
	cfg.TlsCfg = nil
	switch network {
	case "tcp", "tcp4", "tcp6":
		return ConnectNet(fmt.Sprintf("tcp://%s/", address), cfg)
	case "unix":
		return ConnectNet(fmt.Sprintf("unix://%s", address), cfg)
	default:
		return nil, fmt.Errorf("unsupported network: %s", network)
	}
}

// DEPRECATED - use ConnectNet with "tcps://address/"
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
		return ConnectNet(fmt.Sprintf("tcps://%s/", address), cfg)
	default:
		return nil, fmt.Errorf("unsupported network: %s", network)
	}
}
