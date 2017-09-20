/*
Stand-alone nexus router service.

*/
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
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
		logger.Print(err)
		os.Exit(1)
	}

	// Create and run servers.
	var closers []io.Closer
	if conf.WebSocket.Address != "" {
		// Create a new websocket server with the router.
		wss := nexus.NewWebsocketServer(r)
		var closer io.Closer
		if conf.WebSocket.CertFile == "" || conf.WebSocket.KeyFile == "" {
			closer, err = wss.ListenAndServe(conf.WebSocket.Address)
		} else {
			// Config has cert_file and key_file, so do TLS.
			closer, err = wss.ListenAndServeTLS(conf.WebSocket.Address, nil,
				conf.WebSocket.CertFile, conf.WebSocket.KeyFile)
		}
		if err != nil {
			logger.Print(err)
			os.Exit(1)
		}
		closers = append(closers, closer)
		logger.Printf("Listening for websocket connections on ws://%s/", conf.WebSocket.Address)
	}
	if conf.RawSocket.TCPAddress != "" || conf.RawSocket.UnixAddress != "" {
		var keepAliveInterval time.Duration
		if conf.RawSocket.TCPKeepAliveInterval != nil {
			keepAliveInterval = *conf.RawSocket.TCPKeepAliveInterval
		}
		// Create a new rawsocket server with the router.
		rss := nexus.NewRawSocketServer(r, conf.RawSocket.MaxMsgLen,
			keepAliveInterval)
		if conf.RawSocket.TCPAddress != "" {
			// Run rawsocket TCP server.
			closer, err := rss.ListenAndServe("tcp", conf.RawSocket.TCPAddress)
			if err != nil {
				logger.Print(err)
				os.Exit(1)
			}
			closers = append(closers, closer)
			logger.Println("Listening for TCP socket connections on",
				conf.RawSocket.TCPAddress)
		}
		if conf.RawSocket.UnixAddress != "" {
			// Run rawsocket Unix server.
			closer, err := rss.ListenAndServe("unix", conf.RawSocket.UnixAddress)
			if err != nil {
				logger.Print(err)
				os.Exit(1)
			}
			closers = append(closers, closer)
			logger.Println("Listening for Unix socket connections on",
				conf.RawSocket.UnixAddress)
		}
	}
	if len(closers) == 0 {
		logger.Print("No servers configured")
		os.Exit(1)
	}

	// Shutdown server if SIGINT (CTRL-c) received.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	<-shutdown

	// If process does not exit in a few seconds, exit with error.
	exitChan := make(chan struct{})
	go func() {
		select {
		case <-time.After(5 * time.Second):
			logger.Print("Router took too long to stop")
			os.Exit(1)
		case <-exitChan:
		}
	}()

	logger.Print("Shutting down router...")
	for i := range closers {
		closers[i].Close()
	}
	r.Close()
	close(exitChan)
}
