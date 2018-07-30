/*
Progressive Call Results Example Caller

This example demonstrates a caller client that receives data in chunks, as
separate progressive call results.  A caller may do this to receive a large
body of data in managable chunks, or to deliver some portion of the result more
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
	"strings"

	"github.com/gammazero/nexus/examples/newclient"
	"github.com/gammazero/nexus/wamp"
)

const (
	// Example procedure name.
	procedureName = "example.progress.text"
	// Chunk size example caller requests.
	chunkSize = int64(64)
)

func main() {
	logger := log.New(os.Stderr, "CALLER> ", 0)

	// Connect caller client with requested socket type and serialization.
	caller, err := newclient.NewClient(logger)
	if err != nil {
		logger.Fatal(err)
	}
	defer caller.Close()

	// The progress handler accumulates the chunks of data as they arrive.  It
	// also progressively calculates a sha256 hash of the data as it arrives.
	var chunks []string
	h := sha256.New()
	progHandler := func(result *wamp.Result) {
		// Received another chunk of data, computing hash as chunks received.
		chunk := result.Arguments[0].(string)
		logger.Println("Received", len(chunk), "bytes (as progressive result)")
		chunks = append(chunks, chunk)
		h.Write([]byte(chunk))
	}

	ctx := context.Background()

	// Call the example procedure, specifying the size of chunks to send as
	// progressive results.
	result, err := caller.CallProgress(
		ctx, procedureName, nil, wamp.List{chunkSize}, nil, "", progHandler)
	if err != nil {
		logger.Println("Failed to call procedure:", err)
		return
	}

	// As a final result, the callee returns the base64 encoded sha256 hash of
	// the data.  This is decoded and compared to the value that the caller
	// calculated.  If they match, then the caller recieved the data correctly.
	hashB64 := result.Arguments[0].(string)
	calleeHash, err := base64.StdEncoding.DecodeString(hashB64)
	if err != nil {
		logger.Println("decode error:", err)
		return
	}

	// Check if rceived hash matches the hash computed over the received data.
	if !bytes.Equal(calleeHash, h.Sum(nil)) {
		logger.Println("Hash of received data does not match")
		return
	}
	logger.Println("Correctly received all data:")
	logger.Println("----------------------------")
	logger.Println(strings.Join(chunks, ""))
}
