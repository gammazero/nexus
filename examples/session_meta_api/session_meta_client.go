package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/examples/newclient"
	"github.com/gammazero/nexus/v3/wamp"
)

const (
	metaOnJoin  = string(wamp.MetaEventSessionOnJoin)
	metaOnLeave = string(wamp.MetaEventSessionOnLeave)

	metaCount = string(wamp.MetaProcSessionCount)
	metaList  = string(wamp.MetaProcSessionList)
	metaGet   = string(wamp.MetaProcSessionGet)
)

func subscribeMetaOnJoin(subscriber *client.Client, logger *log.Logger) {
	// Define function to handle on_join events received.
	onJoin := func(event *wamp.Event) {
		if len(event.Arguments) != 0 {
			if details, ok := wamp.AsDict(event.Arguments[0]); ok {
				onJoinID, _ := wamp.AsID(details["session"])
				authid, _ := wamp.AsString(details["authid"])
				logger.Printf("Client %v joined realm (authid=%s)\n", onJoinID, authid)
				return
			}
		}
		logger.Println("Client joined realm - no data provided")
	}

	// Subscribe to on_join topic.
	err := subscriber.Subscribe(metaOnJoin, onJoin, nil)
	if err != nil {
		logger.Fatal("subscribe error:", err)
	}
	logger.Println("Subscribed to", metaOnJoin)
}

func subscribeMetaOnLeave(subscriber *client.Client, logger *log.Logger) {
	// Define function to handle on_leave events received.
	onLeave := func(event *wamp.Event) {
		if len(event.Arguments) != 0 {
			if id, ok := wamp.AsID(event.Arguments[0]); ok {
				logger.Println("Client", id, "left realm")
				return
			}
		}
		logger.Println("A client left the realm")
	}

	// Subscribe to on_leave topic.
	err := subscriber.Subscribe(metaOnLeave, onLeave, nil)
	if err != nil {
		logger.Fatal("subscribe error:", err)
	}
	logger.Println("Subscribed to", metaOnLeave)
}

func getSessionCount(caller *client.Client, logger *log.Logger) {
	// Call meta procedure to get session count.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := caller.Call(ctx, metaCount, nil, nil, nil, nil)
	if err != nil {
		logger.Println("Call error:", err)
		return
	}
	count, _ := wamp.AsInt64(result.Arguments[0])
	logger.Println("Current session count:", count)
}

func main() {
	logger := log.New(os.Stdout, "METACLIENT> ", 0)
	// Connect subscriber client with requested socket type and serialization.
	cli, err := newclient.NewClient(logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer cli.Close()

	// Check for feature support in router.
	const featureSessionMetaAPI = "session_meta_api"
	if !cli.HasFeature("broker", featureSessionMetaAPI) {
		logger.Fatal("Broker does not have", featureSessionMetaAPI, "feature")
	}
	if !cli.HasFeature("dealer", featureSessionMetaAPI) {
		logger.Fatal("Dealer does not have", featureSessionMetaAPI, "feature")
	}

	// Subscribe to session meta events.
	subscribeMetaOnJoin(cli, logger)
	subscribeMetaOnLeave(cli, logger)

	// Get current sessions from session meta API.
	getSessionCount(cli, logger)

	// Wait for CTRL-c or client close while handling events.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
	case <-cli.Done():
		logger.Print("Router gone, exiting")
		return // router gone, just exit
	}

	// Unsubscribe from topic.
	if err = cli.Unsubscribe(metaOnJoin); err != nil {
		logger.Println("Failed to unsubscribe:", err)
	}
	if err = cli.Unsubscribe(metaOnLeave); err != nil {
		logger.Println("Failed to unsubscribe:", err)
	}
}
