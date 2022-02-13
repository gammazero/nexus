package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"time"

	"github.com/gammazero/nexus/v3/router"
)

type Config struct {
	// Websocket configuration parameters.
	WebSocket struct {
		// String form of address (example, "192.0.2.1:25", "[2001:db8::1]:80")
		Address string `json:"address"`
		// Files containing a certificate and matching private key.
		CertFile string `json:"cert_file"`
		KeyFile  string `json:"key_file"`
		// Heartbeat ("pings") interval in seconds.  Set to 0 to disable.
		KeepAlive time.Duration `json:"keep_alive"`
		// Enable per message write compression.
		EnableCompression bool `json:"enable_compression"`
		// Enable sending cookie to identify client in later connections.
		EnableTrackingCookie bool `json:"enable_tracking_cookie"`
		// Enable reading HTTP header from client requests.
		EnableRequestCapture bool `json:"enable_request_capture"`
		// Allow origins that match these glob patterns when an origin header
		// is present in the websocket upgrade request.
		AllowOrigins []string `json:"allow_origins"`
		// Limit on number of pending messages to send to each client.
		OutQueueSize int `json:"out_queue_size"`
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
		// Limit on number of pending messages to send to each client.
		OutQueueSize int `json:"out_queue_size"`
	}

	Metrics struct {
		Address string `json:"address"`
	} `json:"metrics"`

	// File to write log data to.  If not specified, log to stdout.
	LogPath string `json:"log_path"`
	// Router configuration parameters.
	// See https://godoc.org/github.com/gammazero/nexus#RouterConfig
	Router router.Config
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

	if config.WebSocket.KeepAlive != 0 {
		config.WebSocket.KeepAlive *= time.Second
	}
	if config.RawSocket.TCPKeepAliveInterval != 0 {
		config.RawSocket.TCPKeepAliveInterval *= time.Second
	}
	return &config
}
