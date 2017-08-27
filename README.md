# nexus
nexus is a [Go](http://golang.org/) implementation of [WAMP](http://wamp-proto.org/) v2 that provides router and client libraries and a router service.

[![Build Status](https://travis-ci.org/gammazero/nexus.svg)](https://travis-ci.org/gammazero/nexus)

The Web Application Messaging Protocol (WAMP) is an open standard WebSocket subprotocol that provides two application messaging patterns in one unified protocol:
[Remote Procedure Calls](http://wamp-proto.org/faq/#rpc) and [Publish & Subscribe](http://wamp-proto.org/faq/#pubsub)

Using WAMP you can build distributed systems out of application components which are loosely coupled and communicate in (soft) real-time.

The nexus project provides a WAMP router library, client library, and stand-alone WAMP router service.
 - The router library can be used to build custom WAMP routers or to embed a WAMP router in an application.
 - The client library can be used to build clients that connect to any WAMP server, or to communicate in-process with a WAMP router embedded in the same application.
 - The router service can be run as-is to provide WAMP routing.

## Status
Currently in active development - contributions welcome.

Initial stable release **nexus-1.0** planned: 31 August 2017

## Installation
```
go get github.com/gammazero/nexus
```

## Examples

https://github.com/gammazero/nexus/tree/master/examples

## Features

### Concurrent Asynchronous I/O

Nexus supports large numbers of clients concurrently sending and receiving message, and never blocks on I/O, even if a client becomes unresponsive.  

See [Router Concurrency](https://github.com/gammazero/nexus/wiki/Router-Concurrency) for details.

### WAMP Advanced Profile

This project implements most of the advanced profile features in WAMP v2.  See [current feature support](https://github.com/gammazero/nexus#advanced-profile-feature-support) provided by nexus.

### Flexibility

Multiple transports and serialization options are supported, and more are being developed to maximize interoperability.  Currently nexus provides websocket and local (in-process) transports.  JSON and [MessagePack](http://msgpack.org/index.html) serialization is available over websockets.

Nexus offers extended functionality for retrieving session information and for message filtering, giving clients more ability to decide where to send messages.

### Security

TLS over websockets is included with the [Gorilla WebSocket](https://github.com/gorilla/websocket) package that nexus uses as its websocket implementation.  The nexus router library provides interfaces for integration of client authentication and authorization logic.

### TODO

#### Features to complete - priority order:
- active call timeout (need handling by router)
- testament_meta_api
- event history
- call trust levels
- publisher trust levels

#### Items to complete:
- documentation (in progress)
- advanced profile examples (in progress)
- TLS config for `nexusd`
- RawSocket transport (planned)

## Advanced Profile Feature Support

### RPC Features

| Feature | Supported |
| ------- | --------- |
| progressive_call_results | Yes |
| progressive_calls | No |
| call_timeout | Yes |
| call_canceling | Yes |
| caller_identification | Yes | 
| call_trustlevels | No |
| registration_meta_api | Yes
| pattern_based_registration | Yes | 
| shared_registration | Yes |
| sharded_registration | No |
| registration_revocation | No |
| procedure_reflection | No |
 
### PubSub Features

| Feature | Supported |
| ------- | --------- |
| subscriber_blackwhite_listing | Yes |
| publisher_exclusion | Yes |
| publisher_identification | Yes |
| publication_trustlevels | No|
| subscription_meta_api | Yes |
| pattern_based_subscription | Yes |
| sharded_subscription | No |
| event_history | No |
| topic_reflection | No |

### Other Advanced Features

| Feature | Supported |
| ------- | --------- |
| challenge-response authentication | Yes | 
| cookie authentication | No |
| ticket authentication | Yes |
| rawsocket transport | No |
| batched WS transport | No |
| longpoll transport | No |
| session meta api | Yes |

## Extended Functionality

### Subscriber black/white listing for any session attribute

Nexus extends WAMP's subscriber black/white listing functionality to support filtering on any attribute in the subscriber session details.

WAMP allows blacklisting `authid` and `authrole` using `exclude_authid` and `exclude_authrole`, and allows whitelisting these attributes using `eligible_authid` and `eligible_authrole`.  Nexus recognizes the publish options `exclude_xxx` and `eligible_xxx`, accompanied with a list of string values to match against, where `xxx` is he name of any attribute in the session details.

As an example, to allow sessions with `org_id=ycorp` or `org_id=zcorp`, a PUBLISH message specifies the following option:
```
eligible_org_id: ["ycorp", "zcorp"]
```

Note: Nexus includes all attributes from the HELLO message in the session details.

### Session Meta API provides all session attributes

The `wamp.session.on_join` meta event message and the response to a `wamp.session.get` meta procedure includes the attributes specified by the WAMP specification (`session`, `authid`, `authrole`, `authmethod`, `authprovider`, `transport`), and includes all attributes from the session HELLO message.  This allows clients to provide more information about themselves, via HELLO, that may then be used by other sessions to make decisions about who to send messages to.

### Handling Message Overflow

If there are too many messages sent to the same session before the session can consume them, the session's channel that the router is writing may become full and block. When this happens the broker or dealer will need to drop messages to be able to continue processing.

The current implementation drops messages in this situation.  If a RPC invocation message is dropped, an error is returned to the caller.

This may be addressed by putting the outgoing messages on a dynamically sized output queue for the session.  See: https://github.com/gammazero/bigchan.  This approach was not chosen as dropping messages will not lead to unbounded memory use. 
