package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gammazero/nexus/v3/wamp/crsign"
)

const (
	testRealm   = "nexus.test"
	testAddress = "localhost:8999"

	testRealm2 = "nexus.test2"

	testTopic  = "test.topic1"
	testTopic2 = "test.topic2"
)

var logger stdlog.StdLog

func init() {
	logger = log.New(os.Stdout, "", log.LstdFlags)
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
		EnableMetaKill:   true,
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

type realmConfigMutator func(*router.RealmConfig)

func newTestRealmConfig(realmName string, fns ...realmConfigMutator) *router.RealmConfig {
	realmConfig := &router.RealmConfig{
		URI:              wamp.URI(realmName),
		StrictURI:        true,
		AnonymousAuth:    true,
		AllowDisclose:    true,
		RequireLocalAuth: true,
	}
	for _, fn := range fns {
		fn(realmConfig)
	}
	return realmConfig
}

type clientConfigMutator func(*Config)

func newTestClientConfig(realmName string, fns ...clientConfigMutator) *Config {
	clientConfig := &Config{
		Realm:           realmName,
		ResponseTimeout: 500 * time.Millisecond,
		Logger:          logger,
		Debug:           false,
	}
	for _, fn := range fns {
		fn(clientConfig)
	}
	return clientConfig
}

func newTestClientWithConfig(r router.Router, clientConfig *Config) (*Client, error) {
	return ConnectLocal(r, *clientConfig)
}

func newTestClient(r router.Router) (*Client, error) {
	return newTestClientWithConfig(r, newTestClientConfig(testRealm))
}

func TestJoinRealm(t *testing.T) {
	defer leaktest.Check(t)()

	realmConfig := newTestRealmConfig(testRealm)
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
		t.Fatalf("Expected non-0 client id, saw %q", client.ID())
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
	eventHandler := func(event *wamp.Event) {
		arg, _ := wamp.AsString(event.Arguments[0])
		if arg != "hello world" {
			errChan <- errors.New("event missing or bad args")
			return
		}
		origTopic, _ := wamp.AsURI(event.Details["topic"])
		if origTopic != wamp.URI(testTopic) {
			errChan <- errors.New("wrong original topic")
			return
		}
		errChan <- nil
	}

	// Expect invalid URI error if not setting match option.
	wcTopic := "nexus..topic"
	err = sub.Subscribe(wcTopic, eventHandler, nil)
	if err == nil {
		t.Fatal("expected invalid uri error")
	}

	// Subscribe should work with match set to wildcard.
	err = sub.Subscribe(wcTopic, eventHandler, wamp.SetOption(nil, "match", "wildcard"))
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

	err = sub.Unsubscribe(wcTopic)
	if err != nil {
		t.Fatal("unsubscribe error:", err)
	}

	// ***Testing PPT Mode****

	// Publish with invalid PPT Scheme and check for error.
	options := wamp.Dict{
		wamp.OptPPTScheme:     "invalid_scheme",
		wamp.OptPPTSerializer: "native",
	}
	args = wamp.List{"hello world"}
	err = pub.Publish(testTopic, options, args, nil)
	if err == nil {
		t.Fatal("Expected error publishing with invalid PPT Scheme")
	}

	// Make sure the event was not received.
	select {
	case <-time.After(time.Millisecond):
	case <-errChan:
		t.Fatal("Should not have called event handler")
	}

	// Publish with invalid PPT serializer and check for error.
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "invalid_serializer",
	}
	args = wamp.List{"hello world"}
	err = pub.Publish(testTopic, options, args, nil)
	if err == nil {
		t.Fatal("Expected error publishing with invalid PPT serializer")
	}

	// Make sure the event was not received.
	select {
	case <-time.After(time.Millisecond):
	case <-errChan:
		t.Fatal("Should not have called event handler")
	}

	eventHandler = func(event *wamp.Event) {
		arg, _ := wamp.AsString(event.Arguments[0])
		if arg != "hello world" {
			errChan <- errors.New("event missing or bad args")
			return
		}
		kwarg, _ := wamp.AsString(event.ArgumentsKw["prop"])
		if kwarg != "hello world" {
			errChan <- errors.New("event missing or bad kwargs")
			return
		}
		errChan <- nil
	}

	// Subscribe to test topic
	testTopic = "test.ppt"
	err = sub.Subscribe(testTopic, eventHandler, nil)
	if err != nil {
		t.Fatal("subscribe error:", err)
	}

	// Publish an event within custom ppt scheme and native serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "native",
	}
	args = wamp.List{"hello world"}
	kwargs := wamp.Dict{"prop": "hello world"}
	err = pub.Publish(testTopic, options, args, kwargs)
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

	// Publish an event within custom ppt scheme and cbor serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "cbor",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	err = pub.Publish(testTopic, options, args, kwargs)
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

	// Publish an event within custom ppt scheme and msgpack serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "msgpack",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	err = pub.Publish(testTopic, options, args, kwargs)
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

	// Publish an event within custom ppt scheme and json serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "json",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	err = pub.Publish(testTopic, options, args, kwargs)
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
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		return InvokeResult{Args: wamp.List{inv.Arguments[0].(int) * 37}}
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
	result, err := caller.Call(ctx, procName, nil, callArgs, nil, nil)
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
	result, err = caller.Call(ctx, procName, nil, callArgs, nil, nil)
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

	// ***Testing PPT Mode****

	// Test registering a valid procedure.
	handler = func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		arg, _ := wamp.AsString(inv.Arguments[0])
		if arg != "hello world" {
			t.Fatal("event missing or bad args")
		}
		kwarg, _ := wamp.AsString(inv.ArgumentsKw["prop"])
		if kwarg != "hello world" {
			t.Fatal("event missing or bad kwargs")
		}

		resArgs := wamp.List{"goodbye world"}
		resKwargs := wamp.Dict{"prop": "goodbye world"}
		options := wamp.Dict{
			wamp.OptPPTScheme:     inv.Details[wamp.OptPPTScheme],
			wamp.OptPPTSerializer: inv.Details[wamp.OptPPTSerializer],
		}

		return InvokeResult{Args: resArgs, Kwargs: resKwargs, Options: options}
	}
	procName = "test.ppt"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure:", err)
	}

	// Test calling the procedure with invalid PPT Scheme
	options := wamp.Dict{
		wamp.OptPPTScheme:     "invalid_scheme",
		wamp.OptPPTSerializer: "native",
	}
	args := wamp.List{"hello world"}
	kwargs := wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	if err == nil {
		t.Fatal("Expected error calling procedure")
	}

	// Test calling the procedure with invalid PPT serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "invalid_serializer",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	if err == nil {
		t.Fatal("Expected error calling procedure")
	}

	// Test calling the procedure within custom ppt scheme and native serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "native",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	if err != nil {
		t.Fatal("failed to call procedure:", err)
	}
	if result.Arguments[0] != "goodbye world" {
		t.Fatal("wrong result:", result.Arguments)
	}
	if result.ArgumentsKw["prop"] != "goodbye world" {
		t.Fatal("wrong result:", result.ArgumentsKw)
	}

	// Test calling the procedure within custom ppt scheme and json serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "json",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	if err != nil {
		t.Fatal("failed to call procedure:", err)
	}
	if result.Arguments[0] != "goodbye world" {
		t.Fatal("wrong result:", result.Arguments)
	}
	if result.ArgumentsKw["prop"] != "goodbye world" {
		t.Fatal("wrong result:", result.ArgumentsKw)
	}

	// Test calling the procedure within custom ppt scheme and cbor serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "cbor",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	if err != nil {
		t.Fatal("failed to call procedure:", err)
	}
	if result.Arguments[0] != "goodbye world" {
		t.Fatal("wrong result:", result.Arguments)
	}
	if result.ArgumentsKw["prop"] != "goodbye world" {
		t.Fatal("wrong result:", result.ArgumentsKw)
	}

	// Test calling the procedure within custom ppt scheme and msgpack serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "msgpack",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	if err != nil {
		t.Fatal("failed to call procedure:", err)
	}
	if result.Arguments[0] != "goodbye world" {
		t.Fatal("wrong result:", result.Arguments)
	}
	if result.ArgumentsKw["prop"] != "goodbye world" {
		t.Fatal("wrong result:", result.ArgumentsKw)
	}

	// Bad handler
	handler = func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		resArgs := wamp.List{"goodbye world"}
		resKwargs := wamp.Dict{"prop": "goodbye world"}
		options := wamp.Dict{
			wamp.OptPPTScheme: "invalid_scheme",
		}

		return InvokeResult{Args: resArgs, Kwargs: resKwargs, Options: options}
	}
	procName = "test.ppt.invoke.bad.scheme"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure:", err)
	}

	// Test calling the procedure with invalid PPT Scheme in YIELD
	args = wamp.List{"hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, nil, args, nil, nil)
	if err == nil {
		t.Fatal("Expected error calling procedure")
	}

	// Bad handler
	handler = func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		resArgs := wamp.List{"goodbye world"}
		resKwargs := wamp.Dict{"prop": "goodbye world"}
		options := wamp.Dict{
			wamp.OptPPTScheme:     "x_custom",
			wamp.OptPPTSerializer: "invalid_serializer",
		}

		return InvokeResult{Args: resArgs, Kwargs: resKwargs, Options: options}
	}
	procName = "test.ppt.invoke.bad.serializer"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure:", err)
	}

	// Test calling the procedure with invalid PPT Scheme in YIELD
	args = wamp.List{"hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, nil, args, nil, nil)
	if err == nil {
		t.Fatal("Expected error calling procedure")
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

	// Handler sends progressive results.
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		senderr := callee.SendProgress(ctx, wamp.List{"Alpha"}, nil)
		if senderr != nil {
			fmt.Println("Error sending Alpha progress:", senderr)
			return InvokeResult{Err: "test.failed"}
		}
		time.Sleep(500 * time.Millisecond)

		senderr = callee.SendProgress(ctx, wamp.List{"Bravo"}, nil)
		if senderr != nil {
			fmt.Println("Error sending Bravo progress:", senderr)
			return InvokeResult{Err: "test.failed"}
		}
		time.Sleep(500 * time.Millisecond)

		senderr = callee.SendProgress(ctx, wamp.List{"Charlie"}, nil)
		if senderr != nil {
			fmt.Println("Error sending Charlie progress:", senderr)
			return InvokeResult{Err: "test.failed"}
		}
		time.Sleep(500 * time.Millisecond)

		var sum int64
		for _, arg := range inv.Arguments {
			n, ok := wamp.AsInt64(arg)
			if ok {
				sum += n
			}
		}
		return InvokeResult{Args: wamp.List{sum}}
	}

	procName := "nexus.test.progproc"

	// Register procedure
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("Failed to register procedure:", err)
	}

	progCount := 0
	progHandler := func(result *wamp.Result) {
		arg := result.Arguments[0].(string)
		if (progCount == 0 && arg == "Alpha") || (progCount == 1 && arg == "Bravo") || (progCount == 2 && arg == "Charlie") {
			progCount++
		}
	}

	// Test calling the procedure.
	callArgs := wamp.List{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ctx := context.Background()
	result, err := caller.Call(ctx, procName, nil, callArgs, nil, progHandler)
	if err != nil {
		t.Fatal("Failed to call procedure:", err)
	}
	sum, ok := wamp.AsInt64(result.Arguments[0])
	if !ok {
		t.Fatal("Could not convert result to int64:", result.Arguments[0])
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
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		return InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure:", err)
	}

	if err = caller.SetCallCancelMode(wamp.CancelModeKillNoWait); err != nil {
		t.Fatal(err)
	}
	errChan := make(chan error)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Calling the procedure, should block.
	go func() {
		callArgs := wamp.List{73}
		_, e := caller.Call(ctx, procName, nil, callArgs, nil, nil)
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

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("expected context.DeadlineExceeded error")
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
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		return InvokeResult{Err: wamp.ErrCanceled}
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
		_, e := caller.Call(ctx, procName, nil, callArgs, nil, nil)
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

	if !errors.Is(err, context.Canceled) {
		t.Fatal("expected context.Canceled error")
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
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		return InvokeResult{Err: wamp.ErrCanceled}
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
		_, e := caller.Call(ctx, procName, opts, callArgs, nil, nil)
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

// ---- network testing ----

func TestConnectContext(t *testing.T) {
	const (
		expect     = "dial tcp: (.*)operation was canceled"
		unixExpect = "dial unix /tmp/wamp.sock: operation was canceled"
	)

	cfg := Config{
		Realm:           testRealm,
		ResponseTimeout: 500 * time.Millisecond,
		Logger:          logger,
		Debug:           false,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ConnectNet(ctx, "http://localhost:9999/ws", cfg)
	resStrMatch, _ := regexp.MatchString(expect, err.Error())
	if err == nil || !resStrMatch {
		t.Fatalf("expected error %q, got %q", expect, err)
	}

	_, err = ConnectNet(ctx, "https://localhost:9999/ws", cfg)
	resStrMatch, _ = regexp.MatchString(expect, err.Error())
	if err == nil || !resStrMatch {
		t.Fatalf("expected error %q, got %q", expect, err)
	}

	_, err = ConnectNet(ctx, "tcp://localhost:9999", cfg)
	resStrMatch, _ = regexp.MatchString(expect, err.Error())
	if err == nil || !resStrMatch {
		t.Fatalf("expected error %q, got %q", expect, err)
	}

	_, err = ConnectNet(ctx, "tcps://localhost:9999", cfg)
	resStrMatch, _ = regexp.MatchString(expect, err.Error())
	if err == nil || !resStrMatch {
		t.Fatalf("expected error %q, got %q", expect, err)
	}

	_, err = ConnectNet(ctx, "unix:///tmp/wamp.sock", cfg)
	if err == nil || err.Error() != unixExpect {
		t.Fatalf("expected error %s, got %s", expect, err)
	}
}

func createTestServer() (router.Router, io.Closer, error) {
	realmConfig := &router.RealmConfig{
		URI:            wamp.URI(testRealm),
		StrictURI:      true,
		AnonymousAuth:  true,
		AllowDisclose:  true,
		EnableMetaKill: true,
	}
	r, err := getTestRouter(realmConfig)
	if err != nil {
		return nil, nil, err
	}

	// Create and run server.
	closer, err := router.NewWebsocketServer(r).ListenAndServe(testAddress)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("Websocket server listening on ws://%s/", testAddress)

	return r, closer, nil
}

func newNetTestClientConfig(realmName string, logger *log.Logger) *Config {
	clientConfig := Config{
		Realm:           realmName,
		ResponseTimeout: 10 * time.Millisecond,
		Serialization:   MSGPACK,
		Logger:          logger,
		Debug:           true,
	}
	return &clientConfig
}

func newNetTestCalleeWithConfig(routerURL string, clientConfig *Config) (*Client, error) {
	cl, err := ConnectNet(context.Background(), routerURL, *clientConfig)
	if err != nil {
		return nil, fmt.Errorf("connect error: %w", err)
	}

	sleep := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		logger.Println("sleep rpc start")
		time.Sleep(5 * time.Second)
		logger.Println("sleep rpc done")
		return InvokeResult{Kwargs: wamp.Dict{"success": true}}
	}

	for ii := 0; ii < 40; ii++ {
		procedureName := fmt.Sprintf("sleep_%d", ii)
		if err = cl.Register(procedureName, sleep, nil); err != nil {
			// Expect to get kill before we get through the list of register functions
			logger.Println("Register", procedureName, "err", err)
		} else {
			logger.Println("Registered procedure", procedureName, "with router")
		}
	}
	return cl, nil
}

func newNetTestCallee(routerURL string) (*Client, error) {
	return newNetTestCalleeWithConfig(
		routerURL,
		newNetTestClientConfig(testRealm, log.New(os.Stderr, "CALLEE> ", log.Lmicroseconds)),
	)
}

func newNetTestKillerWithConfig(t *testing.T, routerURL string, clientConfig *Config) (*Client, error) {
	logger := clientConfig.Logger
	cl, err := ConnectNet(context.Background(), routerURL, *clientConfig)
	if err != nil {
		return nil, err
	}

	// Define function to handle on_join events received.
	onJoin := func(event *wamp.Event) {
		details, ok := wamp.AsDict(event.Arguments[0])
		if !ok {
			logger.Println("Client joined realm - no data provided")
			return
		}
		onJoinID, _ := wamp.AsID(details["session"])
		logger.Printf("Client %v joined realm\n", onJoinID)

		go func() {
			// Call meta procedure to kill new client
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			killArgs := wamp.List{onJoinID}
			killKwArgs := wamp.Dict{"reason": "com.session.kill", "message": "because i can"}
			var result *wamp.Result
			result, err = cl.Call(ctx, string(wamp.MetaProcSessionKill), nil, killArgs, killKwArgs, nil)
			if err != nil {
				logger.Printf("Kill new client failed err %s", err)
			}
			if result == nil {
				logger.Println("Kill new client returned no result")
			}
		}()
	}

	// Subscribe to on_join topic.
	err = cl.Subscribe(string(wamp.MetaEventSessionOnJoin), onJoin, nil)
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}
	logger.Println("Subscribed to", wamp.MetaEventSessionOnJoin)

	return cl, nil
}

func newNetTestKiller(t *testing.T, routerURL string) (*Client, error) {
	return newNetTestKillerWithConfig(
		t,
		routerURL,
		newNetTestClientConfig(testRealm, log.New(os.Stderr, "KILLER> ", log.Lmicroseconds)),
	)
}

// Test for races in client when session is killed by router.
func TestClientRace(t *testing.T) {
	// Create a websocket server
	r, closer, err := createTestServer()
	if err != nil {
		t.Fatal("failed to connect test clients:", err)
	}

	testUrl := fmt.Sprintf("ws://%s/ws", testAddress)
	killer, err := newNetTestKiller(t, testUrl)
	if err != nil {
		t.Fatal("failed to connect caller:", err)
	}
	logger.Println("Starting callee")

	callee, err := newNetTestCallee(testUrl)
	if err != nil {
		t.Fatal("failed to connect callee:", err)
	}

	// If we hit a race condition with the client register, we do not ever return from the Register()
	logger.Println("Finished test - cleanup")

	closer.Close()
	r.Close()
	killer.Close()
	callee.Close()
}

// Test that if the router disconnects the client, while the client is running
// an invocation handler, that the handler still gets marked as done when it
// completes.
func TestInvocationHandlerMissedDone(t *testing.T) {
	//defer leaktest.Check(t)()

	// Connect two clients to the same server
	callee, caller, r, err := connectedTestClients()
	if err != nil {
		t.Fatal("failed to connect test clients:", err)
	}

	calledChan := make(chan struct{})

	// Register procedure.
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		close(calledChan)
		<-ctx.Done()
		time.Sleep(2 * time.Second)
		return InvokeResult{Args: wamp.List{inv.Arguments[0].(int) * 37}}
	}
	procName := "myproc"
	if err = callee.Register(procName, handler, nil); err != nil {
		t.Fatal("failed to register procedure:", err)
	}

	// Call procedure
	callArgs := wamp.List{73}
	ctx := context.Background()

	go caller.Call(ctx, procName, nil, callArgs, nil, nil)

	<-calledChan

	killArgs := wamp.List{callee.ID()}
	killKwArgs := wamp.Dict{"reason": "com.session.kill", "message": "because i can"}
	var result *wamp.Result
	result, err = caller.Call(ctx, string(wamp.MetaProcSessionKill), nil, killArgs, killKwArgs, nil)
	if err != nil {
		t.Log("Kill new client failed:", err)
	}
	if result == nil {
		t.Log("Kill new client returned no result")
	}

	caller.Close()

	done := make(chan struct{})
	go func() {
		callee.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting to close client")
	}
	r.Close()
}

func TestProgressDisconnect(t *testing.T) {
	defer leaktest.Check(t)()

	// Create a websocket server
	r, closer, err := createTestServer()
	if err != nil {
		t.Fatal("failed to connect test clients:", err)
	}
	//defer r.Close()
	defer closer.Close()

	testURL := fmt.Sprintf("ws://%s/ws", testAddress)

	// Connect callee session.
	calleeLog := log.New(os.Stderr, "CALLEE> ", log.Lmicroseconds)
	cfg := Config{
		Realm:           testRealm,
		ResponseTimeout: 10 * time.Millisecond,
		Logger:          calleeLog,
		Debug:           true,
	}
	callee, err := ConnectNet(context.Background(), testURL, cfg)
	if err != nil {
		t.Fatalf("connect error: %s", err)
	}
	defer callee.Close()

	const chunkProc = "example.progress.text"
	sendProgErr := make(chan error)
	disconnect := make(chan struct{})
	disconnected := make(chan struct{})

	// Define invocation handler.
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		for {
			// Send a chunk of data.
			e := callee.SendProgress(ctx, wamp.List{"hello"}, nil)
			if e != nil {
				sendProgErr <- e
				return InvocationCanceled
			}
			<-disconnected
		}
		// Never gets here
	}

	// Register procedure.
	if err = callee.Register(chunkProc, handler, nil); err != nil {
		t.Fatal("Failed to register procedure:", err)
	}

	// Connect caller session.
	cfg.Logger = log.New(os.Stderr, "CALLER> ", log.Lmicroseconds)
	caller, err := ConnectNet(context.Background(), testURL, cfg)
	if err != nil {
		t.Fatalf("connect error: %s", err)
	}
	defer caller.Close()

	progHandler := func(result *wamp.Result) {
		close(disconnect)
	}

	// All results, for all calls, must be received by timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	progErr := make(chan error)
	go func() {
		_, perr := caller.Call(ctx, chunkProc, nil, nil, nil, progHandler)
		progErr <- perr
	}()

	<-disconnect
	closer.Close()
	r.Close()
	close(disconnected)

	// Check for expected error from caller.
	err = <-progErr
	if !errors.Is(err, ErrNotConn) {
		t.Fatalf("expected error from caller: %q got %q", ErrNotConn, err)
	}

	// Check for expected error from callee.
	err = <-sendProgErr
	if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrNotConn) {
		t.Fatalf("wrong error from SendProgress: %s", err)
	}
}

