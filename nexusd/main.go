/*
Stand-alone nexus router service.

*/
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
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

	var wss *nexus.WebsocketServer
	var rssTCP, rssUnix *nexus.RawSocketServer
	if conf.WebSocket.Address != "" {
		// Create a new websocket server with the router.
		wss, err = nexus.NewWebsocketServer(r, conf.WebSocket.Address)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("Listening for websocket connections on", wss.URL())
	}
	if conf.RawSocket.TCPAddress != "" {
		// Create a new websocket server with the router.
		rssTCP, err = nexus.NewRawSocketServer(
			r, "tcp", conf.RawSocket.TCPAddress, conf.RawSocket.MaxMsgLen)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("Listening for rawsocket connections on", rssTCP.Addr())
	}
	if conf.RawSocket.UnixAddress != "" {
		// Create a new websocket server with the router.
		rssUnix, err = nexus.NewRawSocketServer(
			r, "unix", conf.RawSocket.UnixAddress, conf.RawSocket.MaxMsgLen)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("Listening for rawsocket connections on", rssUnix.Addr())
	}
	if wss == nil && rssTCP == nil && rssUnix == nil {
		fmt.Fprintln(os.Stderr, "No servers configured")
		os.Exit(1)
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	var waitServers sync.WaitGroup

	if wss != nil {
		waitServers.Add(1)
		go func() {
			if conf.WebSocket.CertFile != "" && conf.WebSocket.KeyFile != "" {
				// Config has cert_file and key_file, so do TLS.
				wss.ServeTLS(nil, conf.WebSocket.CertFile,
					conf.WebSocket.KeyFile)
			} else {
				wss.Serve()
			}
			waitServers.Done()
		}()
	}
	if rssTCP != nil {
		waitServers.Add(1)
		go func() {
			rssTCP.Serve(conf.RawSocket.TCPKeepAlive)
			waitServers.Done()
		}()
	}
	if rssUnix != nil {
		waitServers.Add(1)
		go func() {
			rssUnix.Serve(false)
			waitServers.Done()
		}()
	}

	// Shutdown server is SIGINT received.
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
	if wss != nil {
		wss.Close()
	}
	if rssTCP != nil {
		rssTCP.Close()
	}
	if rssUnix != nil {
		rssUnix.Close()
	}

	waitServers.Wait()
	r.Close()
	close(exitChan)
	os.Exit(0)
}
