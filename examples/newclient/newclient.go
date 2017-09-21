/*
Package newclient provides a function to create a new client with the socket
type and serialization specified by command like arguments.  This is used for
all the sample clients.

*/
package newclient

import (
	"errors"
	"flag"
	"log"

	"github.com/gammazero/nexus/client"
)

const (
	webAddr  = "ws://localhost:8000/"
	tcpAddr  = "127.0.0.1:8001"
	unixAddr = "/tmp/exmpl_nexus_sock"
)

func NewClient(logger *log.Logger) (*client.Client, error) {
	var sockType, serType string
	flag.StringVar(&sockType, "socket", "web",
		"-socket=[web, tcp, unix].  Default is web")
	flag.StringVar(&serType, "serialize", "",
		"-serialize[json, msgpack] or none for socket default")
	flag.Parse()

	// Get requested serialization.
	serialization := client.JSON
	switch serType {
	case "":
		if sockType != "web" {
			serType = "msgpack"
			serialization = client.MSGPACK
		} else {
			serType = "json"
		}
	case "msgpack":
		serialization = client.MSGPACK
	case "json":
	default:
		return nil, errors.New(
			"invalid serialization, muse be one of: json, msgpack")
	}

	cfg := client.ClientConfig{
		Realm: "nexus.examples",
	}

	// Create client with requested transport type.
	var cli *client.Client
	var err error
	switch sockType {
	case "web":
		cli, err = client.NewWebsocketClient(
			webAddr, serialization, nil, nil, cfg, logger)
	case "tcp":
		cli, err = client.NewRawSocketClient(
			"tcp", tcpAddr, serialization, cfg, logger, 0)
	case "unix":
		cli, err = client.NewRawSocketClient(
			"unix", unixAddr, serialization, cfg, logger, 0)
	default:
		err = errors.New("invalid type, must one of: web, tcp, unix")
	}
	if err != nil {
		return nil, err
	}
	logger.Println("Connected using", sockType, "socket with", serType,
		"serialization")
	return cli, nil
}
