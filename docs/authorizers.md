Authorizer API: [![GoDoc](https://pkg.go.dev/badge/github.com/gammazero/nexus/v3/router)](https://pkg.go.dev/github.com/gammazero/nexus/v3/router#Authorizer)

# Authorizer

An [Authorizer](https://pkg.go.dev/github.com/gammazero/nexus/v3/router#Authorizer) is an interface that specifies an `Authorize` function.
To define an Authorizer, implement this interface and provide an instance in the [realm config](https://pkg.go.dev/github.com/gammazero/nexus/v3/router#RealmConfig) when configuring the router.

The Authorizer function is called for each message that a client tries to send, allowing the router to intercept each message before it is routed to its recipient(s).
This allows the router to make per-message decisions by looking at the sending session and the message.

For example, if only sessions with specific `authid` values are allowed to publish messages with a special attribute, then the authorizer can enforce this restriction.
The authorizer would look at PUBLISH message attributes to see if the special attribute is present, and if so then the authorizer would look at the session to check for the necessary `authid` value, and decide whether or not to send the message.

## Message Interceptor

The Authorizer is given the message through a pointer, so it can change the content of the message before the router routes it.
It is also given a copy of the sending session that holds the session ID and details.
Changes the Authorizer makes to these session details are applied to the sending session after it returns.
This allows the authorizer to also work as an interceptor of messages that can change their content and/or change the sending session's details based on the intercepted message.

This functionality may be used to set values in the session details upon encountering certain messages sent by that session.

The copy of the session details is shallow, so nested values are shared with the session.
To change a nested value, the Authorizer must replace it, not change it in place.

## Example

```go
// Create type that implements Authorizer interface.
type testAuthz struct{}

func (a *testAuthz) Authorize(sess *wamp.Session, msg wamp.Message) (bool, error) {
    // If this is not a subscribe message, then allow it through.
    m, ok := msg.(*wamp.Subscribe)
    if !ok {
        return true, nil
    }
    switch m.Topic {
    case "stocks.amex":
        // Use correct topic: translate AMEX to NYSE.
        m.Topic = "stocks.nyse"
        // Add note to session.
        wamp.SetOption(sess.Details, "old_exchange_name", "amex")
    case "stocks.lse":
        // Only Elizabeth is authorized to subscribe to stocks.lse.
        if wamp.OptionString(sess.Details, "authid") != "Elizabeth" {
            return false, nil
        }
    }
    return true, nil
}

// Create router instance with realm that uses authorizer.
routerConfig := &router.Config{
    RealmConfigs: []*router.RealmConfig{
        {
            URI:           wamp.URI("example.authz"),
            AnonymousAuth: true,
            Authorizer:    &testAuthz{},
        },
    },
}
nxr, err := router.NewRouter(routerConfig, nil)
if err != nil {
    panic(err)
}
defer nxr.Close()
```
