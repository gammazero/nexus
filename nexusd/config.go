package main

import (
	"encoding/json"
	"io/ioutil"
	"log"

	"github.com/gammazero/nexus/router"
)

type Config struct {
	Port          int
	SendQueueSize int    `json:"send_queue_size"`
	LogPath       string `json:"log_path"`
	Router        router.RouterConfig
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
