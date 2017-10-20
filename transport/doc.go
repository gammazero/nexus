/*
Package transport provides a websocket, rawsocket, and local transport
implementation.  The local transport is for in-process connection of a client
to a router.  Each transport implements the wamp.Peer interface, that connect
Send and Recv methods to a particular transport.

*/
package transport
