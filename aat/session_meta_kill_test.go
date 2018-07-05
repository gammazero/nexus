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

	reason := wamp.URI("test.session.kill")
	message := "this is a test"

	// Call meta procedure to kill a client-3
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
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
	// Check for expected GOODBYE message.
	goodbye := cli3.RouterGoodbye()
	if goodbye == nil {
		t.Error("Did not receive goodbye from router")
	}
	if goodbye.Reason != reason {
		t.Error("Did not get expected GOODBYE.Reason, got:", goodbye.Reason)
	}
	if gm, ok := wamp.AsString(goodbye.Details["message"]); ok {
		if gm != message {
			t.Error("Did not get expected goodbye message, got:", gm)
		}
	} else {
		t.Error("Expected message in GOODBYE")
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

	// Test that killing a sesson that does not exist works correctly.
	ctx, c2 := context.WithTimeout(context.Background(), time.Second)
	defer c2()
	args = wamp.List{wamp.ID(12345)}
	kwArgs = wamp.Dict{"reason": reason, "message": message}
	result, err = cli1.Call(ctx, metaKill, nil, args, kwArgs, "")
	if err == nil {
		t.Error("Expected error")
	} else if _, ok := err.(client.RPCError); !ok {
		t.Fatal("Expected RPCError")
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

func TestSessionKillAll(t *testing.T) {
	defer leaktest.Check(t)()

	cli1, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer cli1.Close()

	cli2, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer cli2.Close()

	cli3, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer cli3.Close()

	reason := wamp.URI("test.session.kill")
	message := "this is a test"

	// Call meta procedure to kill a client-3
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	kwArgs := wamp.Dict{"reason": reason, "message": message}
	result, err := cli1.Call(ctx, metaKillAll, nil, nil, kwArgs, "")
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
	// Check for expected GOODBYE message.
	goodbye := cli3.RouterGoodbye()
	if goodbye == nil {
		t.Error("Did not receive goodbye from router")
	}
	if goodbye.Reason != reason {
		t.Error("Did not get expected GOODBYE.Reason, got:", goodbye.Reason)
	}
	gm, ok := wamp.AsString(goodbye.Details["message"])
	if !ok {
		t.Error("Expected message in GOODBYE")
	} else if gm != message {
		t.Error("Did not get expected goodbye message, got:", gm)
	}

	// Check that client-2 was booted.
	select {
	case <-cli2.Done():
	case <-time.After(time.Second):
		t.Fatal("Client 2 did not shutdown")
	}

	// Test that client-1 still connected.
	select {
	case <-cli1.Done():
		t.Fatal("Client 1 should still be connected")
	default:
	}

	cli4, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer cli4.Close()

	// Call killall again, with no reason.
	ctx, c2 := context.WithTimeout(context.Background(), time.Second)
	defer c2()
	result, err = cli1.Call(ctx, metaKillAll, nil, nil, nil, "")
	if err != nil {
		t.Fatal("Call error:", err)
	}
	if result == nil {
		t.Error("Did not receive result")
	}

	// Check that client-4 was booted.
	select {
	case <-cli4.Done():
	case <-time.After(time.Second):
		t.Fatal("Client 4 did not shutdown")
	}

	// Check for expected GOODBYE message.
	goodbye = cli4.RouterGoodbye()
	if goodbye == nil {
		t.Error("Did not receive goodbye from router")
	}
	if goodbye.Reason != wamp.CloseNormal {
		t.Error("Did not get expected GOODBYE.Reason, got:", goodbye.Reason)
	}
	if _, ok = goodbye.Details["message"]; ok {
		t.Error("Should not have received message in GOODBYE.Details")
	}
}

func TestSessionModifyDetails(t *testing.T) {
	defer leaktest.Check(t)()

	cli, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	defer cli.Close()

	// Call meta procedure to modify details.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	delta := wamp.Dict{"pi": 3.14, "authid": "bob"}
	args := wamp.List{cli.ID(), delta}
	result, err := cli.Call(ctx, metaModifyDetails, nil, args, nil, "")
	if err != nil {
		t.Fatal("Call error:", err)
	}
	if result == nil {
		t.Error("Did not receive result")
	}

	// Call session meta-procedure to get session information.
	ctx = context.Background()
	args = wamp.List{cli.ID()}
	result, err = cli.Call(ctx, metaGet, nil, args, nil, "")
	if err != nil {
		t.Fatal("Call error:", err)
	}
	if len(result.Arguments) == 0 {
		t.Fatal("Missing result argument")
	}
	details, ok := wamp.AsDict(result.Arguments[0])
	if !ok {
		t.Fatal("Could not convert result to wamp.Dict")
	}
	pi, _ := wamp.AsFloat64(details["pi"])
	if pi != 3.14 {
		t.Fatal("Wrong value for detail pi, got", details["pi"])
	}
	authid, _ := wamp.AsString(details["authid"])
	if authid != "bob" {
		t.Fatal("Wrong value for detail authid")
	}
}
