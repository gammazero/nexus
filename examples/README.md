# Nexus Examples

## Simple Example

The `simple` example contains a very simple websocket server, and simple websocket subscriber and publisher clients.  This is a good place to start seeing how to build WAMP servers and clients using nexus.

The simple examples can be run from the `examples` directory by running:

1. Run the server with `go run simple/server.go`
2. Run the subscriber with `go run simple/sub/subscriber.go`
3. Run the publisher with `go run simple/pub/publisher.go`

## RPC Example

The RPC example provides a callee client as both an external and internal client.  An internal client is one that is embedded in the same process as the WAMP router.

### Caller and Callee Both External Clients

1. Run the server with `go run server/server.go`
2. Run the callee with `go run rpc/callee/callee.go`
3. Run the caller with `go run rpc/caller/caller.go`

The server runs a websocket, raw tcp socket, and raw unix socket transport at the same time.

To connect a client to the different types of transport, specify `-type=websocket`, `-type=rawtcp`, `-type=rawunix`.  If no type is specified, then the client uses a websocket transport.

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

To connect a client to the different types of transport, specify `-type=websocket`, `-type=rawtcp`, `-type=rawunix`.  If no type is specified, then the client uses a websocket transport.
