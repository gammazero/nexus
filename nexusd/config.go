package main

import (
	"encoding/json"
	"io/ioutil"
	"log"

	"github.com/gammazero/nexus"
)

type Config struct {
	WebSocket struct {
		Address  string `json:"address"`
		CertFile string `json:"cert_file"`
		KeyFile  string `json:"key_file"`
	}

	RawSocket struct {
		TCPAddress   string `json:"tcp_address"`
		TCPKeepAlive bool   `json:"tcp_keepalive"`
		UnixAddress  string `json:"unix_address"`
		MaxMsgLen    int    `json:"max_msg_len"`
	}

	LogPath string `json:"log_path"`
	Router  nexus.RouterConfig
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
