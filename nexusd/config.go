package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
)

type Config struct {
	Realms        []RealmConfig
	Port          int
	SendQueueSize int    `json:"send_queue_size"`
	LogPath       string `json:"log_path"`
	AutoRealm     bool   `json:"autocreate_realm"`
	StrictURI     bool   `json:"strict_uri"`
}

type RealmConfig struct {
	URI            string
	AllowAnonymous bool `json:"allow_anonymous"`
	AllowDisclose  bool `json:"allow_disclose"`
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

func PrintConfig(conf *Config) {
	fmt.Println()
	fmt.Println("nexus configuration")
	fmt.Println("-------------------")
	fmt.Println("port:", conf.Port)
	fmt.Println("log_path:", conf.LogPath)
	fmt.Println("autocreate_realm:", conf.AutoRealm)
	fmt.Println("strict_uri:", conf.StrictURI)
	for i := range conf.Realms {
		fmt.Println(conf.Realms[i].URI)
		fmt.Println("  allow_anonymous:", conf.Realms[i].AllowAnonymous)
		fmt.Println("  allow_disclose:", conf.Realms[i].AllowAnonymous)
	}
	fmt.Println()
}
