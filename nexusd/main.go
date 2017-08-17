package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/server"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-c nexus.json]\n", os.Args[0])
}

func main() {
	var cfgFile string
	fs := flag.NewFlagSet("nexus", flag.ExitOnError)
	fs.StringVar(&cfgFile, "c", "nexus.json", "Path to config file")
	fs.Usage = usage
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}
	// Read config file.
	conf := LoadConfig(cfgFile)

	// Create router and realms from config.
	r, err := router.NewRouter(&conf.Router, nil)
	if err != nil {
		log.Fatalln(err)
	}

	// Create a new websocket server with the router.
	s := server.NewWebsocketServer(r)

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)

	go func() {
		// Shutdown server is SIGINT received.
		<-shutdown
		log.Println("shutting down server...")
		s.Close()
		// If process does not exit in a few seconds, exit with error.
		time.Sleep(3 * time.Second)
		os.Exit(1)
	}()

	// Run server on configured port.
	server := &http.Server{
		Handler: s,
		Addr:    fmt.Sprintf(":%d", conf.Port),
	}
	log.Printf("server starting on port %d...", conf.Port)
	log.Fatalln(server.ListenAndServe())
}
