/*
Package newclient provides a function to create a new client with the socket
type and serialization specified by command like arguments.  This is used for
all the sample clients.

*/
package newclient

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/gammazero/nexus/client"
)

const (
	defaultRealm = "realm1"
	defaultAddr  = "localhost"
	defaultUnix  = "/tmp/exmpl_nexus_sock"

	defaultWsPort   = 8000
	defaultWssPort  = 8443
	defaultTcpPort  = 8080
	defaultTcpsPort = 8081
)

func NewClient(logger *log.Logger) (*client.Client, error) {
	var (
		skipVerify, compress bool

		port int

		addr, caFile, certFile, keyFile, realm, scheme, serType string
	)
	flag.StringVar(&addr, "addr", "",
		fmt.Sprintf("router address. (default %q or %q for network or unix socket)", defaultAddr, defaultUnix))
	flag.IntVar(&port, "port", 0,
		fmt.Sprintf("router port. (default %d, %d, %d, %d for scheme ws, wss, tcp, tcps)", defaultWsPort, defaultWssPort, defaultTcpPort, defaultTcpsPort))
	flag.StringVar(&realm, "realm", defaultRealm, "realm name")
	flag.StringVar(&scheme, "scheme", "ws", "[ws, wss, tcp, tcps, unix]")
	flag.StringVar(&serType, "serialize", "json", "\"json\" or \"msgpack\"")
	flag.BoolVar(&skipVerify, "skipverify", false,
		"accept any certificate presented by the server")
	flag.StringVar(&caFile, "trust", "",
		"CA or self-signed certificate to trust in PEM encoded file")
	flag.StringVar(&certFile, "cert", "",
		"certificate file with PEM encoded data")
	flag.StringVar(&keyFile, "key", "",
		"private key file with PEM encoded data")
	flag.BoolVar(&compress, "compress", false, "enable websocket compression")
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

	if addr == "" {
		if scheme == "unix" {
			addr = defaultUnix
		} else {
			addr = defaultAddr
		}
	}

	cfg := client.Config{
		Realm:         realm,
		Serialization: serialization,
		Logger:        logger,
	}

	if scheme == "https" || scheme == "wss" || scheme == "tcps" {
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
		// If not skipping verification and told to trust a certificate.
		if !skipVerify && caFile != "" {
			// Load PEM-encoded certificate to trust.
			certPEM, err := ioutil.ReadFile(caFile)
			if err != nil {
				return nil, err
			}
			// Create CertPool containing the certificate to trust.
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM(certPEM) {
				return nil, errors.New("failed to import certificate to trust")
			}
			// Trust the certificate by putting it into the pool of root CAs.
			tlscfg.RootCAs = roots

			// Decode and parse the server cert to extract the subject info.
			block, _ := pem.Decode(certPEM)
			if block == nil {
				return nil, errors.New("failed to decode certificate to trust")
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, err
			}
			log.Println("Trusting certificate", caFile, "with CN:",
				cert.Subject.CommonName)

			// Set ServerName in TLS config to CN from trusted cert so that
			// certificate will validate if CN does not match DNS name.
			tlscfg.ServerName = cert.Subject.CommonName
		}

		cfg.TlsCfg = tlscfg
	}
	if compress {
		cfg.WsCfg.EnableCompression = true
	}
	cfg.WsCfg.EnableTrackingCookie = true

	// Create client with requested transport type.
	var cli *client.Client
	var err error
	switch scheme {
	case "http", "ws":
		if port == 0 {
			port = defaultWsPort
		}
		addr = fmt.Sprintf("ws://%s:%d/ws", addr, port)
	case "https", "wss":
		if port == 0 {
			port = defaultWssPort
		}
		addr = fmt.Sprintf("wss://%s:%d/ws", addr, port)
	case "tcp":
		if port == 0 {
			port = defaultTcpPort
		}
		addr = fmt.Sprintf("tcp://%s:%d/", addr, port)
	case "tcps":
		if port == 0 {
			port = defaultTcpsPort
		}
		addr = fmt.Sprintf("tcps://%s:%d/", addr, port)
	case "unix":
		addr = fmt.Sprintf("unix://%s", addr)
	default:
		return nil, errors.New("scheme must be one of: http, https, ws, wss, tcp, tcps, unix")
	}
	cli, err = client.ConnectNet(addr, cfg)
	if err != nil {
		return nil, err
	}

	logger.Println("Connected to", addr, "using", serType, "serialization")
	return cli, nil
}
