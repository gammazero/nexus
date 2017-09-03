package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gammazero/nexus"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-c nexus.json]\n", os.Args[0])
}

func main() {
	var cfgFile string
	fs := flag.NewFlagSet("nexus", flag.ExitOnError)
	fs.StringVar(&cfgFile, "c", "etc/nexus.json", "Path to config file")
	fs.Usage = usage
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}
	// Read config file.
	conf := LoadConfig(cfgFile)

	var logger *log.Logger
	if conf.LogPath == "" {
		// If no log file specified, then log to stdout.
		logger = log.New(os.Stdout, "", log.LstdFlags)
	} else {
		// Open the file to log to and set up logger.
		f, err := os.OpenFile(conf.LogPath, os.O_RDWR|os.O_CREATE|os.O_APPEND,
			0644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		logger = log.New(f, "", log.LstdFlags)
	}

	// Create router and realms from config.
	r, err := nexus.NewRouter(&conf.Router, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Create a new websocket server with the router.
	s := nexus.NewWebsocketServer(r)

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)

	go func() {
		// Shutdown server is SIGINT received.
		<-shutdown
		fmt.Println("Shutting down router...")

		// If process does not exit in a few seconds, exit with error.
		exitChan := make(chan struct{})
		go func() {
			select {
			case <-time.After(5 * time.Second):
				fmt.Fprintln(os.Stderr, "Router took too long to stop")
				os.Exit(1)
			case <-exitChan:
			}
		}()

		s.Close()
		close(exitChan)
		os.Exit(0)
	}()

	// Run service on configured port.
	server := &http.Server{
		Handler: s,
		Addr:    fmt.Sprintf(":%d", conf.Port),
	}
	fmt.Println("Router starting on port", conf.Port)
	log.Fatalln(server.ListenAndServe())
}