type testEmptyDictLeakAuthorizer struct {
}

func (*testEmptyDictLeakAuthorizer) Authorize(sess *wamp.Session, message wamp.Message) (bool, error) {
	var (
		subMsg *wamp.Subscribe
		ok     bool
	)
	if _, ok = message.(*wamp.Goodbye); ok {
		return true, nil
	}
	if subMsg, ok = message.(*wamp.Subscribe); !ok {
		panic(fmt.Sprintf("I can only handle %T, saw %T", subMsg, message))
	}
	if v, ok := subMsg.Options["leaktest"]; ok {
		panic(fmt.Sprintf("leaktest should be empty, saw %v", v))
	}
	subMsg.Options["leaktest"] = "oops"
	return true, nil
}

func TestEmptyDictLeak(t *testing.T) {
	defer leaktest.Check(t)()

	// realm configs
	realm1Config := newTestRealmConfig(testRealm, func(config *router.RealmConfig) {
		config.Authorizer = new(testEmptyDictLeakAuthorizer)
		config.RequireLocalAuthz = true
	})
	realm2Config := newTestRealmConfig(testRealm2, func(config *router.RealmConfig) {
		config.Authorizer = new(testEmptyDictLeakAuthorizer)
		config.RequireLocalAuthz = true
	})

	// client configs
	client1Config := newTestClientConfig(testRealm)
	client2Config := newTestClientConfig(testRealm2)

	// construct routers
	router1, err := getTestRouter(realm1Config)
	if err != nil {
		t.Fatal(err)
	}
	router2, err := getTestRouter(realm2Config)
	if err != nil {
		t.Fatal(err)
	}

	// create local clients to each realm
	client1, err := newTestClientWithConfig(router1, client1Config)
	if err != nil {
		t.Fatal(err)
	}
	if client1.ID() == wamp.ID(0) {
		t.Fatalf("Expected non-0 client id, saw %q", client1.ID())
	}
	client2, err := newTestClientWithConfig(router2, client2Config)
	if err != nil {
		t.Fatal(err)
	}
	if client2.ID() == wamp.ID(0) {
		t.Fatalf("Expected non-0 client id, saw %q", client2.ID())
	}

	// subscribe to topics in each realm and test for leak
	err = client1.Subscribe(testTopic, func(*wamp.Event) {}, nil)
	if err != nil {
		t.Fatalf("Error during subscribe: %v", err)
	}
	err = client2.Subscribe(testTopic2, func(*wamp.Event) {}, nil)
	if err != nil {
		t.Fatalf("Error during subscribe: %v", err)
	}

	client1.Close()
	client2.Close()
	router1.Close()
	router2.Close()
}

