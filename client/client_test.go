package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gammazero/nexus/v3/wamp/crsign"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
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

func checkGoLeaks(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
}

func getTestRouter(t *testing.T, realmConfig *router.RealmConfig) router.Router {
	config := &router.Config{
		RealmConfigs: []*router.RealmConfig{realmConfig},
	}
	r, err := router.NewRouter(config, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		r.Close()
	})
	return r
}

func connectedTestClients(t *testing.T) (*Client, *Client, router.Router) {
	realmConfig := &router.RealmConfig{
		URI:              wamp.URI(testRealm),
		StrictURI:        true,
		AnonymousAuth:    true,
		AllowDisclose:    true,
		RequireLocalAuth: true,
		EnableMetaKill:   true,
	}
	r := getTestRouter(t, realmConfig)

	c1 := newTestClient(t, r)
	c2 := newTestClient(t, r)
	return c1, c2, r
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

func newTestClientWithConfig(t *testing.T, r router.Router, clientConfig *Config) *Client {
	c, err := ConnectLocal(r, *clientConfig)
	require.NoError(t, err, "failed to connect test clients")
	t.Cleanup(func() {
		c.Close()
	})
	return c
}

func newTestClient(t *testing.T, r router.Router) *Client {
	return newTestClientWithConfig(t, r, newTestClientConfig(testRealm))
}

func TestJoinRealm(t *testing.T) {
	checkGoLeaks(t)

	realmConfig := newTestRealmConfig(testRealm)
	r := getTestRouter(t, realmConfig)

	// Test that client can join realm.
	client := newTestClient(t, r)
	require.NotEqual(t, wamp.ID(0), client.ID(), "Expected non-0 client id")

	realmInfo := client.RealmDetails()
	_, err := wamp.DictValue(realmInfo, []string{"roles", "broker"})
	require.NoError(t, err, "Router missing broker role")
	_, err = wamp.DictValue(realmInfo, []string{"roles", "dealer"})
	require.NoError(t, err, "Router missing dealer role")

	client.Close()
	r.Close()
	require.Error(t, client.Close(), "No error from double Close()")

	// Test the Done is signaled.
	select {
	case <-client.Done():
	case <-time.After(time.Millisecond):
		require.FailNow(t, "Expected client done")
	}

	// Test that client cannot join realm when anonymous auth is disabled.
	realmConfig = &router.RealmConfig{
		URI:              wamp.URI("nexus.testnoanon"),
		StrictURI:        true,
		AnonymousAuth:    false,
		AllowDisclose:    false,
		RequireLocalAuth: true,
	}
	r = getTestRouter(t, realmConfig)

	cfg := Config{
		Realm:           "nexus.testnoanon",
		ResponseTimeout: 500 * time.Millisecond,
		Logger:          logger,
	}
	_, err = ConnectLocal(r, cfg)
	require.Error(t, err, "expected error due to no anonymous authentication")
}

func TestClientJoinRealmWithCRAuth(t *testing.T) {
	checkGoLeaks(t)

	crAuth := auth.NewCRAuthenticator(&serverKeyStore{"static"}, time.Second)
	realmConfig := &router.RealmConfig{
		URI:              wamp.URI("nexus.test.auth"),
		StrictURI:        true,
		AnonymousAuth:    false,
		AllowDisclose:    false,
		Authenticators:   []auth.Authenticator{crAuth},
		RequireLocalAuth: true,
	}
	r := getTestRouter(t, realmConfig)

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
	require.NoError(t, err)
	client.Close()
}

func TestSubscribe(t *testing.T) {
	checkGoLeaks(t)

	// Connect two clients to the same server
	sub, pub, _ := connectedTestClients(t)

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
	err := sub.Subscribe(wcTopic, eventHandler, nil)
	require.Error(t, err, "expected invalid uri error")

	// Subscribe should work with match set to wildcard.
	err = sub.Subscribe(wcTopic, eventHandler, wamp.SetOption(nil, "match", "wildcard"))
	require.NoError(t, err)

	// Test getting subscription ID.
	_, ok := sub.SubscriptionID(wcTopic)
	require.True(t, ok, "Did not get subscription ID")
	_, ok = sub.SubscriptionID("no.such.topic")
	require.False(t, ok, "Expected false looking up subscription ID for bad topic")

	// Publish an event to something that matches by wildcard.
	args := wamp.List{"hello world"}
	err = pub.Publish(testTopic, nil, args, nil)
	require.NoError(t, err, "Failed to publish without ack")

	// Make sure the event was received.
	select {
	case err = <-errChan:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "did not get published event")
	}

	opts := wamp.SetOption(nil, wamp.OptAcknowledge, true)
	err = pub.Publish(testTopic, opts, args, nil)
	require.NoError(t, err, "Failed to publish with ack")

	// Make sure the event was received.
	select {
	case err = <-errChan:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "did not get published event")
	}

	// Publish to invalid URI and check for error.
	err = pub.Publish(".bad-uri.bad bad.", opts, args, nil)
	require.Error(t, err, "Expected error publishing to bad URI with ack")

	err = sub.Unsubscribe(wcTopic)
	require.NoError(t, err)

	// ***Testing PPT Mode****

	// Publish with invalid PPT Scheme and check for error.
	options := wamp.Dict{
		wamp.OptPPTScheme:     "invalid_scheme",
		wamp.OptPPTSerializer: "native",
	}
	args = wamp.List{"hello world"}
	err = pub.Publish(testTopic, options, args, nil)
	require.Error(t, err, "Expected error publishing with invalid PPT Scheme")

	// Make sure the event was not received.
	select {
	case <-time.After(time.Millisecond):
	case <-errChan:
		require.FailNow(t, "Should not have called event handler")
	}

	// Publish with invalid PPT serializer and check for error.
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "invalid_serializer",
	}
	args = wamp.List{"hello world"}
	err = pub.Publish(testTopic, options, args, nil)
	require.Error(t, err, "Expected error publishing with invalid PPT serializer")

	// Make sure the event was not received.
	select {
	case <-time.After(time.Millisecond):
	case <-errChan:
		require.FailNow(t, "Should not have called event handler")
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
	require.NoError(t, err)

	// Publish an event within custom ppt scheme and native serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "native",
	}
	args = wamp.List{"hello world"}
	kwargs := wamp.Dict{"prop": "hello world"}
	err = pub.Publish(testTopic, options, args, kwargs)
	require.NoError(t, err, "Failed to publish without ack")

	// Make sure the event was received.
	select {
	case err = <-errChan:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "did not get published event")
	}

	// Publish an event within custom ppt scheme and cbor serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "cbor",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	err = pub.Publish(testTopic, options, args, kwargs)
	require.NoError(t, err, "Failed to publish without ack")

	// Make sure the event was received.
	select {
	case err = <-errChan:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "did not get published event")
	}

	// Publish an event within custom ppt scheme and msgpack serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "msgpack",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	err = pub.Publish(testTopic, options, args, kwargs)
	require.NoError(t, err, "Failed to publish without ack")

	// Make sure the event was received.
	select {
	case err = <-errChan:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "did not get published event")
	}

	// Publish an event within custom ppt scheme and json serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "json",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	err = pub.Publish(testTopic, options, args, kwargs)
	require.NoError(t, err, "Failed to publish without ack")

	// Make sure the event was received.
	select {
	case err = <-errChan:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "did not get published event")
	}
}

