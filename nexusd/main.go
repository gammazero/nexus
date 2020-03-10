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
	"strings"
	"time"

	"github.com/gammazero/nexus/v3/router"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-c nexus.json]\n", os.Args[0])
}

func main() {
	var cfgFile string
	var showVersion bool
	fs := flag.NewFlagSet("nexus", flag.ExitOnError)
	fs.StringVar(&cfgFile, "c", "etc/nexus.json", "Path to config file")
	fs.BoolVar(&showVersion, "version", false, "print version")
	fs.Usage = usage
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}
	if showVersion {
		fmt.Println("version", router.Version)
		os.Exit(0)
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
	r, err := router.NewRouter(&conf.Router, logger)
	if err != nil {
		logger.Print(err)
		os.Exit(1)
	}

	// Create and run servers.
	var closers []io.Closer
	if conf.WebSocket.Address != "" {
		// Create a new websocket server with the router.
		wss := router.NewWebsocketServer(r)
		if conf.WebSocket.EnableCompression {
			wss.Upgrader.EnableCompression = true
			logger.Printf("Compression enabled")
		}
		if conf.WebSocket.EnableTrackingCookie {
			wss.EnableTrackingCookie = true
			logger.Printf("Tracking cookie enabled - not currently used")
		}
		if conf.WebSocket.EnableRequestCapture {
			wss.EnableRequestCapture = true
			logger.Printf("Request capture enabled - not currently used")
		}
		if conf.WebSocket.KeepAlive != 0 {
			wss.KeepAlive = conf.WebSocket.KeepAlive
			logger.Printf("Websocket heartbeat interval: %s", wss.KeepAlive)
		}
		if len(conf.WebSocket.AllowOrigins) != 0 {
			e := wss.AllowOrigins(conf.WebSocket.AllowOrigins)
			if e != nil {
				logger.Print(e)
				os.Exit(1)
			}
			logger.Println("Allowing origins matching:",
				strings.Join(conf.WebSocket.AllowOrigins, "|"))
		}
		if conf.WebSocket.OutQueueSize != 0 {
			wss.OutQueueSize = conf.WebSocket.OutQueueSize
			logger.Printf("Websocket outbound queue size: %d", wss.OutQueueSize)
		}
		var closer io.Closer
		var sockDesc string
		if conf.WebSocket.CertFile != "" && conf.WebSocket.KeyFile != "" {
			// Config has cert_file and key_file, so do TLS.
			closer, err = wss.ListenAndServeTLS(conf.WebSocket.Address, nil,
				conf.WebSocket.CertFile, conf.WebSocket.KeyFile)
			sockDesc = "TLS websocket"
		} else {
			closer, err = wss.ListenAndServe(conf.WebSocket.Address)
			sockDesc = "websocket"
		}
		if err != nil {
			logger.Print(err)
			os.Exit(1)
		}
		closers = append(closers, closer)
		logger.Printf("Listening for %s connections on ws://%s/", sockDesc,
			conf.WebSocket.Address)
	}
	if conf.RawSocket.TCPAddress != "" || conf.RawSocket.UnixAddress != "" {
		// Create a new rawsocket server with the router.
		rss := router.NewRawSocketServer(r)
		rss.RecvLimit = conf.RawSocket.MaxMsgLen
		if conf.RawSocket.OutQueueSize != 0 {
			rss.OutQueueSize = conf.RawSocket.OutQueueSize
			logger.Printf("raw socket outbound queue size: %d", rss.OutQueueSize)
		}
		if conf.RawSocket.TCPAddress != "" {
			if conf.RawSocket.TCPKeepAliveInterval != 0 {
				rss.KeepAlive = conf.RawSocket.TCPKeepAliveInterval
				logger.Printf("tcp keep-alive interval: %s", rss.KeepAlive)
			}

			var closer io.Closer
			var sockDesc string
			if conf.RawSocket.CertFile != "" && conf.RawSocket.KeyFile != "" {
				// Run TLS rawsocket TCP server.
				closer, err = rss.ListenAndServeTLS("tcp",
					conf.RawSocket.TCPAddress, nil, conf.RawSocket.CertFile,
					conf.RawSocket.KeyFile)
				sockDesc = "TLS socket"
			} else {
				// Run rawsocket TCP server.
				closer, err = rss.ListenAndServe("tcp", conf.RawSocket.TCPAddress)
				sockDesc = "socket"
			}
			if err != nil {
				logger.Print(err)
				os.Exit(1)
			}
			closers = append(closers, closer)
			logger.Println("Listening for TCP", sockDesc, "connections on",
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
