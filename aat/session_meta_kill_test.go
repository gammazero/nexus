package aat

import (
	"context"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const (
	metaKill           = string(wamp.MetaProcSessionKill)
	metaKillByAuthid   = string(wamp.MetaProcSessionKillByAuthid)
	metaKillByAuthrole = string(wamp.MetaProcSessionKillByAuthrole)
	metaKillAll        = string(wamp.MetaProcSessionKillAll)

	metaModifyDetails = string(wamp.MetaProcSessionModifyDetails)
)

func TestSessionKill(t *testing.T) {
	defer leaktest.Check(t)()

	cli1, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	cli2, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	cli3, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	reason := wamp.URI("foo.bar.baz")
	message := "this is a test"

	// Call meta procedure to kill a client-3
	ctx := context.Background()
	args := wamp.List{cli3.ID()}
	kwArgs := wamp.Dict{"reason": reason, "message": message}
	result, err := cli1.Call(ctx, metaKill, nil, args, kwArgs, "")
	if err != nil {
		t.Fatal("Call error:", err)
	}
	if result == nil {
		t.Error("Did not receive result")
	}

	// Check that client-3 was booted.
	select {
	case <-cli3.Done():
	case <-time.After(time.Second):
		t.Fatal("Client 3 did not shutdown")
	}

	// Check that client-2 is still connected.
	select {
	case <-cli2.Done():
		t.Fatal("Client 2 should still be connected")
	default:
	}

	// Test that trying to kill self receives error.
	args = wamp.List{cli1.ID}
	result, err = cli1.Call(ctx, metaKill, nil, args, kwArgs, "")
	if err == nil {
		t.Fatal("Expected error")
	}
	rpcErr, ok := err.(client.RPCError)
	if !ok {
		t.Fatal("Expected RPCError")
	}
	if rpcErr.Err.Error != wamp.ErrNoSuchSession {
		t.Error("Wrong error, got", rpcErr.Err.Error, "expected", wamp.ErrNoSuchSession)
	}

	// Make sure everything closes correctly.
	if err = cli3.Close(); err != nil {
		t.Error(err)
	}
	if err = cli2.Close(); err != nil {
		t.Error(err)
	}
	if err = cli1.Close(); err != nil {
		t.Error(err)
	}
}
