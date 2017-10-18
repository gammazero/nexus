# Nexus Examples

## Simple Example

The `simple` example contains a very simple websocket server, and simple websocket subscriber and publisher clients.  This is a good place to start seeing how to build WAMP servers and clients using nexus.

The simple examples can be run from the `examples` directory by running:

1. Run the server with `go run simple/server.go`
2. Run the subscriber with `go run simple/sub/subscriber.go`
3. Run the publisher with `go run simple/pub/publisher.go`

## Example Server and Clients

The project Wiki provides a walk-through of [client](https://github.com/gammazero/nexus/wiki/Client-Library) and [server](https://github.com/gammazero/nexus/wiki/Router-Library) example code.

The example server, in the `server` directory, runs a websocket (with and without TLS), tcp raw socket (with and without TLS), and unix raw socket transport at the same time.  This allows different clients to communicate with each other when connected to the server using any combination of socket types, TLS, and serialization schemes.

The example clients are located in the following locations:

- `pubsub/subscriber/`
- `pubsub/publisher/`
- `rpc/callee/`
- `rpc/caller/`

Set the URL scheme with `-scheme=` to specify the type of transport, and whether or not to use TLS, when connecting a client:

- Websocket: `-scheme=ws`
- Websocket + TLS: `-scheme=wss`
- TCP socket: `-scheme=tcp`
- TCP socket + TLS: `-scheme=tcps`
- Unix socket: `-socket=unix`

If no scheme is specified, then the default is `ws` (websocket without TLS).

When using TLS ("wss" or "tcps" schemes) specifying the `-skipverify` flag.  The `-skipverify` flag is needed to skip verification of the certificate presented by the example server.  The flag can be omitted if using a certificate the can be verified or to check that verification fails with an invalid certificate.

To choose the type of serialization for the client to use, specify `-serialize=json` or `-serialize=msgpack`.  If no serialization is specified, then the default is `json`.

NOTE: The certificate and key used by the example server were created using the following commands:
```
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 3650
openssl rsa -in key.pem -out rsakey.pem
```

## RPC Example

The RPC example provides a callee client as both an external and internal client.  An internal client is one that is embedded in the same process as the WAMP router, and does not require a socket or serialization.

### Caller and Callee Both External Clients

1. Run the server with `go run server/server.go`
2. Run the callee with `go run rpc/callee/callee.go`
3. Run the caller with `go run rpc/caller/caller.go`

### Server with Internal Callee and External Caller.

1. Run the server with `go run rpc/server_embedded_callee/server.go`
2. Run the caller with `go run rpc/caller/caller.go`

This server example only runs a websocket transport.

## Pub/Sub Example

The pub/sub example provides a subscriber client and a publisher client that connect to the nexus server to demonstrate simple pub/sub messaging.

### Run the Subscriber and Publisher Clients

1. Run the server with `go run server/server.go`
2. Run the subscriber with `go run pubsub/subscriber/subscriber.go`
3. Run the publisher with `go run pubsub/publisher/publisher.go`

## Multiple Transport Example

A nexus router is capable of routing messages between clients running with different transports and serializations.  To see this, you can run the example nexus server and then connect clients that use different combinations of websockets and raw sockets, and JSON and MsgPack serialization.

### Run Websocket Subscriber with TCP and Unix Raw Socket Publishers

1. Run the server with `go run server/server.go`
2. Run the subscriber with `go run pubsub/subscriber/subscriber.go`
3. Run a publisher with `go run pubsub/publisher/publisher.go -socket=tcp`
4. Run a publisher with `go run pubsub/publisher/publisher.go -socket=unix`

Try different combinations socket type, TLS, and serialization with multiple clients.
