Client API: [![GoDoc](https://pkg.go.dev/badge/github.com/gammazero/nexus/v3/client)](https://pkg.go.dev/github.com/gammazero/nexus/v3/client)

- [Publish](https://pkg.go.dev/github.com/gammazero/nexus/v3/client#Client.Publish) API
- [Register](https://pkg.go.dev/github.com/gammazero/nexus/v3/client#Client.Register) API
- [Call](https://pkg.go.dev/github.com/gammazero/nexus/v3/client#Client.Call) API

# Creating an Example Subscriber Client

This [example client](https://github.com/gammazero/nexus/blob/main/examples/simple/sub/subscriber.go) that subscribes to receive events on a topic.

Let's walk through the code inside `func main() { ... }`

## Configuring the Client

The client configuration in this example is very simple.  It only specifies the realm that the client is to join when connected to the router and provides a logger for the client to use.  The logger is any logger instance that implements the [StdLog](https://pkg.go.dev/github.com/gammazero/nexus/v3/stdlog#StdLog) interface.  If not provided, the client logs to `os.Stderr`.  

```go
logger := log.New(os.Stdout, "", 0)
cfg := client.Config{
    Realm:  "nexus.examples",
    Logger: logger,
}
```

The `cfg.Serialization` field specifies the serialization, `client.JSON` or `client.MSGPACK`, for the client to use.  The default is `client.JSON` which is what will be used here.  The [`client.Config`](https://pkg.go.dev/github.com/gammazero/nexus/v3/client#Config) allows configuration of other transport and client options. 

It is not necessary to define the client roles and features.  Role and feature announcement is already taken care of by the nexus client when it joins the realm.  It announces all the roles and features it supports.  However, if you want your client to announce a subset of the roles and features, you can provide your own [dictionary](https://pkg.go.dev/github.com/gammazero/nexus/v3/wamp#Dict) of roles and features in the `HelloDetails` section of the [`Config`](https://pkg.go.dev/github.com/gammazero/nexus/v3/client#Config).

## Create Client, Connect to Router and Join Realm

The next section of code calls [`ConnectNet`](https://pkg.go.dev/github.com/gammazero/nexus/v3/client#ConnectNet) to create the websocket client instance, connect it to the server/router, and join the realm specified in the `client.Config`.

```go
// Connect subscriber session.
subscriber, err := client.ConnectNet("http://localhost:8000/", cfg)
if err != nil {
    logger.Fatal(err)
}
defer subscriber.Close()
```

### URL Schemes

A websocket server endpoint is identified by a URL of the form `"scheme://address"`, given as the first argument to `ConnectNet`. Possible schemes and their meaning are as follows:

- `http` or `ws`: Websocket
- `https` or `wss`: Websocket with TLS
- `tcp`: TCP socket
- `tcps`: TCP socket with TLS
- `unix`: Unix socket

For details on URL format, see [`ConnectNet`](https://pkg.go.dev/github.com/gammazero/nexus/v3/client#ConnectNet) API reference.  Also note that a [`ConnectLocal`](https://pkg.go.dev/github.com/gammazero/nexus/v3/client#ConnectLocal) API is available for clients that are embedded in the same application as the router.

After checking for errors, the `defer subscriber.Close()` sets the client to be closed at function exit.

## Define Event Handler and Subscribe to Topic

[Subscribe](https://pkg.go.dev/github.com/gammazero/nexus/v3/client#Client.Subscribe) API

When a client subscribes to a topic, it is registering its interest in having the router send it any events that are published by other clients for that topic.  Subscribing to a topic, using the nexus client, configures the client to automatically call an event handler call-back function whenever the client receives an event from the router. 

The next section of code defines an event handler call-back function and specifies that function when subscribing to a topic. 

```go
// Define function to handle events received.
evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
    logger.Println("Received", exampleTopic, "event")
    if len(args) != 0 {
        logger.Println("  Event Message:", args[0])
    }
}

// Subscribe to topic.
err = subscriber.Subscribe(exampleTopic, evtHandler, nil)
if err != nil {
    logger.Fatal("subscribe error:", err)
}
logger.Println("Subscribed to", exampleTopic)
```

The event handler receives the message portion of an [`Event`](https://pkg.go.dev/github.com/gammazero/nexus/v3/wamp#Event) message.  The sample here only prints that the event was received and the first item in the `args` list if any.  Event handlers are called serially so that they execute in the same order as the event messages are received in.  This could not be guaranteed if executing concurrently.  However, if order is not important in your implementation, you can process events concurrently by starting a goroutine in your handler implementation to do the processing work.

The call to [`Subscribe`](https://godoc.org/github.com/gammazero/nexus/v3/client#Client.Subscribe) subscribes the client to the specified topic or topic pattern.  The first argument to `Subscribe` specifies an exact event URI to match or it can specify a URI pattern to match multiple events for the same handler.  The second argument specifies the event handler function that is registered to be called every time an event is received for the topic.  The last argument is and optional [dictionary](https://pkg.go.dev/github.com/gammazero/nexus/v3/wamp#Dict) that specifies options for the subscribe message.

To subscribe to topics matching a URI wildcard pattern, specify `wamp.Dict{wamp.OptMatch: wamp.MatchWildcard}` as the value of the last argument.  The option value `wamp.MatchPrefix` is also available.  The default behavior is to match exactly.

## Wait for the Shutdown while Handling Events

The last section of code waits to receive a shutdown signal, CTRL-c in this example, or for the router connection to close, and then gracefully exits.  While waiting, the event handler is called each time the client receives an event on the topic it subscribed to.

```go
// Wait for CTRL-c or client close while handling events.
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt)
select {
case <-sigChan:
case <-subscriber.Done():
    logger.Print("Router gone, exiting")
    return // router gone, just exit
}

// Unsubscribe from topic.
if err = subscriber.Unsubscribe(exampleTopic); err != nil {
    logger.Println("Failed to unsubscribe:", err)
}
```

Since the client is exiting, calling `Unsubscribe` it not necessary since the router will remove all the client's subscriptions as soon as the client goes away.  The `Unsubscribe` is included here as an example of how to unsubscribe from a topic.  The graceful exit is accomplished by calling [`Close()`](https://pkg.go.dev/github.com/gammazero/nexus/v3/client#Client.Close) on the client, which was done using `defer subscriber.Close()` in the first part of the example.
