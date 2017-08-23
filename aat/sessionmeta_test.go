package aat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const (
	metaOnJoin  = string(wamp.MetaEventSessionOnJoin)
	metaOnLeave = string(wamp.MetaEventSessionOnLeave)

	metaCount = string(wamp.MetaProcSessionCount)
	metaList  = string(wamp.MetaProcSessionList)
	metaGet   = string(wamp.MetaProcSessionGet)
)

func TestMetaEventOnJoin(t *testing.T) {
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	var onJoinID wamp.ID
	errChan := make(chan error)
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) == 0 {
			errChan <- errors.New("missing argument")
			return
		}
		details = wamp.NormalizeDict(args[0])
		if details == nil {
			errChan <- errors.New("argument was not wamp.Dict")
			return
		}
		onJoinID = wamp.OptionID(details, "session")
		errChan <- nil
	}

	// Subscribe to event.
	err = subscriber.Subscribe(metaOnJoin, evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Wait for any event from subscriber joining.
	var timeout bool
	for !timeout {
		select {
		case <-errChan:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	// Connect client to generate wamp.session.on_join event.
	sess, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	// Make sure the event was received.
	select {
	case err = <-errChan:
		if err != nil {
			t.Fatal("Event error:", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get published event")
	}

	if onJoinID != sess.ID() {
		t.Fatal(metaOnJoin, "meta even had wrong session ID")
	}

	err = subscriber.Unsubscribe(metaOnJoin)
	if err != nil {
		t.Fatal("unsubscribe error:", err)
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
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		t.Fatal("Failed to connect client:", err)
	}

	var onLeaveID wamp.ID
	errChan := make(chan error)
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) == 0 {
			errChan <- errors.New("missing argument")
			return
		}
		var ok bool
		onLeaveID, ok = wamp.AsID(args[0])
		if !ok {
			errChan <- errors.New("argument was not wamp.ID")
			return
		}
		errChan <- nil
	}

	// Subscribe to event.
	err = subscriber.Subscribe(metaOnLeave, evtHandler, nil)
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
		case err = <-errChan:
			onLeaveID = wamp.ID(0)
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	// Disconnect client to generate wamp.session.on_leave event.
	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	// Make sure the event was received.
	select {
	case err = <-errChan:
		if err != nil {
			t.Fatal("Event error:", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not get published event")
	}

	if onLeaveID != sess.ID() {
		t.Fatal(metaOnLeave, "meta even had wrong session ID, got", onLeaveID,
			"want", sess.ID())
	}

	err = subscriber.Unsubscribe(metaOnLeave)
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}
	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestMetaProcSessionCount(t *testing.T) {
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
	sync := make(chan struct{})
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		sync <- struct{}{}
	}
	err = subscriber.Subscribe(metaOnJoin, evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}
	err = subscriber.Subscribe(metaOnLeave, evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Wait for any events from previously closed clients.
	var timeout bool
	for !timeout {
		select {
		case <-sync:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	// Call meta procedure to get session count.
	ctx := context.Background()
	result, err := caller.Call(ctx, metaCount, nil, nil, nil, "")
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
	<-sync

	// Call meta procedure to get session count.
	result, err = caller.Call(ctx, metaCount, nil, nil, nil, "")
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
	<-sync

	// Call meta procedure to get session count.
	result, err = caller.Call(ctx, metaCount, nil, nil, nil, "")
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

	subscriber.Unsubscribe(metaOnJoin)
	subscriber.Unsubscribe(metaOnLeave)
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
	sync := make(chan struct{})
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		sync <- struct{}{}
	}
	err = subscriber.Subscribe(metaOnLeave, evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Wait for any events from previously closed clients.
	var timeout bool
	for !timeout {
		select {
		case <-sync:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	// Call meta procedure to get session list.
	ctx := context.Background()
	result, err := caller.Call(ctx, metaList, nil, nil, nil, "")
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

	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
	// Wait for router to register client leaving.
	<-sync

	// Call meta procedure to get session list.
	result, err = caller.Call(ctx, metaList, nil, nil, nil, "")
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
		if id == sess.ID() {
			found = true
			break
		}
	}
	if found {
		t.Fatal("Session ID should not be in session list")
	}

	subscriber.Unsubscribe(metaOnLeave)
	subscriber.Close()

	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}

func TestMetaProcSessionGet(t *testing.T) {
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
	sync := make(chan struct{})
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		sync <- struct{}{}
	}
	err = subscriber.Subscribe(metaOnLeave, evtHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Call meta procedure to get session list.
	ctx := context.Background()
	args := wamp.List{sess.ID()}
	result, err := caller.Call(ctx, metaGet, nil, args, nil, "")
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
	resultID := wamp.OptionID(dict, "session")
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
		case <-sync:
		case <-time.After(200 * time.Millisecond):
			timeout = true
		}
	}

	err = sess.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
	// Wait for router to register client leaving.
	<-sync

	// Call meta procedure to get session list.
	result, err = caller.Call(ctx, metaGet, nil, wamp.List{sess.ID()}, nil, "")
	if err == nil {
		t.Fatal("Expected error")
	}
	rpcErr := err.(client.RPCError)
	if rpcErr.Err.Error != wamp.ErrNoSuchSession {
		t.Fatal("Expected error URI:", wamp.ErrNoSuchSession, "got", rpcErr.Err.Error)
	}

	subscriber.Unsubscribe(metaOnLeave)
	err = subscriber.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}

	err = caller.Close()
	if err != nil {
		t.Fatal("Failed to disconnect client:", err)
	}
}
