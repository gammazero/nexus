package client

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/router/auth"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/wamp"
	"github.com/gammazero/nexus/wamp/crsign"
)

const (
	testRealm = "nexus.test"
)

var logger stdlog.StdLog

func init() {
	logger = log.New(os.Stdout, "", log.LstdFlags)
}

func getTestPeer(r router.Router) wamp.Peer {
	cli, rtr := transport.LinkedPeers()
	go r.Attach(rtr)
	return cli
}

func getTestRouter(realmConfig *router.RealmConfig) (router.Router, error) {
	config := &router.Config{
		RealmConfigs: []*router.RealmConfig{realmConfig},
	}
	return router.NewRouter(config, logger)
}

func connectedTestClients() (*Client, *Client, router.Router, error) {
	realmConfig := &router.RealmConfig{
		URI:              wamp.URI(testRealm),
		StrictURI:        true,
		AnonymousAuth:    true,
		AllowDisclose:    true,
		RequireLocalAuth: true,
	}
	r, err := getTestRouter(realmConfig)
	if err != nil {
		return nil, nil, nil, err
	}

	c1, err := newTestClient(r)
	if err != nil {
		return nil, nil, nil, err
	}
	c2, err := newTestClient(r)
	if err != nil {
		return nil, nil, nil, err
	}
	return c1, c2, r, nil
}

func newTestClient(r router.Router) (*Client, error) {
	cfg := Config{
		Realm:           testRealm,
		ResponseTimeout: 500 * time.Millisecond,
		Logger:          logger,
		Debug:           true,
	}
	return ConnectLocal(r, cfg)
}

func TestJoinRealm(t *testing.T) {
	defer leaktest.Check(t)()

	realmConfig := &router.RealmConfig{
		URI:              wamp.URI(testRealm),
		StrictURI:        true,
		AnonymousAuth:    true,
		AllowDisclose:    true,
		RequireLocalAuth: true,
	}
	r, err := getTestRouter(realmConfig)
	if err != nil {
		t.Fatal(err)
	}

	// Test that client can join realm.
	client, err := newTestClient(r)
	if err != nil {
		t.Fatal(err)
	}

	if client.ID() == wamp.ID(0) {
		t.Fatal("Invalid client ID")
	}

	realmInfo := client.RealmDetails()
	_, err = wamp.DictValue(realmInfo, []string{"roles", "broker"})
	if err != nil {
		t.Fatal("Router missing broker role")
	}
	_, err = wamp.DictValue(realmInfo, []string{"roles", "dealer"})
	if err != nil {
		t.Fatal("Router missing dealer role")
	}

	client.Close()
	r.Close()

	if err = client.Close(); err == nil {
		t.Fatal("No error from double Close()")
	}

	// Test the Done is signaled.
	select {
	case <-client.Done():
	case <-time.After(time.Millisecond):
		t.Fatal("Expected client done")
	}

	// Test that client cannot join realm when anonymous auth is disabled.
	realmConfig = &router.RealmConfig{
		URI:              wamp.URI("nexus.testnoanon"),
		StrictURI:        true,
		AnonymousAuth:    false,
		AllowDisclose:    false,
		RequireLocalAuth: true,
	}
	r, err = getTestRouter(realmConfig)
	if err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Realm:           "nexus.testnoanon",
		ResponseTimeout: 500 * time.Millisecond,
		Logger:          logger,
	}
	_, err = ConnectLocal(r, cfg)
	if err == nil {
		t.Fatal("expected error due to no anonymous authentication")
	}
	r.Close()
}

func TestClientJoinRealmWithCRAuth(t *testing.T) {
	defer leaktest.Check(t)()

	crAuth := auth.NewCRAuthenticator(&serverKeyStore{"static"}, time.Second)
	realmConfig := &router.RealmConfig{
		URI:              wamp.URI("nexus.test.auth"),
		StrictURI:        true,
		AnonymousAuth:    false,
		AllowDisclose:    false,
		Authenticators:   []auth.Authenticator{crAuth},
		RequireLocalAuth: true,
	}
	r, err := getTestRouter(realmConfig)
	if err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Realm: "nexus.test.auth",
		HelloDetails: wamp.Dict{
			"authid": "jdoe",
		},
		AuthHandlers: map[string]AuthFunc{
			"wampcra": clientAuthFunc,
		},
		Logger: logger,
	}
	client, err := ConnectLocal(r, cfg)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	r.Close()
}

