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
	"github.com/gammazero/nexus/wamp"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-c nexus.json]\n", os.Args[0])
}

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	router.SetLogger(logger)

	var cfgFile string
	fs := flag.NewFlagSet("nexus", flag.ExitOnError)
	fs.StringVar(&cfgFile, "c", "nexus.json", "Path to config file")
	fs.Usage = usage
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}
	// Read config file.
	conf := LoadConfig(cfgFile)
	if len(conf.Realms) == 0 && !conf.AutoRealm {
		PrintConfig(conf)
		log.Fatalln("server requires at least one realm, or autocreate_realm must be enabled")
	}

	// Create router and realms from config.
	r := router.NewRouter(conf.AutoRealm, conf.StrictURI)
	for i := range conf.Realms {
		r.AddRealm(wamp.URI(conf.Realms[i].URI), conf.Realms[i].AllowAnonymous,
			conf.Realms[i].AllowDisclose)
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
