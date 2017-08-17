# nexus
[![Build Status](https://travis-ci.org/gammazero/nexus.svg)](https://travis-ci.org/gammazero/nexus)

Go Implementation of [WAMP](http://wamp-proto.org/) v2

The Web Application Messaging Protocol (WAMP) is an open standard WebSocket subprotocol that provides two application messaging patterns in one unified protocol:
[Remote Procedure Calls](http://wamp-proto.org/faq/#rpc) and [Publish & Subscribe](http://wamp-proto.org/faq/#pubsub)

Using WAMP you can build distributed systems out of application components which are loosely coupled and communicate in (soft) real-time.

The nexus project provides a WAMP router library, client library, and stand-alone WAMP router service.
 - The router library can be used to build custom WAMP routers or to embed a WAMP router in an application.
 - The client library can be used to build clients that connect to any WAMP server.
 - The router service can be run as-is to provide WAMP routing.

## Project Objectives

### Performance 

Nexus achieves high throughput by never blocking on I/O.  Messages received from clients are dispatched to the appropriate handlers for routing and are then written to the outbound message queues of the receiving clients.  This way the router never delays processing of messages due to waiting for slow clients.

### Feature availability

This project intends to implement most or all of the advanced profile features in WAMP v2.

### Flexibility

Multiple transports and serialization options will be supported to maximize interoperability.

### Security

Transport security, such as TLS over websockets will be included.  Router library provides interfaces for integration of client authentication logic.

## Project Status
Currently in active development - contributions welcome.

#### Features to complete - priority order:
- active call timeout (need handling by router)
- testament_meta_api
- event history
- call trust levels
- publisher trust levels

#### Items to complete:
- documentation (in progress)
- examples (in progress)
- RawSocket transport (planned)
- cool logo (maybe)

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

As an example, to allow sessions with `org_id=ycorp` or `org_id=zcorp`, a PUBLISH message specifies, in it options, the following:
```
eligible_org_id: {"ycorp", "zcorp"}
```

Note: Nexus includes all attributes from the HELLO message in the session details.