func TestSubscribe(t *testing.T) {
	defer leaktest.Check(t)()

	// Connect two clients to the same server
	sub, pub, r, err := connectedTestClients()
	if err != nil {
		t.Fatal("failed to connect test clients:", err)
	}

	testTopic := "nexus.test.topic"
	errChan := make(chan error)
	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		arg, _ := wamp.AsString(args[0])
		if arg != "hello world" {
			errChan <- errors.New("event missing or bad args")
			return
		}
		origTopic := wamp.OptionURI(details, "topic")
		if origTopic != wamp.URI(testTopic) {
			errChan <- errors.New("wrong original topic")
			return
		}
		errChan <- nil
	}

	// Expect invalid URI error if not setting match option.
	wcTopic := "nexus..topic"
	err = sub.Subscribe(wcTopic, evtHandler, nil)
	if err == nil {
		t.Fatal("expected invalid uri error")
	}

	// Subscribe should work with match set to wildcard.
	err = sub.Subscribe(wcTopic, evtHandler, wamp.SetOption(nil, "match", "wildcard"))
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Test getting subscription ID.
	if _, ok := sub.SubscriptionID(wcTopic); !ok {
		t.Fatal("Did not get subscription ID")
	}
	if _, ok := sub.SubscriptionID("no.such.topic"); ok {
		t.Fatal("Expected !ok looking up subscription ID for bad topic")
	}

	// Publish an event to something that matches by wildcard.
	args := wamp.List{"hello world"}
	err = pub.Publish(testTopic, nil, args, nil)
	if err != nil {
		t.Fatal("Failed to publish without ack:", err)
	}
	// Make sure the event was received.
	select {
	case err = <-errChan:
	case <-time.After(time.Second):
		t.Fatal("did not get published event")
	}
	if err != nil {
		t.Fatal(err)
	}

	opts := wamp.SetOption(nil, wamp.OptAcknowledge, true)
	err = pub.Publish(testTopic, opts, args, nil)
	if err != nil {
		t.Fatal("Failed to publish with ack:", err)
	}
	// Make sure the event was received.
	select {
	case err = <-errChan:
	case <-time.After(time.Second):
		t.Fatal("did not get published event")
	}
	if err != nil {
		t.Fatal(err)
	}

	// Publish to invalid URI and check for error.
	err = pub.Publish(".bad-uri.bad bad.", opts, args, nil)
	if err == nil {
		t.Fatal("Expected error publishing to bad URI with ack")
	}

	// Make sure the event was not received.
	select {
	case <-time.After(time.Millisecond):
	case err = <-errChan:
		t.Fatal("Should not have called event handler")
	}

	err = sub.Unsubscribe(wcTopic)
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}
	pub.Close()
	sub.Close()
	r.Close()
}

func TestRemoteProcedureCall(t *testing.T) {
	defer leaktest.Check(t)()

	// Connect two clients to the same server
	callee, caller, r, err := connectedTestClients()
	if err != nil {
		t.Fatal("failed to connect test clients:", err)
	}

	// Test unregister invalid procedure.
	if err = callee.Unregister("invalidmethod"); err == nil {
		t.Fatal("expected error unregistering invalid procedure")
	}

	// Test registering a valid procedure.
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *InvokeResult {
		return &InvokeResult{Args: wamp.List{args[0].(int) * 37}}
	}
	procName := "myproc"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure:", err)
	}

	// Test getting registration ID.
	if _, ok := callee.RegistrationID(procName); !ok {
		t.Fatal("Did not get subscription ID")
	}
	if _, ok := callee.RegistrationID("no.such.procedure"); ok {
		t.Fatal("Expected !ok looking up registration ID for bad procedure")
	}

	// Test calling the procedure.
	callArgs := wamp.List{73}
	ctx := context.Background()
	result, err := caller.Call(ctx, procName, nil, callArgs, nil, "")
	if err != nil {
		t.Fatal("failed to call procedure:", err)
	}
	if result.Arguments[0] != 2701 {
		t.Fatal("wrong result:", result.Arguments)
	}

	// Test unregister.
	if err = callee.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure:", err)
	}

	// Test calling unregistered procedure.
	callArgs = wamp.List{555}
	result, err = caller.Call(ctx, procName, nil, callArgs, nil, "")
	if err == nil {
		t.Fatal("expected error calling unregistered procedure")
	}
	if result != nil {
		t.Fatal("result should be nil on error")
	}
	if !strings.HasSuffix(err.Error(), "wamp.error.no_such_procedure") {
		t.Fatal("Wrong error when calling unregistered procedure")
	}
	rpcErr, ok := err.(RPCError)
	if !ok {
		t.Fatal("Expected err to be RPCError")
	}
	if rpcErr.Err.Error != wamp.ErrNoSuchProcedure {
		t.Fatal("Wrong error URI in RPC error")
	}

	caller.Close()
	callee.Close()
	r.Close()
}

