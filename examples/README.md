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
