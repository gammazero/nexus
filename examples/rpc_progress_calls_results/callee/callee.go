package main

import (
	"context"
	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/examples/newclient"
	"github.com/gammazero/nexus/v3/wamp"
	"log"
	"os"
	"os/signal"
)

const procedureName = "mirror"

func main() {
	logger := log.New(os.Stdout, "CALLEE> ", 0)
	// Connect callee client with requested socket type and serialization.
	callee, err := newclient.NewClient(logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer callee.Close()

	// Important node: ctx context.Context here is the context of the whole invocation
	// starting with 1st call/invocation and lasts until final result
	mirror := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		log.Println("Received and sending back payload data...", inv.Arguments[0])
		if isInProgress, _ := inv.Details[wamp.OptProgress].(bool); !isInProgress {
			return client.InvokeResult{Args: inv.Arguments}
		} else {
			senderr := callee.SendProgress(ctx, inv.Arguments, nil)
			if senderr != nil {
				log.Println("Error sending progress:", senderr)
				return client.InvokeResult{Err: "test.failed"}
			}
		}

		return client.InvokeResult{Err: wamp.InternalProgressiveOmitResult}
	}

	// Register procedure "sum"
	if err = callee.Register(procedureName, mirror, nil); err != nil {
		logger.Fatal("Failed to register procedure:", err)
	}
	logger.Println("Registered procedure", procedureName, "with router")

	// Wait for CTRL-c or client close while handling remote procedure calls.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
	case <-callee.Done():
		logger.Print("Router gone, exiting")
		return // router gone, just exit
	}

	if err = callee.Unregister(procedureName); err != nil {
		logger.Println("Failed to unregister procedure:", err)
	}
}
