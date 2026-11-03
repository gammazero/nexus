Router API: ![GoDoc](https://pkg.go.dev/badge/github.com/gammazero/nexus/v3/router)](https://pkg.go.dev/github.com/gammazero/nexus/v3/router)

# Creating a WAMP router with nexus

Here is a router that serves websocket clients: [`examples/simple/server.go`](https://github.com/gammazero/nexus/blob/main/examples/simple/server.go)

Let's walk through the code inside `func main() { ... }`

## Configure and Create Router

The first thing the code does is to create a configuration for the router.
```go
// Create router instance.
routerConfig := &router.Config{
    RealmConfigs: []*router.RealmConfig{
        &router.RealmConfig{
            URI:           wamp.URI(realm),
            AnonymousAuth: true,
        },
    },
}
nxr, err := router.NewRouter(routerConfig, nil)
if err != nil {
    log.Fatal(err)
}
defer nxr.Close()
```

This `routerConfig` tells the router to create a realm known by the URI "nexus.examples", and to allow anonymous authentication.  This is a hard-coded simple example.  The router configuration can also be read from a file as is done with nexusd and its [configuration file](https://github.com/gammazero/nexus/blob/main/cmd/nexusd/etc/nexus.json#L14-L24).

Then the configuration is passed to [`nexus.NewRouter()`](https://pkg.go.dev/github.com/gammazero/nexus/v3/router#NewRouter) to create an instance of the router.  The second argument passed to the router is any logger instance that implements the [StdLog](https://pkg.go.dev/github.com/gammazero/nexus/v3/stdlog#StdLog) interface.  If `nil`, then the router logs to stdout.  After checking for errors, the `defer nxr.Close()` closes the router at function exit.

## Run Server

The next piece of code creates a server instance and runs the server. This means the server will listen for connections on the interface/port specified by the address parameter. 

```go
// Create and run server.
closer, err := router.NewWebsocketServer(nxr).ListenAndServe(address)
if err != nil {
    log.Fatal(err)
}
```

`NewWebsocketServer` takes a router instance and creates a new websocket server. To run the websocket server, call one of the server's ListenAndServe methods:

- [`ListenAndServe`](https://pkg.go.dev/github.com/gammazero/nexus/v3/router#WebsocketServer.ListenAndServe)
- [`ListenAndServeTLS`](https://pkg.go.dev/github.com/gammazero/nexus/v3/router#WebsocketServer.ListenAndServeTLS)

Or, use the various [`ListenAndServe`](https://golang.org/pkg/net/http/#ListenAndServe) functions provided by `net/http`. This works because WebsocketServer implements the [`http.Handler`](https://golang.org/pkg/net/http/#Handler) interface.  For example:

```go
s := NewWebsocketServer(r)
server := &http.Server{
    Handler: s,
    Addr:    address,
}
server.ListenAndServe()
```

## Wait for the Shutdown and Exit

The last section of code only waits to receive a shutdown signal, CTRL-c in this example, and then gracefully exits.
```go
// Wait for SIGINT (CTRL-c), then close server and exit.
shutdown := make(chan os.Signal, 1)
signal.Notify(shutdown, os.Interrupt)
<-shutdown
closer.Close()
```

Calling `closer.Close()` causes the server to stop accepting connections on its listening socket, and to exit the goroutine that accepts the connections.  This will not affect any connections that are already established. Only when the _router_ is closed are the established connections are closed and their associated goroutines exited.

## Handling Additional Socket Types

Here is an example nexus WAMP router that handles websockets, TCP rawsockets, and unix rawsockets.  It is very similar to the simple websocket server above, and shows how easy it is to serve different types of sockets.

https://github.com/gammazero/nexus/blob/main/examples/server/server.go