func TestEventContentSafety(t *testing.T) {
	defer leaktest.Check(t)()

	// Connect two subscribers and one publisher to router
	sub1, sub2, r, err := connectedTestClients()
	if err != nil {
		t.Fatal("failed to connect subscribers clients:", err)
	}
	defer sub1.Close()
	defer sub2.Close()
	defer r.Close()
	pub, err := newTestClient(r)
	if err != nil {
		t.Fatal("failed to connect published client:", err)
	}
	defer pub.Close()

	errChan := make(chan error)
	gate := make(chan struct{}, 1)
	eventHandler := func(event *wamp.Event) {
		gate <- struct{}{}
		_, ok := event.Details["oops"]
		if ok {
			errChan <- errors.New("should not have seen oops")
			<-gate
			return
		}
		arg, ok := event.Arguments[0].(string)
		if !ok {
			errChan <- errors.New("arg was not strings")
			<-gate
			return
		}
		if arg != "Hello" {
			errChan <- fmt.Errorf("expected \"Hello\", got %q", arg)
			<-gate
			return
		}

		prop, ok := event.ArgumentsKw["prop"]
		if !ok || prop != "value" {
			errChan <- fmt.Errorf("expected kwArgs 'prop':'value', got %q", prop)
			<-gate
			return
		}

		event.Details["oops"] = true
		event.Arguments[0] = "oops"
		event.ArgumentsKw["prop"] = "oops"
		errChan <- nil
		<-gate
	}

	// Expect invalid URI error if not setting match option.
	if err = sub1.Subscribe(testTopic, eventHandler, nil); err != nil {
		t.Fatal(err)
	}
	if err = sub2.Subscribe(testTopic, eventHandler, nil); err != nil {
		t.Fatal(err)
	}

	if err = pub.Publish(testTopic, nil, wamp.List{"Hello"}, wamp.Dict{"prop": "value"}); err != nil {
		t.Fatal("Failed to publish:", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case err = <-errChan:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("did not get published event")
		}
	}
}

func TestRouterGetFeatures(t *testing.T) {
	realmConfig := &router.RealmConfig{
		URI:           wamp.URI("nexus.test.auth"),
		StrictURI:     true,
		AnonymousAuth: true,
	}
	r, err := getTestRouter(realmConfig)
	if err != nil {
		t.Fatal(err)
	}

	routerFeatures := r.GetRouterFeatures()

	dealerFeatures := (*routerFeatures)["roles"].(wamp.Dict)[wamp.RoleDealer].(wamp.Dict)
	if len(dealerFeatures) < 1 {
		t.Fatal("dealer features are missed")
	}

	brokerFeatures := (*routerFeatures)["roles"].(wamp.Dict)[wamp.RoleBroker].(wamp.Dict)
	if len(brokerFeatures) < 1 {
		t.Fatal("broker features are missed")
	}
}
