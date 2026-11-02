// Package auth provides interfaces for providing authentication logic
// implementations to the WAMP router.
//
// In addition to the authentication interfaces, this package provides a
// default implementations for the following authentication methods: "wampcra"
// for [challenge response authentication], "ticket" for [ticket-based
// authentication], "cryptosign" for [cryptosign-based Authentication], and
// "anonymous" to allow anonymous clients.
//
// # Cookie and WebSocket Request Authentication
//
// The authenticators have access to the HTTP request and tracking/auth cookies
// when these are enabled in the router configuration. This allows a router
// implementation to use cookie-based authentication, client tracking
// (generally for remembering previously authenticated clients), and
// information from WebSocket requests for making authentication decisions.
//
// Tracking cookie data is included in the HELLO details from the client as
// `Details.transport.auth.cookie`. WebSocket HTTP Request is included with the
// HELLO details, as `Details.transport.auth.request`. A [BypassKeyStore]
// implementation is provided for authenticators to check client cookie data to
// see if the client is already authenticated.
//
// To enable the use of client tracking cookies WebSocket request data and , see
// the documentation for the WebSocket server.
//
// [challenge response authentication]: https://wamp-proto.org/wamp_latest_ietf.html#name-challenge-response-authenti
// [ticket-based authentication]: https://wamp-proto.org/wamp_latest_ietf.html#name-ticket-based-authentication
// [cryptosign-based Authentication]: https://wamp-proto.org/wamp_latest_ietf.html#name-cryptosign-based-authentica
package auth
