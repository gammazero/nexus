package main

import (
	"context"
	"fmt"
	"encoding/json"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/wamp"
	"io"
	"log"
	"os"
	"os/signal"
	"time"
)

const procedureName = "server.time"
const wwsPort = 8008

func main() {
	broker := newBroker(wwsPort)

	// Register procedure "sum"
	if err := broker.rpcclient.Register(procedureName, serverTime, nil); err != nil {
		log.Fatal("Failed to register procedure:", err)
	}
	log.Println("Registered procedure", procedureName, "with router")

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	<-shutdown

	if err := broker.rpcclient.Unregister(procedureName); err != nil {
		log.Println("Failed to unregister procedure:", err)
	}

	broker.close()
}

func serverTime(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {

	body, _ := json.Marshal(map[string]interface{}{"time":time.Now()})
	Args := wamp.List{"ok", string(body)}
	return &client.InvokeResult{Args: Args}
}

/* ---------------------------------------------------------------- */

type Broker struct {
	nxr       router.Router
	rpcclient *client.Client
	closer    io.Closer
}

func newBroker(wwsPort int) *Broker {

	routerConfig := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			&router.RealmConfig{
				URI:           wamp.URI("nexus.example"),
				AnonymousAuth: true,
				AllowDisclose: true,
			},
		},
	}

	nxr, err := router.NewRouter(routerConfig, nil)
	if err != nil {
		log.Fatal(err)
	}

	cfg := client.Config{
		Realm: "nexus.example",
	}

	wss := router.NewWebsocketServer(nxr)
	wss.Upgrader.EnableCompression = true
	wss.EnableTrackingCookie = true
	wss.KeepAlive = 30 * time.Second

	rpcclient, err := client.ConnectLocal(nxr, cfg)
	if err != nil {
		log.Fatal(err)
	}

	// Create and run websocket server.
	closer, err := wss.ListenAndServe(fmt.Sprintf(":%d", wwsPort))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Websocket server listening on ws://:%d/", wwsPort)

	broker := &Broker{
		nxr:       nxr,
		rpcclient: rpcclient,
		closer:    closer,
	}
	return broker

}

func (b *Broker) close() {
	b.closer.Close()
	b.rpcclient.Close()
	b.nxr.Close()
}
