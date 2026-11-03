# Router Concurrency

## Rules
The router is non-blocking in that:
1. Reading and writing one session will never block another session.
2. Writing to a session will not affect reading from that same session.
3. Reading from a session will not affect writing to that same session.

This means that:
- Different publishers can dispatch concurrently to one subscriber.
- One publisher can dispatch concurrently to different subscribers.
- One publisher dispatches serially to one subscriber.
- Different callers can concurrently call a callee.

## Design Concept

- [Floorplan Diagram](nexus-floorplan.pdf)

The nexus WAMP router operates as a set of concurrent processing stages, that does not block on I/O at any stage (src-client -> router/realm -> broker -> sendQueue -> dst-client).
Messages received from clients are dispatched to the appropriate handlers for routing and are then written to the outbound message queues of the receiving clients.
This way the router never delays processing of messages waiting for I/O with clients.

```
      Router
        Realm1
          sessions
  invk <--- session1............(outHandler) <--------+
   evt <--- session2............(outHandler) <-----+  |
   evt <--- session3............(outHandler) <--+  |  |
   pub ---> session4........(inHandler)         |  |  |
  call ---> session5..(inHandler)  |            |  |  |
                          |        |pub         |  |  |
                      call|        |            |  |  |
                          |        V            |  |  |
          Broker..........)..(Handler)          |  |  |
            subscribers   |     |   |     event |  |  |
              *session2   |     |   +-----------+  |  |
              *session3   |     +------------------+  |
                          |                           |
                          V            invocation     |
          Dealer.......(Handler)----------------------+
            callees
              *session1

          Roles
            callee: session1
            subscriber: session2, session3
            publisher: session4
            caller: session5
```

## Operation

Each (xHandler) is a single goroutine+channel that persists for the lifetime of the associated object.
Therefore, order is preserved for messages between any two sessions regardless of goroutine scheduling order.

Broker and Dealer cannot dispatch an event to a new goroutine, because this would mean that messages bound for the same destination session would be sent in separate goroutines, and delivery order would be affected by goroutine scheduling order.
Instead they dispatch to the receiving session's out channel+goroutine.

Broker and Dealer have their own handler goroutines to:
1) Safely access subscription or call maps.
2) Not make session's input-handler wait for Broker when it could be dispatching other messages to Dealer.

NOTE: Sessions have separate in (recv) and out (send) handlers so that processing incoming messages is not blocked while waiting for outgoing messages to finish being sent.
Also, the router is not blocked waiting for a session to send a message to a client.

## Handling Message Overflow

- **Replies are never dropped (https://github.com/gammazero/nexus/pull/437).** Before v3.4.0, replies to a client's own requests, such as PUBLISHED, SUBSCRIBED, REGISTERED, RESULT and ERROR, used to be dropped when the client's outbound queue was full, so the request failed only when the client's response timeout expired.
In v3.4.0 and later, they now wait in an ordered per-session backlog.
Before handling a client's next request, the router waits until there is room for its reply.
A client that reads nothing for a minute while the router waits is disconnected.
- **EVENTs wait for a slow subscriber (https://github.com/gammazero/nexus/pull/439).** In v3.4.0 and later, an EVENT to a subscriber whose queue is full now waits in the same backlog, up to the realm's new `EventBacklog` limit, which defaults to the client's outbound queue size.
INVOCATIONs, INTERRUPTs and progressive RESULTs wait there too.
`EventOverflow` selects what happens to an EVENT that does not fit either: "drop", the default, drops it as before, and "close" closes the client with a GOODBYE carrying `nexus.close.slow_subscriber`, so that it knows it fell behind and can reconnect.
See [Outbound queue size](../cmd/nexusd/README.md#outbound-queue-size) in the nexusd README for guidance.
- **A slow caller no longer blocks its callee (https://github.com/gammazero/nexus/pull/412).** In v3.4.0 and later, a YIELD for a caller whose queue is full is queued for that call.
It no longer blocks every other message from the callee for up to a minute as it did before v3.4.0.