func TestRemoteProcedureCall(t *testing.T) {
	checkGoLeaks(t)

	// Connect two clients to the same server
	callee, caller, _ := connectedTestClients(t)

	// Test unregister invalid procedure.
	err := callee.Unregister("invalidmethod")
	require.Error(t, err, "expected error unregistering invalid procedure")

	// Test registering a valid procedure.
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		return InvokeResult{Args: wamp.List{inv.Arguments[0].(int) * 37}}
	}
	procName := "myproc"
	err = callee.Register(procName, handler, nil)
	require.NoError(t, err, "failed to register procedure")

	// Test getting registration ID.
	_, ok := callee.RegistrationID(procName)
	require.True(t, ok, "Did not get subscription ID")
	_, ok = callee.RegistrationID("no.such.procedure")
	require.False(t, ok, "Expected false looking up registration ID for bad procedure")

	// Test calling the procedure.
	callArgs := wamp.List{73}
	ctx := context.Background()
	result, err := caller.Call(ctx, procName, nil, callArgs, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2701, result.Arguments[0])

	// Test unregister.
	err = callee.Unregister(procName)
	require.NoError(t, err)

	// Test calling unregistered procedure.
	callArgs = wamp.List{555}
	result, err = caller.Call(ctx, procName, nil, callArgs, nil, nil)
	require.Error(t, err, "expected error calling unregistered procedure")
	require.Nil(t, result, "result should be nil on error")
	require.ErrorContains(t, err, "wamp.error.no_such_procedure")
	var rpcErr RPCError
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, wamp.ErrNoSuchProcedure, rpcErr.Err.Error, "Wrong error URI in RPC error")

	// ***Testing PPT Mode****

	// Test registering a valid procedure.
	handler = func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		arg, _ := wamp.AsString(inv.Arguments[0])
		require.Equal(t, "hello world", arg, "event missing or bad args")
		kwarg, _ := wamp.AsString(inv.ArgumentsKw["prop"])
		require.Equal(t, "hello world", kwarg, "event missing or bad kwargs")

		resArgs := wamp.List{"goodbye world"}
		resKwargs := wamp.Dict{"prop": "goodbye world"}
		options := wamp.Dict{
			wamp.OptPPTScheme:     inv.Details[wamp.OptPPTScheme],
			wamp.OptPPTSerializer: inv.Details[wamp.OptPPTSerializer],
		}

		return InvokeResult{Args: resArgs, Kwargs: resKwargs, Options: options}
	}
	procName = "test.ppt"
	err = callee.Register(procName, handler, nil)
	require.NoError(t, err)

	// Test calling the procedure with invalid PPT Scheme
	options := wamp.Dict{
		wamp.OptPPTScheme:     "invalid_scheme",
		wamp.OptPPTSerializer: "native",
	}
	args := wamp.List{"hello world"}
	kwargs := wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	require.Error(t, err, "Expected error calling procedure")

	// Test calling the procedure with invalid PPT serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "invalid_serializer",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	require.Error(t, err, "Expected error calling procedure")

	// Test calling the procedure within custom ppt scheme and native serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "native",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	require.NoError(t, err)
	require.Equal(t, "goodbye world", result.Arguments[0])
	require.Equal(t, "goodbye world", result.ArgumentsKw["prop"])

	// Test calling the procedure within custom ppt scheme and json serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "json",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	require.NoError(t, err)
	require.Equal(t, "goodbye world", result.Arguments[0])
	require.Equal(t, "goodbye world", result.ArgumentsKw["prop"])

	// Test calling the procedure within custom ppt scheme and cbor serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "cbor",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	require.NoError(t, err)
	require.Equal(t, "goodbye world", result.Arguments[0])
	require.Equal(t, "goodbye world", result.ArgumentsKw["prop"])

	// Test calling the procedure within custom ppt scheme and msgpack serializer
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "msgpack",
	}
	args = wamp.List{"hello world"}
	kwargs = wamp.Dict{"prop": "hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, options, args, kwargs, nil)
	require.NoError(t, err)
	require.Equal(t, "goodbye world", result.Arguments[0])
	require.Equal(t, "goodbye world", result.ArgumentsKw["prop"])

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
	err = callee.Register(procName, handler, nil)
	require.NoError(t, err, "failed to register procedure")

	// Test calling the procedure with invalid PPT Scheme in YIELD
	args = wamp.List{"hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, nil, args, nil, nil)
	require.Error(t, err, "Expected error calling procedure")

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
	err = callee.Register(procName, handler, nil)
	require.NoError(t, err)

	// Test calling the procedure with invalid PPT Scheme in YIELD
	args = wamp.List{"hello world"}
	ctx = context.Background()
	result, err = caller.Call(ctx, procName, nil, args, nil, nil)
	require.Error(t, err, "Expected error calling procedure")
}

func TestProgressiveCallResults(t *testing.T) {
	// Connect two clients to the same server
	callee, caller, _ := connectedTestClients(t)

	// Handler sends progressive results.
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		// Creating another child context to check that progressive results
		// work as expected even with another child context passed.
		ctx = context.WithValue(ctx, "Some-key", true)
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
	err := callee.Register(procName, handler, nil)
	require.NoError(t, err)

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
	require.NoError(t, err)
	sum, ok := wamp.AsInt64(result.Arguments[0])
	require.True(t, ok, "Could not convert result to int64")
	require.Equal(t, int64(55), sum)
	require.Equal(t, 3, progCount)

	// Test unregister.
	err = callee.Unregister(procName)
	require.NoError(t, err)
}

func TestProgressiveCallInvocations(t *testing.T) {
	// Connect two clients to the same server
	callee, caller, _ := connectedTestClients(t)

	var progressiveIncPayload []int
	var mu sync.Mutex

	invocationHandler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		mu.Lock()
		progressiveIncPayload = append(progressiveIncPayload, inv.Arguments[0].(int))
		mu.Unlock()

		if isInProgress, _ := inv.Details[wamp.OptProgress].(bool); !isInProgress {
			var sum int64
			for _, arg := range progressiveIncPayload {
				n, ok := wamp.AsInt64(arg)
				if ok {
					sum += n
				}
			}
			return InvokeResult{Args: wamp.List{sum}}
		}

		return InvokeResult{Err: wamp.InternalProgressiveOmitResult}
	}

	procName := "nexus.test.progproc"

	// Register procedure
	err := callee.Register(procName, invocationHandler, nil)
	require.NoError(t, err)

	// Test calling the procedure.
	callArgs := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ctx := context.Background()

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

	result, err := caller.CallProgressive(ctx, procName, sendProgDataCb, nil)
	require.NoError(t, err)
	sum, ok := wamp.AsInt64(result.Arguments[0])
	require.True(t, ok, "Could not convert result to int64")
	require.Equal(t, int64(55), sum)
	require.Equal(t, 10, callSends)

	require.Equal(t, progressiveIncPayload, callArgs, "Progressive data should be processed sequentially")

	// Test unregister.
	err = callee.Unregister(procName)
	require.NoError(t, err)
}