func TestProgressiveCall(t *testing.T) {
	// Connect two clients to the same server
	callee, caller, r, err := connectedTestClients()
	if err != nil {
		t.Fatal("failed to connect test clients:", err)
	}

	// Hanbdler sends progressive results.
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *InvokeResult {
		senderr := callee.SendProgress(ctx, wamp.List{"Alpha"}, nil)
		if senderr != nil {
			fmt.Println("Error sending Alpha progress:", senderr)
			return &InvokeResult{Err: "test.failed"}
		}
		time.Sleep(500 * time.Millisecond)

		senderr = callee.SendProgress(ctx, wamp.List{"Bravo"}, nil)
		if senderr != nil {
			fmt.Println("Error sending Bravo progress:", senderr)
			return &InvokeResult{Err: "test.failed"}
		}
		time.Sleep(500 * time.Millisecond)

		senderr = callee.SendProgress(ctx, wamp.List{"Charlie"}, nil)
		if senderr != nil {
			fmt.Println("Error sending Charlie progress:", senderr)
			return &InvokeResult{Err: "test.failed"}
		}
		time.Sleep(500 * time.Millisecond)

		var sum int64
		for i := range args {
			n, ok := wamp.AsInt64(args[i])
			if ok {
				sum += n
			}
		}
		return &InvokeResult{Args: wamp.List{sum}}
	}

	procName := "nexus.test.progproc"

	// Register procedure
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("Failed to register procedure:", err)
	}

	progCount := 0
	progHandler := func(result *wamp.Result) {
		arg := result.Arguments[0].(string)
		if (progCount == 0 && arg != "Alpha") || (progCount == 1 && arg != "Bravo") || (progCount == 2 && arg != "Charlie") {
			return
		}
		progCount++
	}

	// Test calling the procedure.
	callArgs := wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ctx := context.Background()
	result, err := caller.CallProgress(ctx, procName, nil, callArgs, nil, "", progHandler)
	if err != nil {
		t.Fatal("Failed to call procedure:", err)
	}
	sum, ok := wamp.AsInt64(result.Arguments[0])
	if !ok {
		t.Fatal("Could not convert result to int64")
	}
	if sum != 55 {
		t.Fatal("Wrong result:", sum)
	}
	if progCount != 3 {
		t.Fatal("Expected progCount == 3")
	}

	// Test unregister.
	if err = callee.Unregister(procName); err != nil {
		t.Fatal("Failed to unregister procedure:", err)
	}

	caller.Close()
	callee.Close()
	r.Close()
}

func TestTimeoutCancelRemoteProcedureCall(t *testing.T) {
	defer leaktest.Check(t)()

	// Connect two clients to the same server
	callee, caller, r, err := connectedTestClients()
	if err != nil {
		t.Fatal("failed to connect test clients:", err)
	}

	// Test registering a valid procedure.
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		return &InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure:", err)
	}

	errChan := make(chan error)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Calling the procedure, should block.
	go func() {
		callArgs := wamp.List{73}
		_, e := caller.Call(ctx, procName, nil, callArgs, nil, "killnowait")
		errChan <- e
	}()

	// Make sure the call is blocked.
	select {
	case err = <-errChan:
		t.Fatal("call should have been blocked")
	case <-time.After(200 * time.Millisecond):
	}

	// Make sure the call is canceled.
	select {
	case err = <-errChan:
	case <-time.After(2 * time.Second):
		t.Fatal("call should have been canceled")
	}

	rpcError, ok := err.(RPCError)
	if !ok {
		t.Fatal("expected RPCError type of error")
	}
	if rpcError.Err.Error != wamp.ErrCanceled {
		t.Fatal("expected canceled error, got:", err)
	}
	if err = callee.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure:", err)
	}

	caller.Close()
	callee.Close()
	r.Close()
}

