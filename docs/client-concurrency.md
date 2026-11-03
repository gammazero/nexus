The client is non-blocking in that different sessions, whether from the same or different client, will not block each other.  Within a single session, operations may block, waiting for a response or for a callback to handle a response.  If a client needs to do different things concurrently, such as publishing events, receiving events, and making RPC calls, these can be done concurrently using separate sessions.

## Some restrictions on reentrancy 
You cannot call a client function, that waits for a response, from within an event callback. This is because the client is waiting for the callback to return, before it process the next incoming message.  The call made within the callback will timeout because the client cannot any process incoming messages until the callback returns. This behavior is due to the need to handle events serially, to guarantee ordered event delivery as required by the WAMP protocol. If events were handled asynchronously, using separate goroutines, then order could not be guaranteed and would be happen however goroutines were scheduled.

If an event needs to trigger a call, this is easy to handle in a number of ways.

- Use a goroutine to call client functions from within an event handler.
- Use SubscribeChan to have events delivered on a channel instead of using an event handler callback. Of course the channel needs to be read from to keep processing events.
- Use some other signaling mechanism to allow your event handler to signal another goroutine to call Call