func TestProgressiveCallsAndResults(t *testing.T) {
	// Connect two clients to the same server
	callee, caller, _ := connectedTestClients(t)

	// Handler sends progressive results.
	invocationHandler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		if isInProgress, _ := inv.Details[wamp.OptProgress].(bool); !isInProgress {
			return InvokeResult{Args: inv.Arguments}
		}
		senderr := callee.SendProgress(ctx, inv.Arguments, nil)
		if senderr != nil {
			callee.log.Println("Error sending progress:", senderr)
			return InvokeResult{Err: "test.failed"}
		}
		return InvokeResult{Err: wamp.InternalProgressiveOmitResult}
	}

	procName := "nexus.test.progproc"

	// Register procedure
	err := callee.Register(procName, invocationHandler, nil)
	require.NoError(t, err)

	// Test calling the procedure.
	callArgs := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	ctx := context.Background()

	var progressiveResults []int
	var mu sync.Mutex

	progRescb := func(result *wamp.Result) {
		mu.Lock()
		defer mu.Unlock()
		progressiveResults = append(progressiveResults, result.Arguments[0].(int))
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

	result, err := caller.CallProgressive(ctx, procName, sendProgDataCb, progRescb)
	require.NoError(t, err)
	progressiveResults = append(progressiveResults, result.Arguments[0].(int))
	var sum int64
	for _, arg := range progressiveResults {
		n, ok := wamp.AsInt64(arg)
		if ok {
			sum += n
		}
	}
	require.Equal(t, int64(55), sum)
	require.Equal(t, 10, callSends)

	require.Equal(t, progressiveResults, callArgs, "Progressive data should be processed sequentially")

	// Test unregister.
	err = callee.Unregister(procName)
	require.NoError(t, err)
}

