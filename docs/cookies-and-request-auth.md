# Cookie and WebSocket Request Authentication and Authorization

The nexus router can be configured to supply client tracking cookies and to expose these and the websocket HTTP request to authentication and authorization logic.

## Client Tracking Cookies

The [`WebsocketServer`](https://pkg.go.dev/github.com/gammazero/nexus/v3/router#WebsocketServer) can be configured to enable client tracking cookies.
This causes the server to send a random-value cookie to each WebSocket client in the response to its upgrade request.
The router saves the cookie it sent in the client's HELLO details, as `Details.transport.auth.nextcookie`, to identify the returning client.

The returning WebSocket client may identify itself by sending the previously issued tracking cookie in a WebSocket request.
If a request header sent to the server contains the tracking cookie, then the cookie is included in the HELLO details from the client as `Details.transport.auth.cookie`.
This is used for identifying clients that have already been authenticated, so that the returning client may skip re-authenticating itself.

This functionality is available in the [CRAuthenticator](https://pkg.go.dev/github.com/gammazero/nexus/v3/router/auth#CRAuthenticator) implementation included with the nexus router `auth` package, when enabled for the router.
This is typically used by providing a [`BypassKeyStore`](https://pkg.go.dev/github.com/gammazero/nexus/v3/router/auth#BypassKeyStore) implementation, where the `AlreadyAuth` function returns `true` if a client is recognized as already authenticated, allowing the challenge-response authentication to be bypassed.

## Cookie and Request Authentication

In addition to WebSocket tracking cookies, the WebSocket HTTP Request can also be included with the HELLO details, as `Details.transport.auth.request`, when enabled in the router.
This allows `Authenticator` implementations to use any cookie values, or other data present in the HTTP request, to make authentication decisions.

## Cookie and Request Authorization

Once a client is authenticated, the `Details.transport.auth` information is included in the session details.
This allows [`Authorizer`](https://pkg.go.dev/github.com/gammazero/nexus/v3/router#Authorizer) implementations to inspect this data for making authorization decisions.

Note: `session.Details.transport.auth` information is not available in the session meta information for the following reasons:

1. It is not serializable (stored as go http objects)
2. To prevent exposure of authentication material

## Enabling Cookie and Request Data

To enable tracking cookies and request capture, set the `EnableTrackingCookie` and `EnableRequestCapture` fields of the [`WebsocketServer`](https://pkg.go.dev/github.com/gammazero/nexus/v3/router#WebsocketServer) after creating it, and before starting it:

```go
wss := router.NewWebsocketServer(nxr)
wss.EnableTrackingCookie = true
wss.EnableRequestCapture = true
closer, err := wss.ListenAndServe(address)
```

The `TrackingCookieSameSiteAttribute` and `TrackingCookieSecureAttribute` fields set the `SameSite` and `Secure` attributes of the tracking cookie.
The tracking cookie is always `HttpOnly`, so scripts in a browser cannot read it.

When running nexusd, set `enable_tracking_cookie` and `enable_request_capture` in the `websocket` section of the config file.
