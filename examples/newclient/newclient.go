/*
Package newclient provides a function to create a new client with the socket
type and serialization specified by command like arguments.  This is used for
all the sample clients.

*/
package newclient

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"

	"github.com/gammazero/nexus/client"
)

const (
	wsAddr   = "127.0.0.1:8000"
	wssAddr  = "localhost:8100"
	tcpAddr  = "127.0.0.1:8001"
	tcpsAddr = "localhost:8101"
	unixAddr = "/tmp/exmpl_nexus_sock"
)

func NewClient(logger *log.Logger) (*client.Client, error) {
	var skipVerify bool
	var scheme, serType, certFile, keyFile string
	flag.StringVar(&scheme, "scheme", "ws",
		"-scheme=[ws, wss, tcp, tcps, unix].  Default is ws (websocket no tls)")
	flag.StringVar(&serType, "serialize", "json",
		"-serialize[json, msgpack] or none for socket default")
	flag.BoolVar(&skipVerify, "skipverify", false,
		"accept any certificate presented by the server")
	flag.StringVar(&certFile, "cert", "",
		"certificate file with PEM encoded data")
	flag.StringVar(&keyFile, "key", "",
		"private key file with PEM encoded data")
	flag.Parse()

	// Get requested serialization.
	serialization := client.JSON
	switch serType {
	case "json":
	case "msgpack":
		serialization = client.MSGPACK
	default:
		return nil, errors.New(
			"invalid serialization, muse be one of: json, msgpack")
	}

	cfg := client.ClientConfig{
		Realm:         "nexus.examples",
		Serialization: serialization,
		Logger:        logger,
	}

	if scheme == "wss" || scheme == "tcps" {
		// If TLS requested, then set up TLS configuration.
		tlscfg := &tls.Config{
			InsecureSkipVerify: skipVerify,
		}
		// If asked to load a client certificate to present to server.
		if certFile != "" || keyFile != "" {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return nil, fmt.Errorf("error loading X509 key pair: %s", err)
			}
			tlscfg.Certificates = append(tlscfg.Certificates, cert)
		}
		cfg.TlsCfg = tlscfg
	}

	// Create client with requested transport type.
	var cli *client.Client
	var addr string
	var err error
	switch scheme {
	case "ws":
		addr = fmt.Sprintf("%s://%s/", scheme, wsAddr)
	case "wss":
		addr = fmt.Sprintf("%s://%s/", scheme, wssAddr)
	case "tcp":
		addr = fmt.Sprintf("%s://%s/", scheme, tcpAddr)
	case "tcps":
		addr = fmt.Sprintf("%s://%s/", scheme, tcpsAddr)
	case "unix":
		addr = fmt.Sprintf("%s://%s", scheme, unixAddr)
	default:
		return nil, errors.New("scheme must be one of: ws, wss, tcp, tcps, unix")
	}
	cli, err = client.ConnectNet(addr, cfg)
	if err != nil {
		return nil, err
	}

	logger.Println("Connected to", addr, "using", serType, "serialization")
	return cli, nil
}