func TestTimeoutCancelRemoteProcedureCall(t *testing.T) {
	checkGoLeaks(t)

	// Connect two clients to the same server
	callee, caller, _ := connectedTestClients(t)

	// Test registering a valid procedure.
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		return InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	err := callee.Register(procName, handler, nil)
	require.NoError(t, err)

	err = caller.SetCallCancelMode(wamp.CancelModeKillNoWait)
	require.NoError(t, err)
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
	case <-errChan:
		require.FailNow(t, "call should have been blocked")
	case <-time.After(200 * time.Millisecond):
	}

	// Make sure the call is canceled.
	select {
	case err = <-errChan:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "call should have been canceled")
	}

	err = callee.Unregister(procName)
	require.NoError(t, err)
}

func TestCancelRemoteProcedureCall(t *testing.T) {
	// Connect two clients to the same server
	callee, caller, _ := connectedTestClients(t)

	// Test registering a valid procedure.
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		return InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	err := callee.Register(procName, handler, nil)
	require.NoError(t, err)

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
	case <-errChan:
		require.FailNow(t, "call should have been blocked")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()

	// Make sure the call is canceled.
	select {
	case err = <-errChan:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		require.FailNow(t, "call should have been canceled")
	}

	err = callee.Unregister(procName)
	require.NoError(t, err)
}

