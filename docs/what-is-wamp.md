# Understanding WAMP

The [Web Application Messaging Protocol (WAMP)](http://wamp-proto.org/) defines a communication system that provides publish-subscribe (PubSub) and remote procedure call (RPD) communications.

## Origins

Traditionally, web applications have been written to mirror static documents.  Updates to the content of these static documents required a user to refresh their browser, or for code running within the browser to request updates.  This is not sufficient for the dynamic nature of data in the modern Web.  Updates may come from the server, originating from other collaborating application components, all needing to communicate with each other in real-time.

Most distributed application messaging, including that of Web applications, converges to two basic patters: [Publish & Subscribe (PubSub)](http://wamp-proto.org/faq/#pubsub) and [Remote Procedure Calls (RPC)](http://wamp-proto.org/faq/#rpc).  PubSub is needed for automatic updates; publishing an event to interested parties.  RPC is needed for request/response type communication between different components.  Modern web applications need these patterns to communicate between browser and server-based components, and it is rare that only one or the other pattern is needed.

Supporting both PubSub and RPC would typically require two different technologies, and therefore two different tech stacks and maybe two different servers for them.  All of the application components would need separate connections to each service supplying the messaging patterns, requiring separate authentication and authorization, each with a separate [API](https://en.wikipedia.org/wiki/Application_programming_interface), all resulting in increased system and implementation complexity.

## Solution to Messaging

WAMP solves this problem by integrating the two messaging patterns into a single protocol.  It does so in such a way so as to decouple communicating components; by _routing_ messages between them, so that components do not need to know about the existence or identity of other components.  With a single connection, single API and single library, an application can use both messaging patterns with the application's server components.

### Not Only for Web Applications. 

Since distributed application messaging generally converges to PubSub and RPC, WAMP is also a solution for messaging in distributed applications in general.  WAMP is particularly advantageous because it allows loose coupling of communicating entities; they do not need to communicate directly to each other, but to a WAMP router that distributes messages.

WAMP offers additional protocol features on top of the basic routing, such as authentication of clients, authorization based on roles and publication topics, load-balancing procedure calls, pattern-based subscription and registration, restriction of publications to certain clients, and more depending on the WAMP implementation.

All of these features make WAMP an excellent solution to most distributed application messaging needs, whether for a web application or any distributed set of communicating components.  WAMP is well-suited as the communication framework for [IoT](https://en.wikipedia.org/wiki/Internet_of_things) deployments.

## WAMP Concepts

### Overview (from the WAMP specification):

WAMP is a routed protocol, with all components connecting to a _WAMP Router_, where the WAMP Router performs message routing between the components.

WAMP provides two messaging patterns: _Publish & Subscribe_ and _routed Remote Procedure Calls_.

Publish & Subscribe (PubSub) is an established messaging pattern where a component, the _Subscriber_, informs the router that it wants to receive information on a topic (i.e., it subscribes to a topic). Another component, a _Publisher_, can then publish to this topic, and the router distributes events to all Subscribers.

Routed Remote Procedure Calls (RPCs) rely on the same sort of decoupling that is used by the Publish & Subscribe pattern. A component, the _Callee_, announces to the router that it provides a certain procedure, identified by a procedure name. Other components, _Callers_, can then call the procedure, with the router invoking the procedure on the Callee, receiving the procedure's result, and then forwarding this result back to the Caller. Routed RPCs differ from traditional client-server RPCs in that the router serves as an intermediary between the Caller and the Callee.

The decoupling in routed RPCs arises from the fact that the Caller is no longer required to have knowledge of the Callee; it merely needs to know the identifier of the procedure it wants to call. There is also no longer a need for a direct connection between the caller and the callee, since all traffic is routed. This enables the calling of procedures in components which are not reachable externally (e.g. on a NATted connection) but which can establish an outgoing connection to the WAMP router.

Combining these two patterns into a single protocol allows it to be used for the entire messaging requirements of an application, thus reducing technology stack complexity, as well as networking overheads.

### Transports

WAMP uses WebSocket as its default transport.  This means that components can be run anywhere where outgoing HTTP connections are possible. This includes the browser and mobile devices.  WebSockets provides a persistent, bidirectional connection, that enables a true push from the server. Messages do not contain HTTP headers, which reduces overhead on the wire compared with HTTP. WebSockets is supported in all modern browsers.

WAMP may use different transports as well, such as the [RawSocket Transport](http://wamp-proto.org/static/rfc/draft-oberstet-hybi-crossbar-wamp.html#rawsocket). WAMP can run over any transport which is message-oriented, ordered, reliable, and bi-directional.  Since connections are bi-directional, the router is able to push events and calls immediately.

### Components and Roles

The PubSub messaging pattern defines three roles: _Subscribers_ and _Publishers_, which communicate via a _Broker_.

The routed RPC messaging pattern also defines three roles: _Callers_ and _Callees_, which communicate via a _Dealer_.

A _Router_ is a component which implements one or both of the Broker and Dealer roles. A _Client_ is a component which implements any or all of the Subscriber, Publisher, Caller, or Callee roles.

When a Client connects to a Router, it establishes a session by joining a _Realm_ on that Router. A Realm is a WAMP routing and administrative domain, optionally protected by authentication and authorization. Routing occurs only between WAMP Sessions that have joined the same Realm.  The client specifies the Realm to join by a URI included in the first message to the Router.
