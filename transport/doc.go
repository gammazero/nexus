// Package transport provides a WebSocket, RawSocket, and local transport.
//
// Clients using any of these transports can communicate through a nexus router
// to other clients using any of these transports. Each transport implements
// the wamp.Peer interface, that connects Send and Recv methods to that
// transport.
//
// # WebSocket Transport
//
// Nexus uses the [Gorilla WebSocket] package as its WebSocket implementation.
//
// # RawSocket Transport
//
// The RawSocket transport works over TCP and Unix sockets. Peers can choose to
// limit message sizes, with the default being 16Mb.
//
// # Local Transport
//
// The local transport is used to connect clients to the router when the
// clients are embedded in the same application as the router. This eliminates
// the need for any socket and serialization overhead, and does not require
// authentication.
//
// # Serialization and TLS
//
// The WebSocket and RawSocket transports can use [TLS] and any of the
// available serializations, which are [JSON], [MessagePack], and [CBOR]. The
// local transport does not use TLS or serialization.
//
// [Gorilla WebSocket]: https://github.com/gorilla/websocket#gorilla-websocket
// [JSON]: https://en.wikipedia.org/wiki/JSON
// [MessagePack]: https://msgpack.org/index.html
// [CBOR]: https://www.rfc-editor.org/info/rfc8949/
package transport
