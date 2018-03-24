<img src="https://github.com/gammazero/nexus/raw/master/doc/n-logo2.png" align="left" hspace="10" vspace="6">

# WAMP v2 router library, client library and router service

[![Build Status](https://travis-ci.org/gammazero/nexus.svg)](https://travis-ci.org/gammazero/nexus)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/gammazero/nexus/blob/master/LICENSE)


**nexus** is a [WAMP](http://wamp-proto.org/) v2 router library, client library, and a router service, that implements most of the features defined in the advanced profile.  The nexus project is written in [Go](http://golang.org/) and designed for highly concurrent asynchronous I/O.  The nexus router provides extended functionality.  The router and client interoperate with other WAMP implementations.

## Full Documentation

See the [**nexus project Wiki**](https://github.com/gammazero/nexus/wiki) for full documentation, examples, and operational details.

For the API: [![GoDoc](https://godoc.org/github.com/gammazero/nexus?status.svg)](https://godoc.org/github.com/gammazero/nexus)

## What is WAMP and nexus

The Web Application Messaging Protocol (WAMP) is an open standard WebSocket subprotocol that provides two application messaging patterns in one unified protocol:
[Remote Procedure Calls](http://wamp-proto.org/faq/#rpc) and [Publish & Subscribe](http://wamp-proto.org/faq/#pubsub)

Using WAMP you can build distributed systems out of application components which are loosely coupled and communicate in (soft) real-time.

Nexus is a software package that provides a WAMP router library, client library, and stand-alone WAMP router service.  This nexus package provides a messaging system for building distributed applications and services.  Endpoints using websockets, TCP sockets, Unix sockets, and in-process connections may all communicate with eachother via pub/sub and routed RPC messaging.
 - The router library can be used to build custom WAMP routers or to embed a WAMP router in an application.
 - The client library can be used to build clients that connect to any WAMP server, or to communicate in-process with a WAMP router embedded in the same application.
 - The router service can be run as-is to provide WAMP routing.
 
### Nexus Features

- **Concurrent Asynchronous I/O** Nexus supports large numbers of clients concurrently sending and receiving messages, and never blocks on I/O, even if a client becomes unresponsive.  See [Router Concurrency](https://github.com/gammazero/nexus/wiki/Router-Concurrency) for details.
- **WAMP Advanced Profile Features**  This project implements most of the advanced profile features in WAMP v2.  See [current feature support](https://github.com/gammazero/nexus#advanced-profile-feature-support) provided by nexus.  Nexus also offers extended functionality for retrieving session information and for message filtering, giving clients more ability to decide where to send messages.
- **Flexibility** Multiple transports and serialization options are supported, and more are being developed to maximize interoperability.  Currently nexus provides websocket, rawsocket (tcp and unix), and local (in-process) transports.  [JSON](https://en.wikipedia.org/wiki/JSON) and [MessagePack](http://msgpack.org/index.html) serialization is available over websockets and rawsockets.
- **Security** TLS is available over websockets and rawsockets with client and server APIs that allow configuration of TLS.  The nexus router library also provides interfaces for integration of client authentication and authorization logic.

## Quick Start

### Installation
```
go get github.com/gammazero/nexus
```

### Build, Configure, and Run Router Service
```
cd nexusd
go build
vi etc/nexus.json
./nexusd
```

### Examples

Look at the examples to see how to create simple clients and servers.  Examples of using advanced profile features are available in the [full documentation](https://github.com/gammazero/nexus/wiki).

https://github.com/gammazero/nexus/tree/master/examples

## Contributing

Please read the [Contributing](https://github.com/gammazero/nexus/blob/master/CONTRIBUTING.md#contributing-to-nexus) guide if you are interested in becoming a contributor to this project.

## Status

A stable release of nexus is available with support for most advanced profile features.  Nexus is in active development, to add remaining advanced profile features, examples, automated tests, and documentation.

### TODO

These features listed here are being added.  If there are are specific items needed, or if any changes in current functionality are needed, then please open an [issue](https://github.com/gammazero/nexus/issues).

- more stress tests and benchmarks
- more documentation
- more advanced profile examples
- testament_meta_api
- event history
- [CBOR](https://tools.ietf.org/html/rfc7049) Serialization
- call trust levels
- publisher trust levels

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
| cookie authentication | Yes |
| ticket authentication | Yes |
| rawsocket transport | Yes |
| batched WS transport | No |
| longpoll transport | No |
| session meta api | Yes |
| TLS for websockets | Yes |
| TLS for rawsockets | Yes |
| websocket compression | Yes |

## Extended Functionality

Nexus provides [extended functionality](https://github.com/gammazero/nexus/wiki/Extended-Functionality) around subscriber black/white listing and in the information available via the session meta API.  This enhances the ability of clients to make desisions about message recipients.
