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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Create and run servers.
	var wsCloser, tcpCloser, unixCloser io.Closer
	if conf.WebSocket.Address != "" {
		// Create a new websocket server with the router.
		wss := nexus.NewWebsocketServer(r)
		if conf.WebSocket.CertFile == "" || conf.WebSocket.KeyFile == "" {
			wsCloser, err = wss.ListenAndServe(conf.WebSocket.Address)
		} else {
			// Config has cert_file and key_file, so do TLS.
			wsCloser, err = wss.ListenAndServeTLS(conf.WebSocket.Address, nil,
				conf.WebSocket.CertFile, conf.WebSocket.KeyFile)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("Listening for websocket connections on ws://%s/\n",
			conf.WebSocket.Address)
	}
	if conf.RawSocket.TCPAddress != "" || conf.RawSocket.UnixAddress != "" {
		// Create a new rawsocket server with the router.
		rss := nexus.NewRawSocketServer(r, conf.RawSocket.MaxMsgLen,
			conf.RawSocket.TCPKeepAlive)
		if conf.RawSocket.TCPAddress != "" {
			// Run rawsocket TCP server.
			tcpCloser, err = rss.ListenAndServe("tcp", conf.RawSocket.TCPAddress)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println("Listening for rawsocket connections on",
				conf.RawSocket.TCPAddress)
		}
		if conf.RawSocket.UnixAddress != "" {
			// Run rawsocket Unix server.
			tcpCloser, err = rss.ListenAndServe("unix", conf.RawSocket.UnixAddress)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println("Listening for rawsocket connections on",
				conf.RawSocket.UnixAddress)
		}
	}
	if wsCloser == nil && tcpCloser == nil && unixCloser == nil {
		fmt.Fprintln(os.Stderr, "No servers configured")
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
			fmt.Fprintln(os.Stderr, "Router took too long to stop")
			os.Exit(1)
		case <-exitChan:
		}
	}()

	fmt.Println("Shutting down router...")
	if wsCloser != nil {
		wsCloser.Close()
	}
	if tcpCloser != nil {
		tcpCloser.Close()
	}
	if unixCloser != nil {
		unixCloser.Close()
	}

	r.Close()
	close(exitChan)
}
