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
	webAddr    = "ws://127.0.0.1:8000"
	webAddrTLS = "wss://127.0.0.1:8100"
	tcpAddr    = "tcp://127.0.0.1:8001"
	tcpAddrTLS = "tcps://127.0.0.1:8101"
	unixAddr   = "unix:///tmp/exmpl_nexus_sock"
)

func NewClient(logger *log.Logger) (*client.Client, error) {
	var useTLS, skipVerify bool
	var sockType, serType, certFile, keyFile string
	flag.StringVar(&sockType, "socket", "web",
		"-socket=[web, tcp, unix].  Default is web")
	flag.StringVar(&serType, "serialize", "json",
		"-serialize[json, msgpack] or none for socket default")
	flag.BoolVar(&useTLS, "tls", false, "communicate using TLS")
	flag.BoolVar(&skipVerify, "skipverify", false,
		"accept any certificate presented by the server")
	flag.StringVar(&certFile, "cert", "",
		"certificate file with PEM encoded data")
	flag.StringVar(&keyFile, "key", "",
		"private key file with PEM encoded data")
	flag.Parse()

	// If TLS requested, then set up TLS configuration.
	var tlscfg *tls.Config
	if useTLS {
		tlscfg = &tls.Config{
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
	}

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
		TlsCfg:        tlscfg,
		Serialization: serialization,
		Logger:        logger,
	}

	// Create client with requested transport type.
	var cli *client.Client
	var addr string
	var err error
	switch sockType {
	case "web":
		if useTLS {
			addr = webAddrTLS
		} else {
			addr = webAddr
		}
	case "tcp":
		if useTLS {
			addr = tcpAddrTLS
		} else {
			addr = tcpAddr
		}
	case "unix":
		addr = unixAddr
	default:
		return nil, errors.New("socket must be one of: web, tcp, unix")
	}
	cli, err = client.ConnectNet(addr, cfg)
	if err != nil {
		return nil, err
	}

	if useTLS {
		sockType = "TLS " + sockType
	}
	logger.Println("Connected using", sockType, "socket with", serType,
		"serialization")
	return cli, nil
}
