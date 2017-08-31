<img src="https://github.com/gammazero/nexus/raw/master/doc/n-logo2.png" align="left" hspace="10" vspace="6">

# WAMP v2 router library, client library and router service

[![Build Status](https://travis-ci.org/gammazero/nexus.svg)](https://travis-ci.org/gammazero/nexus)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/gammazero/nexus/blob/master/LICENSE)


**nexus** is a [WAMP](http://wamp-proto.org/) v2 router library, client library, and a router service, that implements most of the features defined in the advanced profile.  The nexus project is written in [Go](http://golang.org/) and designed for highly concurrent asynchronous I/O.  The nexus router provides extended functionality.  The router and client interoperate with other WAMP implementations.

## Full Documentation

See the [Wiki](https://github.com/gammazero/nexus/wiki) for full documentation, examples, and operational details.

For the API: [![GoDoc](https://godoc.org/github.com/gammazero/nexus?status.svg)](https://godoc.org/github.com/gammazero/nexus)

## What is WAMP and nexus

The Web Application Messaging Protocol (WAMP) is an open standard WebSocket subprotocol that provides two application messaging patterns in one unified protocol:
[Remote Procedure Calls](http://wamp-proto.org/faq/#rpc) and [Publish & Subscribe](http://wamp-proto.org/faq/#pubsub)

Using WAMP you can build distributed systems out of application components which are loosely coupled and communicate in (soft) real-time.

The nexus project provides a WAMP router library, client library, and stand-alone WAMP router service.
 - The router library can be used to build custom WAMP routers or to embed a WAMP router in an application.
 - The client library can be used to build clients that connect to any WAMP server, or to communicate in-process with a WAMP router embedded in the same application.
 - The router service can be run as-is to provide WAMP routing.
 
### Nexus Features

- **Concurrent Asynchronous I/O** Nexus supports large numbers of clients concurrently sending and receiving messages, and never blocks on I/O, even if a client becomes unresponsive.  See [Router Concurrency](https://github.com/gammazero/nexus/wiki/Router-Concurrency) for details.
- **WAMP Advanced Profile Features**  This project implements most of the advanced profile features in WAMP v2.  See [current feature support](https://github.com/gammazero/nexus#advanced-profile-feature-support) provided by nexus.  Nexus also offers extended functionality for retrieving session information and for message filtering, giving clients more ability to decide where to send messages.
- **Flexibility** Multiple transports and serialization options are supported, and more are being developed to maximize interoperability.  Currently nexus provides websocket and local (in-process) transports.  JSON and [MessagePack](http://msgpack.org/index.html) serialization is available over websockets.
- **Security** TLS over websockets is included with the [Gorilla WebSocket](https://github.com/gorilla/websocket) package that nexus uses as its websocket implementation.  The nexus router library provides interfaces for integration of client authentication and authorization logic.

## Quick Start

### Installation
```
go get github.com/gammazero/nexus
```

### Examples

Look at the examples to see how to create simple clients and servers.  Examples of using advanced profile features are provide in the full documentation.

https://github.com/gammazero/nexus/tree/master/examples

## Contributing

Please read the [Contributing](https://github.com/gammazero/nexus/blob/master/CONTRIBUTING.md#welcome) guide if you are interested in becoming a contributor to this project.

## Status
Initial stable release **nexus-1.0** planned: 05 September 2017

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

- [Subscriber black/white listing for any session attribute](https://github.com/gammazero/nexus/wiki/Subscriber-black-white-listing-for-any-session-attribute)
- [Session Meta API provides all session attributes](https://github.com/gammazero/nexus/wiki/Session-Meta-API-provides-all-session-attributes)
