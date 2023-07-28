package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/examples/newclient"
	"github.com/gammazero/nexus/v3/wamp"
)

const procedureName = "sum"

func main() {
	logger := log.New(os.Stdout, "CALLEE> ", 0)
	// Connect callee client with requested socket type and serialization.
	callee, err := newclient.NewClient(logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer callee.Close()

	// Register procedure "sum"
	if err = callee.Register(procedureName, sum, nil); err != nil {
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

var progressiveIncPayload []int64
var mu sync.Mutex

// Important node: ctx context.Context here is the context of the whole invocation
// starting with 1st call/invocation and lasts until final result
func sum(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	mu.Lock()
	n, _ := wamp.AsInt64(inv.Arguments[0])
	progressiveIncPayload = append(progressiveIncPayload, n)
	mu.Unlock()

	if isInProgress, _ := inv.Details[wamp.OptProgress].(bool); !isInProgress {
		log.Println("Calculating sum and sending final result")
		var sum int64
		for _, arg := range progressiveIncPayload {
			n, ok := wamp.AsInt64(arg)
			if ok {
				sum += n
			}
		}
		// Let's clean up data as call is finished
		progressiveIncPayload = nil
		return client.InvokeResult{Args: wamp.List{sum}}
	}

	log.Println("Accumulating intermediate payload data...", inv.Arguments[0])

	return client.InvokeResult{Err: wamp.InternalProgressiveOmitResult}
}
