package main

import (
	"context"
	"fmt"
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
	if err := run(logger); err != nil {
		logger.Fatal(err)
	}
}

func run(logger *log.Logger) error {
	// Connect callee client with requested socket type and serialization.
	callee, err := newclient.NewClient(logger)
	if err != nil {
		return err
	}
	defer callee.Close()

	// Register procedure "sum".
	s := &summer{payloads: map[wamp.ID][]int64{}}
	if err = callee.Register(procedureName, s.sum, nil); err != nil {
		return fmt.Errorf("failed to register procedure: %w", err)
	}
	logger.Println("Registered procedure", procedureName, "with router")

	// Wait for CTRL-c or client close while handling remote procedure calls.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	select {
	case <-sigChan:
	case <-callee.Done():
		logger.Print("Router gone, exiting")
		return nil // router gone, just exit.
	}

	if err = callee.Unregister(procedureName); err != nil {
		logger.Println("Failed to unregister procedure:", err)
	}
	return nil
}

// summer accumulates the numbers sent by each progressive call, until the
// final call of each asks for their sum.
type summer struct {
	mu sync.Mutex
	// INVOCATION request ID -> numbers received so far. Every invocation of a
	// progressive call has the request ID of its first invocation.
	payloads map[wamp.ID][]int64
}

// Important note: ctx context.Context here is the context of the whole
// invocation starting with 1st call/invocation and lasts until final result
func (s *summer) sum(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	var n int64
	if len(inv.Arguments) != 0 {
		n, _ = wamp.AsInt64(inv.Arguments[0])
	}

	s.mu.Lock()
	payload, ok := s.payloads[inv.Request]
	if !ok {
		// Drop the numbers if the call is canceled before its final call.
		reqID := inv.Request
		context.AfterFunc(ctx, func() {
			s.mu.Lock()
			delete(s.payloads, reqID)
			s.mu.Unlock()
		})
	}
	payload = append(payload, n)
	isInProgress, _ := inv.Details[wamp.OptProgress].(bool)
	if isInProgress {
		s.payloads[inv.Request] = payload
	} else {
		// Let's clean up data as call is finished.
		delete(s.payloads, inv.Request)
	}
	s.mu.Unlock()

	if !isInProgress {
		log.Println("Calculating sum and sending final result")
		var sum int64
		for _, n := range payload {
			sum += n
		}
		return client.InvokeResult{Args: wamp.List{sum}}
	}

	log.Println("Accumulating intermediate payload data...", n)

	return client.InvokeResult{Err: wamp.InternalProgressiveOmitResult}
}
