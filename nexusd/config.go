package main

import (
	"encoding/json"
	"io/ioutil"
	"log"

	"github.com/gammazero/nexus"
)

type Config struct {
	// Websocket configuration parameters.
	WebSocket struct {
		// String form of address (example, "192.0.2.1:25", "[2001:db8::1]:80")
		Address string `json:"address"`
		// Files containing a certificate and matching private key.
		CertFile string `json:"cert_file"`
		KeyFile  string `json:"key_file"`
	}

	// RawSocket configuration parameters.
	RawSocket struct {
		// String form of address (example, "192.0.2.1:25", "[2001:db8::1]:80")
		TCPAddress string `json:"tcp_address"`
		// True to enable TCP keepalive
		TCPKeepAlive bool `json:"tcp_keepalive"`
		// Path to Unix domain socket.
		UnixAddress string `json:"unix_address"`
		// Maximum message length server can receive. Default = 16M.
		MaxMsgLen int `json:"max_msg_len"`
	}

	// File to write log data to.  If not specified, log to stdout.
	LogPath string `json:"log_path"`
	// Router configuration parameters.
	// See https://godoc.org/github.com/gammazero/nexus#RouterConfig
	Router nexus.RouterConfig
}

func LoadConfig(path string) *Config {
	file, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatal("Config File Missing. ", err)
	}

	var config Config
	err = json.Unmarshal(file, &config)
	if err != nil {
		log.Fatal("Config Parse Error: ", err)
	}

	return &config
}
