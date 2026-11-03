# Authenticators

## Router-side Authenticators

Router Authenticator API: [![GoDoc](https://pkg.go.dev/badge/github.com/gammazero/nexus/v3/router/auth)](https://pkg.go.dev/github.com/gammazero/nexus/v3/router/auth)

The nexus router implementation provides an [authentication package](https://pkg.go.dev/github.com/gammazero/nexus/v3/router/auth) that allows implementors to supply authenticator implementations.
Included in this package is a challenge-response authenticator and ticket authenticator, which only need a [KeyStore](https://pkg.go.dev/github.com/gammazero/nexus/v3/router/auth#KeyStore) implementation for looking up authentication keys.

The authenticators have access to HTTP request and tracking/auth cookies when these are enabled in the router configuration.
This allows a router implementation to use cookie-based authentication, client tracking (generally for remembering previously authenticated clients), and information from websocket requests for making authentication decisions.

To enable the use of websocket request data and client tracking cookies, see the documentation for [Cookie and Request Auth/Authz](cookies-and-request-auth.md).

### Authenticator Interface

By supplying an implementation of the [`Authenticator`](https://pkg.go.dev/github.com/gammazero/nexus/v3/router/auth#Authenticator) interface, any authentication logic can be defined for a router.

Nexus provides two implementations: `CRAuthenticator` for challenge-response authentication and `TicketAuthenticator` for ticket authentication.

### CRAuthenticator

To implement a wampcra authenticator, the biggest part is to define a `KeyStore`.
Here is a simple example that holds key information for one user:

```go
type serverKeyStore struct {
    provider string
    role     string

    pbkdf2Key  []byte
    salt       string
    keylen     int
    iterations int

    ticket string
}

func (ks *serverKeyStore) AuthKey(authid, authmethod string) ([]byte, error) {
    if authid != "jdoe" {
        return nil, errors.New("no such user: " + authid)
    }
    switch authmethod {
    case "wampcra":
        // Lookup the user's PBKDF2-derived key.
        return ks.pbkdf2Key, nil
    case "ticket":
        // Lookup the user's ticket.
        return []byte(ks.ticket), nil
    }
    return nil, errors.New("unsupported authmethod")
}

func (ks *serverKeyStore) PasswordInfo(authid string) (string, int, int) {
    if authid != "jdoe" {
        return "", 0, 0
    }
    return ks.salt, ks.keylen, ks.iterations
}

func (ks *serverKeyStore) Provider() string { return ks.provider }

func (ks *serverKeyStore) AuthRole(authid string) (string, error) {
    if authid != "jdoe" {
        return "", errors.New("no such user: " + authid)
    }
    return ks.role, nil
}
```

Then configure the router:
```go
keyStore := &serverKeyStore{
    provider: "static",
    role:     "user",
    // Set the key, salt, keylen and iterations for the user.
}
crAuth := auth.NewCRAuthenticator(keyStore, time.Second)
routerConfig := &router.Config{
    RealmConfigs: []*router.RealmConfig{
        {
            URI:            wamp.URI("nexus.test.auth"),
            Authenticators: []auth.Authenticator{crAuth},
        },
    },
}
nxr, err := router.NewRouter(routerConfig, nil)
```

The new authenticator takes care of all the rest: Creates challenge string, composes CHALLENGE message and sends to client, gets key from KeyStore, computes HMAC-SHA256 over challenge string with user's key, compares computed sig to sig sent in challenge response.

A ticket authenticator, created with [`auth.NewTicketAuthenticator`](https://pkg.go.dev/github.com/gammazero/nexus/v3/router/auth#NewTicketAuthenticator), is basically the same, and can use the same `KeyStore` instance.
The `KeyStore.AuthKey()` can look at the `authid` and `authmethod` to determine what key to return.
So that same method can return CR keys or tickets depending on `authmethod`.

## Client-side Challenge-Response Authenticator

The nexus client library contains logic for working with challenge-response authentication.
This includes any challenge-response authentication, and is not limited to the particular implementation that is included with the router auth package.

To implement a nexus client that uses WAMP CR authentication, define a function that is called when a CHALLENGE is received:
```go
func clientAuthFunc(c *wamp.Challenge) (string, wamp.Dict) {
    // Get user password and return signature.
    password := AskUserPassword()
    // Sign challenge with key made using password with PBKDF2.
    return crsign.RespondChallenge(password, c, nil), wamp.Dict{}
}
```
The function [`crsign.RespondChallenge`](https://pkg.go.dev/github.com/gammazero/nexus/v3/wamp/crsign#RespondChallenge) is used by clients to sign the challenge string contained in the CHALLENGE message using the given password.
This handles computing a derived key from the password using [PBKDF2](https://en.wikipedia.org/wiki/PBKDF2).

After defining the auth handler function, configure a client to use it:
```go
cfg := client.Config{
    Realm: "nexus.test.auth",
    HelloDetails: wamp.Dict{
        "authid": "jdoe",
    },
    AuthHandlers: map[string]client.AuthFunc{
        "wampcra": clientAuthFunc,
    },
    Logger: logger,
}
cli, err := client.ConnectNet(context.Background(), routerURL, cfg)
```
