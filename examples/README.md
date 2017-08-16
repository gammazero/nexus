# Nexus Examples

## RPC Example

The RPC example provides a callee client as both an external and internal client.  An internal client is one that is embedded in the same process as the WAMP router.

### Caller and Callee Both External Clients

1. Run the server with `go run server/server.go`
2. Run the callee with `go run rpc/client_callee/callee.go`
3. Run the caller with `go run rpc/client_caller/caller.go`

### Server with Internal Callee and External Caller.

1. Run the server with `go run rpc/server_embedded_callee/server.go`
2. Run the caller with `go run rpc/client_caller/caller.go`

## Pub/Sub Example

The pub/sub example provides a subscriber client and a publisher client that connect to the nexus server to demonstrate simple pub/sub messaging.

### Run the Subscriber and Publisher Clients

1. Run the server with `go run server/server.go`
2. Run the subscriber with `go run pubsub/subscriber/subscriber.go`
3. Run the publisher with `go run pubsub/publisher/publisher.go`
