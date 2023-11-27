package main

import (
	"context"
	"log"
	"os"
	"sync"

	"github.com/dtegapp/nexus/v3/examples/newclient"
	"github.com/dtegapp/nexus/v3/wamp"
)

func main() {
	logger := log.New(os.Stderr, "CALLER> ", 0)

	// Connect caller client with requested socket type and serialization.
	caller, err := newclient.NewClient(logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer caller.Close()

	// Test calling the "mirror" procedure with args 1..10.  Requires
	// external rpc client to be running.
	callArgs := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ctx := context.Background()

	var progressiveResults []int64
	var mu sync.Mutex

	progRescb := func(result *wamp.Result) {
		mu.Lock()
		defer mu.Unlock()
		n, _ := wamp.AsInt64(result.Arguments[0])
		progressiveResults = append(progressiveResults, n)
	}

	callSends := 0
	sendProgDataCb := func(ctx context.Context) (options wamp.Dict, args wamp.List, kwargs wamp.Dict, err error) {
		options = wamp.Dict{}

		if callSends == (len(callArgs) - 1) {
			options[wamp.OptProgress] = false
		} else {
			options[wamp.OptProgress] = true
		}

		args = wamp.List{callArgs[callSends]}
		callSends++

		return options, args, nil, nil
	}

	logger.Println("Progressively calls remote procedure to get numbers back")
	result, err := caller.CallProgressive(ctx, "mirror", sendProgDataCb, progRescb)
	if err != nil {
		logger.Fatal(err)
	}

	n, _ := wamp.AsInt64(result.Arguments[0])
	progressiveResults = append(progressiveResults, n)
	var sum int64
	for _, arg := range progressiveResults {
		sum += arg
	}
	logger.Println("Sum of sent/received numbers is:", sum)
}
