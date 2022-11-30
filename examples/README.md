# Nexus Examples

## Simple Example

The `simple` example contains a very simple websocket server, and simple websocket subscriber and publisher clients.
This is a good place to start seeing how to build WAMP servers and clients using nexus.

The simple examples can be run from the `examples` directory by running:

1. Run the server with `go run simple/server.go`
2. Run the subscriber with `go run simple/sub/subscriber.go`
3. Run the publisher with `go run simple/pub/publisher.go`

## Example Server and Clients

The project Wiki provides a walk-through of [client](https://github.com/gammazero/nexus/wiki/Client-Library) and
[server](https://github.com/gammazero/nexus/wiki/Router-Library) example code.

The example server, in the `server` directory, runs a websocket (with and without TLS), tcp raw socket
(with and without TLS), and Unix raw socket transport at the same time.  This allows different clients to
communicate with each other when connected to the server using any combination of socket types, TLS, and
serialization schemes.

The example clients are located in the following locations:

- `pubsub/subscriber/`
- `pubsub/publisher/`
- `rpc/callee/`
- `rpc/caller/`
- `session_meta_api/`

When connecting a client, set the URL scheme with `-scheme=` to specify the type of transport, and
whether to use TLS:

- Websocket: `-scheme=ws`
- Websocket + TLS: `-scheme=wss`
- TCP socket: `-scheme=tcp`
- TCP socket + TLS: `-scheme=tcps`
- Unix socket: `-socket=unix`

If no scheme is specified, then the default is `ws` (websocket without TLS).

When using TLS ("wss" or "tcps" schemes), certificate verification fails when using a certificate that cannot
be verified.  For verification of the server's certificate to work, it is necessary to trust the server's
certificate by specifying `-trust=server/cert.pem`.  Verification can also be skipped using the
`-skipverify` flag.  Example running subscriber client:

```
go run pubsub/subscriber/subscriber.go -scheme=wss -trust=server/cert.pem
```

To choose the type of serialization for the client to use, specify `-serialize=json` or `-serialize=msgpack`.
If no serialization is specified, then the default is `json`.

NOTE: The certificate and key used by the example server were created using the following commands:

```
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 3650
openssl rsa -in key.pem -out rsakey.pem
```

See `-help` option for additional options available for server and clients.

## RPC Example

The RPC example provides two callee clients.  One callee client is embedded in the server running the WAMP
router, and the other is an external client.  The internal client does not require a socket or serialization
and is run as part of the server.  The external client connects to the router using a socket.

The external caller client makes calls to both the internal and the external callee clients.

### Caller and Callee Both External Clients

1. Run the server with `go run server/server.go`
2. Run the callee with `go run rpc/callee/callee.go`
3. Run the caller with `go run rpc/caller/caller.go`

## Pub/Sub Example

The pub/sub example provides a subscriber client and a publisher client that connect to the nexus server to
demonstrate simple pub/sub messaging.

This pub/sub example does not have an internal client embedded in the router, as creating an embedded client
is demonstrated by the internal RPC client.

### Run the Subscriber and Publisher Clients

1. Run the server with `go run server/server.go`
2. Run the subscriber with `go run pubsub/subscriber/subscriber.go`
3. Run the publisher with `go run pubsub/publisher/publisher.go`

## Session Meta API Example

The session meta API example provides a client that subscribes to session meta events and calls session meta
procedures to demonstrate the session meta API.

### Run the Session Meta Client Example

1. Run the server with `go run server/server.go`
2. Run the client with `go run session_meta_api/session_meta_client.go`

## Multiple Transport Example

A nexus router is capable of routing messages between clients running with different transports and
serializations.  To see this, you can run the example nexus server and then connect clients that use different
combinations of websockets and raw sockets, and JSON and MsgPack serialization.

### Run Websocket Subscriber with TCP and Unix Raw Socket Publishers

1. Run the server with `go run server/server.go`
2. Run the subscriber with `go run pubsub/subscriber/subscriber.go`
3. Run the publisher with `go run pubsub/publisher/publisher.go -scheme=tcp`
4. Run the publisher with `go run pubsub/publisher/publisher.go -scheme=unix`

Try different combinations socket type, TLS, and serialization with multiple clients.  See `-help` for options.

## Payload PassThru Mode Examples

Nexus supports [Payload PassThru Mode](https://wamp-proto.org/wamp_latest_ietf.html#name-payload-passthru-mode)
(`ppt` for short) in all roles. Check and run examples in [ppt](ppt) folder.

1. Run the server with `go run server/server.go`
2. Run the subscriber with `go run ppt/subscriber/subscriber.go`
3. Run the publisher with `go run ppt/publisher/publisher.go`
4. Run the callee with `go run ppt/callee/callee.go`
5. Run the caller with `go run ppt/caller/caller.go`

## Progressive calls (and progressive results)

Nexus supports [Progressive Calls](https://wamp-proto.org/wamp_latest_ietf.html#name-progressive-calls)
dealer feature. Check and run examples in [rpc_progressive_calls](./rpc_progressive_calls) and
[rpc_progress_calls_results](./rpc_progress_calls_results) folders.

Please note that invocation handler is invoked with payload chunks in the same order they are received through the wire
even that it is running asynchronously. Also `ctx context.Context` parameter of invocation handler is the context of
the whole invocation starting with 1st call/invocation and lasts until final result. But if the client is
specifying `timeout` option with every progressive call callback, then timeouted context is updated and
cancel deadline is pushed forward in timeline. This can help processing progressive calls for slow clients.
To use timeouted context for the whole call - do not specify `timeout` option within intermediate data chunks.

1. Run the server with `go run server/server.go`
2. Run the callee which accumulates progressive call data chunks with `go run rpc_progressive_calls/callee/callee.go`
3. Run the caller which makes progressive call and sends data chunks with `go run rpc_progressive_calls/caller/caller.go`
4. Run the callee which is aware of progressive call data chunks and at same time sends
   progressive results with `go run rpc_progress_calls_results/callee/callee.go`
5. Run the caller which makes progressive call and sends data chunks and at the same time
   receives progress results with `go run rpc_progress_calls_results/caller/caller.go`