func TestTimeoutRemoteProcedureCall(t *testing.T) {
	checkGoLeaks(t)

	// Connect two clients to the same server
	callee, caller, _ := connectedTestClients(t)

	// Test registering a valid procedure.
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		<-ctx.Done() // handler will block forever until canceled.
		return InvokeResult{Err: wamp.ErrCanceled}
	}
	procName := "myproc"
	err := callee.Register(procName, handler, nil)
	require.NoError(t, err)

	err = callee.Register("bad proc! no no", handler, nil)
	require.Error(t, err, "Expected error registering with bad procedure name")

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
	case <-errChan:
		require.FailNow(t, "call should have been blocked")
	case <-time.After(200 * time.Millisecond):
	}

	// Make sure the call is canceled.
	select {
	case err = <-errChan:
		var rpcError RPCError
		require.ErrorAs(t, err, &rpcError)
		require.Equal(t, wamp.ErrTimeout, rpcError.Err.Error)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "call should have been canceled")
	}

	err = callee.Unregister(procName)
	require.NoError(t, err)
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
	require.Error(t, err)
	resStrMatch, _ := regexp.MatchString(expect, err.Error())
	require.True(t, resStrMatch)

	_, err = ConnectNet(ctx, "https://localhost:9999/ws", cfg)
	require.Error(t, err)
	resStrMatch, _ = regexp.MatchString(expect, err.Error())
	require.True(t, resStrMatch)

	_, err = ConnectNet(ctx, "tcp://localhost:9999", cfg)
	require.Error(t, err)
	resStrMatch, _ = regexp.MatchString(expect, err.Error())
	require.True(t, resStrMatch)

	_, err = ConnectNet(ctx, "tcps://localhost:9999", cfg)
	require.Error(t, err)
	resStrMatch, _ = regexp.MatchString(expect, err.Error())
	require.True(t, resStrMatch)

	_, err = ConnectNet(ctx, "unix:///tmp/wamp.sock", cfg)
	require.Error(t, err)
	require.EqualError(t, err, unixExpect)
}

func createTestServer(t *testing.T) (router.Router, io.Closer) {
	realmConfig := &router.RealmConfig{
		URI:            wamp.URI(testRealm),
		StrictURI:      true,
		AnonymousAuth:  true,
		AllowDisclose:  true,
		EnableMetaKill: true,
	}
	r := getTestRouter(t, realmConfig)

	// Create and run server.
	closer, err := router.NewWebsocketServer(r).ListenAndServe(testAddress)
	require.NoError(t, err)
	t.Cleanup(func() {
		closer.Close()
	})
	log.Printf("Websocket server listening on ws://%s/", testAddress)

	return r, closer
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

func newNetTestCalleeWithConfig(t *testing.T, routerURL string, clientConfig *Config) *Client {
	cl, err := ConnectNet(context.Background(), routerURL, *clientConfig)
	require.NoError(t, err)
	t.Cleanup(func() {
		cl.Close()
	})

	sleep := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		logger.Println("sleep rpc start")
		select {
		case <-ctx.Done():
			return InvocationCanceled
		case <-time.After(5 * time.Second):
		}
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
	return cl
}

func newNetTestCallee(t *testing.T, routerURL string) *Client {
	return newNetTestCalleeWithConfig(
		t,
		routerURL,
		newNetTestClientConfig(testRealm, log.New(os.Stderr, "CALLEE> ", log.Lmicroseconds)),
	)
}

func newNetTestKillerWithConfig(t *testing.T, routerURL string, clientConfig *Config) *Client {
	logger := clientConfig.Logger
	cl, err := ConnectNet(context.Background(), routerURL, *clientConfig)
	require.NoError(t, err)
	t.Cleanup(func() {
		cl.Close()
	})

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
	require.NoError(t, err)
	logger.Println("Subscribed to", wamp.MetaEventSessionOnJoin)

	return cl
}

func newNetTestKiller(t *testing.T, routerURL string) *Client {
	return newNetTestKillerWithConfig(
		t,
		routerURL,
		newNetTestClientConfig(testRealm, log.New(os.Stderr, "KILLER> ", log.Lmicroseconds)),
	)
}

// Test for races in client when session is killed by router.
func TestClientRace(t *testing.T) {
	// Create a websocket server
	_, _ = createTestServer(t)

	testUrl := fmt.Sprintf("ws://%s/ws", testAddress)
	_ = newNetTestKiller(t, testUrl)
	logger.Println("Starting callee")

	_ = newNetTestCallee(t, testUrl)

	// If we hit a race condition with the client register, we do not ever return from the Register()
	logger.Println("Finished test - cleanup")
}

// Test that if the router disconnects the client, while the client is running
// an invocation handler, that the handler still gets marked as done when it
// completes.
func TestInvocationHandlerMissedDone(t *testing.T) {
	checkGoLeaks(t)

	// Connect two clients to the same server
	callee, caller, _ := connectedTestClients(t)

	calledChan := make(chan struct{})

	// Register procedure.
	handler := func(ctx context.Context, inv *wamp.Invocation) InvokeResult {
		close(calledChan)
		<-ctx.Done()
		return InvokeResult{Args: wamp.List{inv.Arguments[0].(int) * 37}}
	}
	procName := "myproc"
	err := callee.Register(procName, handler, nil)
	require.NoError(t, err)

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
		require.FailNow(t, "Timed out waiting to close client")
	}
}

