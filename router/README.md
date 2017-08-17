## Router Concurrency

The nexus WAMP router operates as a concurrent pipeline that does not block on I/O at any stage (src-client -> router/realm -> broker -> sendQueue -> dst-client).  Messages received from clients are dispatched to the appropriate handlers for routing and are then written to the outbound message queues of the receiving clients.  This way the router never delays processing of messages due to wait for slow clients.

If an outbound message queue becomes full and blocks, due to a slow client that
is receiving a high volume of message, the router drops the message.  Outbound
message queue sizes are configurable (can be unlimited).  A size limit limits
the number of pending messages that can pile up waiting for a slow client.

```
      Router
        Reaml1
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

### Rules

- Different publishers can dispatch concurrently to one subscriber.
- One publisher can dispatch concurrently to different subscribers.
- One publisher dispatches serially to one subscriber.

### Operation

Each (xHandler) is a singe goroutine+channel that persists for the lifetime of the associated object.  Therefore, order is preserved for messages between any two sessions regardless of goroutine scheduling order.

Broker and Dealer cannot dispatch an event to a new goroutine, because this would mean that messages bound for the same destination session would be sent in separate goroutines, and delivery order would be affected by goroutine scheduling order.  Instead they dispatch to the receiving session's out channel+goroutine.

Broker and Dealer have their own handler goroutines to:
1) Safely access subscription or call maps.
2) Not make session's input-handler wait for Broker when it could be dispatching other messages to Dealer.

NOTE: Sessions have separate in (recv) and out (send) handlers so that processing incoming messages is not blocked while waiting for outgoing messages to finish being sent.  Also, the router is not blocked waiting for a session to send a message to a client.

### Handling Message Overflow

If there are too many messages sent to the same session before the session can consume them, the session's channel that the router is writing may become full and block. When this happens the broker or dealer will need to drop messages to be able to continue processing.

The current implementation drops messages in this situation.  If a RPC invocation message is dropped, an error is returned to the caller.

This may be addressed by putting the outgoing messages on a dynamically sized output queue for the session.  See: https://github.com/gammazero/bigchan.  This approach was not chosen as dropping messages will not lead to unbounded memory use. 


