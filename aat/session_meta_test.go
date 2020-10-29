package aat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

const (
	metaOnJoin  = string(wamp.MetaEventSessionOnJoin)
	metaOnLeave = string(wamp.MetaEventSessionOnLeave)

	metaCount = string(wamp.MetaProcSessionCount)
	metaList  = string(wamp.MetaProcSessionList)
	metaGet   = string(wamp.MetaProcSessionGet)
)

func TestMetaEventOnJoin(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Check for feature support in router.
	if !subscriber.HasFeature(wamp.RoleBroker, wamp.FeatureSessionMetaAPI) {
		t.Error("Broker does not support", wamp.FeatureSessionMetaAPI)
	}
	if !subscriber.HasFeature(wamp.RoleDealer, wamp.FeatureSessionMetaAPI) {
		t.Error("Dealer does not support", wamp.FeatureSessionMetaAPI)
	}

	// Subscribe to event.
	onJoinEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnJoin, onJoinEvents, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Wait for any event from subscriber joining.
	var timeout bool
	for !timeout {
		select {
		case <-onJoinEvents:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	// Connect client to generate wamp.session.on_join event.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	var onJoinID wamp.ID
	onJoin := func(event *wamp.Event) error {
		args := event.Arguments
		if len(args) == 0 {
			return errors.New("missing argument")
		}
		details := wamp.NormalizeDict(args[0])
		if details == nil {
			return errors.New("argument was not wamp.Dict")
		}
		onJoinID, _ = wamp.AsID(details["session"])
		return nil
	}

	// Make sure the event was received.
	select {
	case event := <-onJoinEvents:
		if err = onJoin(event); err != nil {
			t.Fatalf("Event error: %s", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get published event")
	}

	if onJoinID != sess.ID() {
		t.Fatal(metaOnJoin, "meta even had wrong session ID")
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestMetaEventOnLeave(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	argsChan := make(chan wamp.List)
	eventHandler := func(event *wamp.Event) {
		argsChan <- event.Arguments
	}

	// Subscribe to event.
	err = subscriber.Subscribe(metaOnLeave, eventHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Connect a session.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Wait for any events from previously closed clients.
	var timeout bool
	for !timeout {
		select {
		case <-argsChan:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	sid := sess.ID()

	// Disconnect client to generate wamp.session.on_leave event.
	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	// Make sure the event was received.
	var eventArgs wamp.List
	select {
	case eventArgs = <-argsChan:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get published event")
	}

	// Check that all expected arguments are returned in on_leave event.
	if len(eventArgs) < 3 {
		t.Fatal("expected 3 event args, got:", len(eventArgs))
	}
	onLeaveID, _ := wamp.AsID(eventArgs[0])
	if onLeaveID != sid {
		t.Fatal(metaOnLeave, "meta event had wrong session ID, got", onLeaveID,
			"want", sid)
	}
	authid, _ := wamp.AsString(eventArgs[1])
	if len(authid) == 0 {
		t.Error("expected non-empty authid")
	}
	authrole, _ := wamp.AsString(eventArgs[2])
	if authrole != "trusted" && authrole != "anonymous" {
		t.Error("expected authrole of trusted or anonymous, got:", authrole)
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestMetaProcSessionCount(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect caller.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Subscribe to on_join and on_leave events.
	onJoinEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnJoin, onJoinEvents, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}
	onLeaveEvents := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnLeave, onLeaveEvents, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Wait for any events from previously closed clients.
	var timeout bool
	for !timeout {
		select {
		case <-onLeaveEvents:
		case <-onJoinEvents:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	// Call meta procedure to get session count.
	ctx := context.Background()
	result, err := caller.Call(ctx, metaCount, nil, nil, nil, nil)
	if err != nil {
		t.Fatal("Call error:", err)
	}
	firstCount, ok := wamp.AsInt64(result.Arguments[0])
	if !ok {
		t.Fatal("Could not convert result to int64")
	}
	if firstCount == 0 {
		t.Fatal("Session count should not be zero")
	}

	// Connect client to increment session count.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}
	// Wait for router to register new session.
	select {
	case <-onJoinEvents:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for router to register new session")
	}

	// Call meta procedure to get session count.
	result, err = caller.Call(ctx, metaCount, nil, nil, nil, nil)
	if err != nil {
		t.Fatal("Call error:", err)
	}
	count, ok := wamp.AsInt64(result.Arguments[0])
	if !ok {
		t.Fatal("Could not convert result to int64")
	}
	if count != firstCount+1 {
		t.Fatal("Session count should one more the previous")
	}

	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
	// Wait for router to register client leaving.
	select {
	case <-onLeaveEvents:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for router to register client leaving")
	}

	// Call meta procedure to get session count.
	result, err = caller.Call(ctx, metaCount, nil, nil, nil, nil)
	if err != nil {
		t.Fatal("Call error:", err)
	}
	count, ok = wamp.AsInt64(result.Arguments[0])
	if !ok {
		t.Fatal("Could not convert result to int64")
	}
	if count != firstCount {
		t.Fatal("Session count should be same as first")
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestMetaProcSessionList(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect a client to session.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Connect caller.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Subscribe to on_leave event.
	eventChan := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnLeave, eventChan, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Wait for any events from previously closed clients.
	var timeout bool
	for !timeout {
		select {
		case <-eventChan:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	// Call meta procedure to get session list.
	ctx := context.Background()
	result, err := caller.Call(ctx, metaList, nil, nil, nil, nil)
	if err != nil {
		t.Fatal("Call error:", err)
	}
	if len(result.Arguments) == 0 {
		t.Fatal("Missing result argument")
	}
	list, ok := wamp.AsList(result.Arguments[0])
	if !ok {
		t.Fatal("Could not convert result to wamp.List")
	}
	if len(list) == 0 {
		t.Fatal("Session list should not be empty")
	}
	var found bool
	for i := range list {
		id, _ := wamp.AsID(list[i])
		if id == sess.ID() {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Missing session ID from session list")
	}
	firstLen := len(list)

	sid := sess.ID()

	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
	// Wait for router to register client leaving.
	<-eventChan

	// Call meta procedure to get session list.
	result, err = caller.Call(ctx, metaList, nil, nil, nil, nil)
	if err != nil {
		t.Fatal("Call error:", err)
	}
	if len(result.Arguments) == 0 {
		t.Fatal("Missing result argument")
	}
	list, ok = wamp.AsList(result.Arguments[0])
	if !ok {
		t.Fatal("Could not convert result to wamp.List")
	}
	if len(list) != firstLen-1 {
		t.Fatal("Session list should be one less than previous")
	}
	found = false
	for i := range list {
		id, _ := wamp.AsID(list[i])
		if id == sid {
			found = true
			break
		}
	}
	if found {
		t.Fatal("Session ID should not be in session list")
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestMetaProcSessionGet(t *testing.T) {
	defer leaktest.Check(t)()
	// Connect a client to session.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Connect caller.
	caller, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Subscribe to on_leave event.
	eventChan := make(chan *wamp.Event)
	err = subscriber.SubscribeChan(metaOnLeave, eventChan, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Call meta procedure to get session info.
	ctx := context.Background()
	args := wamp.List{sess.ID()}
	result, err := caller.Call(ctx, metaGet, nil, args, nil, nil)
	if err != nil {
		t.Fatal("Call error:", err)
	}
	if len(result.Arguments) == 0 {
		t.Fatal("Missing result argument")
	}
	dict, ok := wamp.AsDict(result.Arguments[0])
	if !ok {
		t.Fatal("Could not convert result to wamp.Dict")
	}
	resultID, _ := wamp.AsID(dict["session"])
	if resultID != sess.ID() {
		t.Fatal("Wrong session ID in result")
	}
	for _, attr := range []string{"authid", "authrole", "authmethod", "authprovider"} {
		if _, ok = dict[attr]; !ok {
			t.Fatal("Result missing", attr, "DICT:", dict)
		}
	}

	// Wait for any events from previously closed clients.
	var timeout bool
	for !timeout {
		select {
		case <-eventChan:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	sid := sess.ID()

	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
	// Wait for router to register client leaving.
	<-eventChan

	// Call meta procedure to get session list.
	result, err = caller.Call(ctx, metaGet, nil, wamp.List{sid}, nil, nil)
	if err == nil {
		t.Fatal("Expected error")
	}
	rpcErr := err.(client.RPCError)
	if rpcErr.Err.Error != wamp.ErrNoSuchSession {
		t.Fatal("Expected error URI:", wamp.ErrNoSuchSession, "got", rpcErr.Err.Error)
	}

	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}
