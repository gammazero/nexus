package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"time"

	"github.com/gammazero/nexus/router"
)

type Config struct {
	// Websocket configuration parameters.
	WebSocket struct {
		// String form of address (example, "192.0.2.1:25", "[2001:db8::1]:80")
		Address string `json:"address"`
		// Files containing a certificate and matching private key.
		CertFile string `json:"cert_file"`
		KeyFile  string `json:"key_file"`
		// Enable per message write compression.
		EnableCompression    bool `json:"enable_compression"`
		AllowContextTakeover bool `json:"allow_context_takeover"`
		// Enable sending cookie to identify client in later connections.
		EnableTrackingCookie bool `json:"enable_tracking_cookie"`
		// Enable reading HTTP header from client requests.
		EnableRequestCapture bool `json:"enable_request_capture"`
		// Allow origins that match these glob patterns when an origin header
		// is present in the websocket upgrade request.
		AllowOrigins []string `json:"allow_origins"`
	}

	// RawSocket configuration parameters.
	RawSocket struct {
		// String form of address (example, "192.0.2.1:25", "[2001:db8::1]:80")
		TCPAddress string `json:"tcp_address"`
		// TCP keepalive interval in seconds.  Set to 0 to disable.
		TCPKeepAliveInterval time.Duration `json:"tcp_keepalive_interval"`
		// Path to Unix domain socket.
		UnixAddress string `json:"unix_address"`
		// Maximum message length server can receive. Default = 16M.
		MaxMsgLen int `json:"max_msg_len"`
		// Files containing a certificate and matching private key.
		CertFile string `json:"cert_file"`
		KeyFile  string `json:"key_file"`
	}

	// File to write log data to.  If not specified, log to stdout.
	LogPath string `json:"log_path"`
	// Router configuration parameters.
	// See https://godoc.org/github.com/gammazero/nexus#RouterConfig
	Router router.RouterConfig
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

	if config.RawSocket.TCPKeepAliveInterval != 0 {
		config.RawSocket.TCPKeepAliveInterval *= time.Second
	}
	return &config
}
