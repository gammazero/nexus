/*
Progressive Call Results Example Callee

This example demonstrates a callee client that sends data in chunks, as
separate progressive call results.  A callee may do this to send a large body
of data in managable chunks, or to deliver some portion of the result more
immediately.

In this example, the progressive results are portions of a larger body of text.
The final result is a sha256 sum of all the data, allowing the caller to verify
that it received everything correcly.

*/

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"log"
	"os"
	"os/signal"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/examples/newclient"
	"github.com/gammazero/nexus/v3/wamp"
)

const procedureName = "example.progress.text"

func main() {
	logger := log.New(os.Stdout, "CALLEE> ", 0)
	// Connect callee client with requested socket type and serialization.
	callee, err := newclient.NewClient(logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer callee.Close()

	// Handler is a closure used to capture the callee, since this is not
	// provided as a parameter to this callback.
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		return sendData(ctx, callee, inv.Arguments)
	}

	// Register procedure.
	if err = callee.Register(procedureName, handler, nil); err != nil {
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

// sendData sends the body of data in chunks of the requested size.  The final
// result message contains the sha256 hash of the data to allow the caller to
// verify that all the data was correctly received.
func sendData(ctx context.Context, callee *client.Client, args wamp.List) client.InvokeResult {
	// Compute the base64-encoded sha256 hash of the data.
	h := sha256.New()
	h.Write([]byte(gettysburg))
	hash64 := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// Put data in buffer to read chunks from.
	b := bytes.NewBuffer([]byte(gettysburg))

	// Get chunksize requested by caller, use default if not set.
	var chunkSize int
	if len(args) != 0 {
		i, _ := wamp.AsInt64(args[0])
		chunkSize = int(i)
	}
	if chunkSize == 0 {
		chunkSize = 64
	}

	// Read and send chunks of data until the buffer is empty.
	for chunk := b.Next(chunkSize); len(chunk) != 0; chunk = b.Next(chunkSize) {
		// Send a chunk of data.
		err := callee.SendProgress(ctx, wamp.List{string(chunk)}, nil)
		if err != nil {
			// If send failed, return an error saying the call canceled.
			return client.InvokeResult{Err: wamp.ErrCanceled}
		}
	}

	// Send sha256 hash as final result.
	return client.InvokeResult{Args: wamp.List{hash64}}
}

// This is the body of data that is sent in chunks.
var gettysburg = `Four score and seven years ago our fathers brought forth on this continent, a new nation, conceived in Liberty, and dedicated to the proposition that all men are created equal.

Now we are engaged in a great civil war, testing whether that nation, or any nation so conceived and dedicated, can long endure. We are met on a great battle-field of that war. We have come to dedicate a portion of that field, as a final resting place for those who here gave their lives that that nation might live. It is altogether fitting and proper that we should do this.

But, in a larger sense, we can not dedicate -- we can not consecrate -- we can not hallow -- this ground. The brave men, living and dead, who struggled here, have consecrated it, far above our poor power to add or detract. The world will little note, nor long remember what we say here, but it can never forget what they did here. It is for us the living, rather, to be dedicated here to the unfinished work which they who fought here have thus far so nobly advanced. It is rather for us to be here dedicated to the great task remaining before us -- that from these honored dead we take increased devotion to that cause for which they gave the last full measure of devotion -- that we here highly resolve that these dead shall not have died in vain -- that this nation, under God, shall have a new birth of freedom -- and that government of the people, by the people, for the people, shall not perish from the earth.`