func TestProgressDisconnect(t *testing.T) {
	checkGoLeaks(t)

	// Create a websocket server
	r, closer := createTestServer(t)

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
	require.NoError(t, err)
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
	err = callee.Register(chunkProc, handler, nil)
	require.NoError(t, err)

	// Connect caller session.
	cfg.Logger = log.New(os.Stderr, "CALLER> ", log.Lmicroseconds)
	caller, err := ConnectNet(context.Background(), testURL, cfg)
	require.NoError(t, err)
	defer caller.Close()

	progHandler := func(_ *wamp.Result) {
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
	require.ErrorIs(t, err, ErrNotConn)

	// Check for expected error from callee.
	err = <-sendProgErr
	require.Error(t, err)
	if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrNotConn) {
		require.FailNowf(t, "wrong error from SendProgress: %s", err.Error())
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
	checkGoLeaks(t)

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
	router1 := getTestRouter(t, realm1Config)
	router2 := getTestRouter(t, realm2Config)

	// create local clients to each realm
	client1 := newTestClientWithConfig(t, router1, client1Config)
	require.NotEqual(t, wamp.ID(0), client1.ID(), "Expected non-0 client id")
	client2 := newTestClientWithConfig(t, router2, client2Config)
	require.NotEqual(t, wamp.ID(0), client2.ID(), "Expected non-0 client id")

	// subscribe to topics in each realm and test for leak
	err := client1.Subscribe(testTopic, func(*wamp.Event) {}, nil)
	require.NoError(t, err)
	err = client2.Subscribe(testTopic2, func(*wamp.Event) {}, nil)
	require.NoError(t, err)
}

func TestEventContentSafety(t *testing.T) {
	checkGoLeaks(t)

	// Connect two subscribers and one publisher to router
	sub1, sub2, r := connectedTestClients(t)
	pub := newTestClient(t, r)

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
	err := sub1.Subscribe(testTopic, eventHandler, nil)
	require.NoError(t, err)
	err = sub2.Subscribe(testTopic, eventHandler, nil)
	require.NoError(t, err)

	err = pub.Publish(testTopic, nil, wamp.List{"Hello"}, wamp.Dict{"prop": "value"})
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		select {
		case err = <-errChan:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			require.FailNow(t, "did not get published event")
		}
	}
}

func TestRouterFeatures(t *testing.T) {
	realmConfig := &router.RealmConfig{
		URI:           wamp.URI("nexus.test.auth"),
		StrictURI:     true,
		AnonymousAuth: true,
	}
	r := getTestRouter(t, realmConfig)

	routerFeatures := r.RouterFeatures()

	dealerFeatures := (*routerFeatures)["roles"].(wamp.Dict)[wamp.RoleDealer].(wamp.Dict)
	require.GreaterOrEqual(t, len(dealerFeatures), 1, "dealer features are missed")

	brokerFeatures := (*routerFeatures)["roles"].(wamp.Dict)[wamp.RoleBroker].(wamp.Dict)
	require.GreaterOrEqual(t, len(brokerFeatures), 1, "broker features are missed")
}
