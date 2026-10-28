package main

import (
	"context"
	"errors"
	"fmt"
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
)

func subscribeMetaOnJoin(subscriber *client.Client, logger *log.Logger) error {
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
	if err := subscriber.Subscribe(metaOnJoin, onJoin, nil); err != nil {
		return fmt.Errorf("subscribe error: %w", err)
	}
	logger.Println("Subscribed to", metaOnJoin)
	return nil
}

func subscribeMetaOnLeave(subscriber *client.Client, logger *log.Logger) error {
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
	if err := subscriber.Subscribe(metaOnLeave, onLeave, nil); err != nil {
		return fmt.Errorf("subscribe error: %w", err)
	}
	logger.Println("Subscribed to", metaOnLeave)
	return nil
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
	if len(result.Arguments) == 0 {
		logger.Println("Call error: result has no arguments")
		return
	}
	count, _ := wamp.AsInt64(result.Arguments[0])
	logger.Println("Current session count:", count)
}

func main() {
	logger := log.New(os.Stdout, "METACLIENT> ", 0)
	if err := run(logger); err != nil {
		logger.Fatal(err)
	}
}

func run(logger *log.Logger) error {
	// Connect subscriber client with requested socket type and serialization.
	cli, err := newclient.NewClient(logger)
	if err != nil {
		return err
	}
	defer cli.Close()

	// Check for feature support in router.
	if !cli.HasFeature(wamp.RoleBroker, wamp.FeatureSessionMetaAPI) {
		return errors.New("broker does not have " + wamp.FeatureSessionMetaAPI + " feature")
	}
	if !cli.HasFeature(wamp.RoleDealer, wamp.FeatureSessionMetaAPI) {
		return errors.New("dealer does not have " + wamp.FeatureSessionMetaAPI + " feature")
	}

	// Subscribe to session meta events.
	if err = subscribeMetaOnJoin(cli, logger); err != nil {
		return err
	}
	if err = subscribeMetaOnLeave(cli, logger); err != nil {
		return err
	}

	// Get current sessions from session meta API.
	getSessionCount(cli, logger)

	// Wait for CTRL-c or client close while handling events.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
	case <-cli.Done():
		logger.Print("Router gone, exiting")
		return nil // router gone, just exit
	}

	// Unsubscribe from topic.
	if err = cli.Unsubscribe(metaOnJoin); err != nil {
		logger.Println("Failed to unsubscribe:", err)
	}
	if err = cli.Unsubscribe(metaOnLeave); err != nil {
		logger.Println("Failed to unsubscribe:", err)
	}
	return nil
}