func TestCancelRemoteProcedureCall(t *testing.T) {
	// Connect two clients to the same server
	callee, caller, r, err := connectedTestClients()
	if err != nil {
		t.Fatal("failed to connect test clients:", err)
	}

	// Test registering a valid procedure.
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		return &InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure:", err)
	}

	errChan := make(chan error)
	ctx, cancel := context.WithCancel(context.Background())
	// Calling the procedure, should block.
	go func() {
		callArgs := wamp.List{73}
		_, e := caller.Call(ctx, procName, nil, callArgs, nil, "killnowait")
		errChan <- e
	}()

	// Make sure the call is blocked.
	select {
	case err = <-errChan:
		t.Fatal("call should have been blocked")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()

	// Make sure the call is canceled.
	select {
	case err = <-errChan:
	case <-time.After(time.Second):
		t.Fatal("call should have been canceled")
	}

	rpcError, ok := err.(RPCError)
	if !ok {
		t.Fatal("expected RPCError type of error")
	}
	if rpcError.Err.Error != wamp.ErrCanceled {
		t.Fatal("expected canceled error, got:", err)
	}
	if err = callee.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure:", err)
	}

	caller.Close()
	callee.Close()
	r.Close()
}

func TestTimeoutRemoteProcedureCall(t *testing.T) {
	defer leaktest.Check(t)()

	// Connect two clients to the same server
	callee, caller, r, err := connectedTestClients()
	if err != nil {
		t.Fatal("failed to connect test clients:", err)
	}

	// Test registering a valid procedure.
	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		return &InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure:", err)
	}

	if err = callee.Register("bad proc! no no", handler, nil); err == nil {
		t.Fatal("Expected error registering with bad procedure name")
	}

	errChan := make(chan error)
	ctx := context.Background()
	opts := wamp.Dict{wamp.OptTimeout: 1000}
	// Calling the procedure, should block.
	go func() {
		callArgs := wamp.List{73}
		_, e := caller.Call(ctx, procName, opts, callArgs, nil, wamp.CancelModeKillNoWait)
		errChan <- e
	}()

	// Make sure the call is blocked.
	select {
	case err = <-errChan:
		t.Fatal("call should have been blocked")
	case <-time.After(200 * time.Millisecond):
	}

	// Make sure the call is canceled.
	select {
	case err = <-errChan:
	case <-time.After(2 * time.Second):
		t.Fatal("call should have been canceled")
	}

	rpcError, ok := err.(RPCError)
	if !ok {
		t.Fatal("expected RPCError type of error")
	}
	if rpcError.Err.Error != wamp.ErrCanceled {
		t.Fatal("expected canceled error, got:", err)
	}
	if err = callee.Unregister(procName); err != nil {
		t.Fatal("failed to unregister procedure:", err)
	}

	caller.Close()
	callee.Close()
	r.Close()
}

// ---- authentication test stuff ------

func clientAuthFunc(c *wamp.Challenge) (string, wamp.Dict) {
	// If the client needed to lookup a user's key, this would require decoding
	// the JSON-encoded ch string and getting the authid. For this example
	// assume that client only operates as one user and knows the key to use.
	sig := crsign.RespondChallenge("squeemishosafradge", c, nil)
	return sig, wamp.Dict{}
}

type serverKeyStore struct {
	provider string
}

func (ks *serverKeyStore) AuthKey(authid, authmethod string) ([]byte, error) {
	if authid != "jdoe" {
		return nil, errors.New("no such user: " + authid)
	}
	switch authmethod {
	case "wampcra":
		// Lookup the user's key.
		return []byte("squeemishosafradge"), nil
	case "ticket":
		// Lookup the user's key.
		return []byte("ticketforjoe1234"), nil
	}
	return nil, nil
}

func (ks *serverKeyStore) PasswordInfo(authid string) (string, int, int) {
	return "", 0, 0
}

func (ks *serverKeyStore) Provider() string { return ks.provider }

func (ks *serverKeyStore) AuthRole(authid string) (string, error) {
	if authid != "jdoe" {
		return "", errors.New("no such user: " + authid)
	}
	return "user", nil
}
