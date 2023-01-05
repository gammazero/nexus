<img src="doc/n-logo2.png" align="left" hspace="10" vspace="6">

# WAMP v2 router library, client library and router service

[![Build Status](https://travis-ci.org/gammazero/nexus.svg)](https://travis-ci.org/gammazero/nexus)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![GoDoc](https://godoc.org/github.com/gammazero/nexus?status.svg)](https://godoc.org/github.com/gammazero/nexus)

**nexus** is a [WAMP](http://wamp-proto.org/) v2 router library, client library, and a router service, that implements
most of the features defined in the WAMP-protocol advanced profile.  The nexus project is written in
[Go](http://golang.org/) and designed for highly concurrent asynchronous I/O.  The nexus router provides extended
functionality.  The router and client interoperate with other WAMP implementations.

## Documentation

See the [**nexus project Wiki**](https://github.com/gammazero/nexus/wiki) for documentation, examples, and
operational details.

[![GoDoc](https://godoc.org/github.com/gammazero/nexus?status.svg)](https://godoc.org/github.com/gammazero/nexus) for
API documentation.

## What is WAMP and nexus

The Web Application Messaging Protocol (WAMP) is an open standard WebSocket subprotocol that provides two application
messaging patterns in one unified protocol:
[Remote Procedure Calls](http://wamp-proto.org/faq/index.html#rpc) and
[Publish & Subscribe](http://wamp-proto.org/faq/index.html#pubsub).  WAMP is also referred to as WAMP-proto it
distinguish it from [other uses](https://en.wikipedia.org/wiki/WAMP_(disambiguation) of the acronym.

Using WAMP you can build distributed systems out of application components which are loosely coupled and communicate
in (soft) real-time.

Nexus is a software package that provides a WAMP router library, client library, and stand-alone WAMP router service.
This nexus package provides a messaging system for building distributed applications and services.  Endpoints using
websockets, TCP sockets, Unix sockets, and in-process connections may all communicate with each other via pub/sub
and routed RPC messaging.

 - The router library can be used to build custom WAMP routers or to embed a WAMP router in an application.
 - The client library can be used to build clients that connect to any WAMP server, or to communicate in-process
   with a WAMP router embedded in the same application.
 - The router service can be run as-is to provide WAMP routing.

## Installation

### Install nexusd router service from source

```
go get -d github.com/gammazero/nexus/nexusd
cd ${GOPATH}/src/github.com/gammazero/nexus/nexusd
make install
```

Run `nexusd -help` to see usage information.  Although `nexusd` can be run using only command line flags,
a [configuration file](https://raw.githubusercontent.com/gammazero/nexus/v3/nexusd/etc/nexus.json) offers much
more configurability.

### Create configuration file and run nexusd service

```
cp ${GOPATH}/src/github.com/gammazero/nexus/nexusd/etc/nexus.json ./
vi nexus.json
nexusd -c nexus.json
```

### Install examples to run and test with

```
git clone git@github.com:gammazero/nexus.git
cd nexus/examples
```

## Building routers and clients with nexus

The nexus router library is used to build a WAMP router or embed one in your service.  The nexus client library is
used to build WAMP clients into you goloang applications. To use either of these libraries, import the v3 version of
the appropriate packages.  Version v3 is the only currently supported version.

```go
import (
	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)
```

## Examples

Look at the examples to see how to create simple clients and servers.  Follow the
[README.md](https://github.com/gammazero/nexus/blob/v3/examples/README.md) in the `nexus/examples` directory.
Examples of using advanced profile features are also available in the
[wiki documentation](https://github.com/gammazero/nexus/wiki).

https://github.com/gammazero/nexus/tree/v3/examples

The examples include interoperability examples to demonstrate nexus working with different WAMP implementations.
Each of these has its own README.md that describes how to set up the environment and run the examples.

https://github.com/gammazero/nexus/tree/v3/examples/interop

The nexusd service is also an example stand-alone WAMP router.  All the router options are configurable in nexusd
using a configuration file, and one is provided in the `nexusd/etc/` directory.  This can be used as starting point
to write your own WAMP router service.

https://github.com/gammazero/nexus/tree/v3/nexusd

## Contributing

Please read the [Contributing](https://github.com/gammazero/nexus/blob/v3/CONTRIBUTING.md#contributing-to-nexus)
guide if you are interested in becoming a contributor to this project.

Please report any problems, or request changes and new functionality by opening
an [issue](https://github.com/gammazero/nexus/issues).

## Nexus Features

- **Concurrent Asynchronous I/O** Nexus supports large numbers of clients concurrently sending and receiving messages,
  and never blocks on I/O, even if a client becomes unresponsive.  See
  [Router Concurrency](https://github.com/gammazero/nexus/wiki/Router-Concurrency) and
  [Client Concurrency](https://github.com/gammazero/nexus/wiki/Client-Concurrency) for details.
- **WAMP Advanced Profile Features**  This project implements most of the advanced profile features in WAMP v2.
  See [current feature support](https://github.com/gammazero/nexus#advanced-profile-feature-support) provided by nexus.
  Nexus also offers extended functionality for retrieving session information and for message filtering, giving clients
  more ability to decide where to send messages.
- **Flexibility** Multiple transports and serialization options are supported, and more are being developed to maximize
  interoperability. Currently, nexus provides websocket, rawsocket (tcp and unix), and local (in-process) transports.
  [JSON](https://en.wikipedia.org/wiki/JSON), [MessagePack](http://msgpack.org/index.html), and
  [CBOR](https://tools.ietf.org/html/rfc7049) serialization is available over websockets and rawsockets.
- **Security** TLS is available over websockets and rawsockets with client and server APIs that allow configuration
  of TLS.  The nexus router library also provides interfaces for integration of client authentication and authorization
  logic.

### Status

The currently maintained version of this module is 3.x.  Earlier major versions are no longer supported.

## Advanced Profile Feature Support

### RPC Features

| Feature                      | Supported |
|------------------------------|-----------|
| progressive_call_results     | Yes       |
| progressive_calls            | Yes       |
| call_timeout                 | Yes       |
| call_canceling               | Yes       |
| caller_identification        | Yes       |
| call_trustlevels             | No        |
| registration_meta_events     | Yes       |
| registration_meta_procedures | Yes       |
| pattern_based_registration   | Yes       |
| shared_registration          | Yes       |
| sharded_registration         | No        |
| registration_revocation      | No        |
| procedure_reflection         | No        |
| payload_passthru_mode        | Yes       |

### PubSub Features

| Feature                       | Supported |
|-------------------------------|-----------|
| subscriber_blackwhite_listing | Yes       |
| publisher_exclusion           | Yes       |
| publisher_identification      | Yes       |
| publication_trustlevels       | No        |
| subscription_meta_events      | Yes       |
| subscription_meta_procedures  | Yes       |
| pattern_based_subscription    | Yes       |
| sharded_subscription          | No        |
| event_history                 | No        |
| topic_reflection              | No        |
| testament_meta_api            | Yes       |
| payload_passthru_mode         | Yes       |

### Other Advanced Features

| Feature                           | Supported  |
|-----------------------------------|------------|
| rawsocket (TCP & Unix) transport  | Yes        |
| session meta events               | Yes        |
| session meta procedures           | Yes        |
| TLS for websockets                | Yes        |
| TLS for rawsockets                | Yes        |
| challenge-response authentication | Yes        |
| cookie authentication             | Yes        |
| ticket authentication             | Yes        |
| batched WS transport              | No         |
| longpoll transport                | No         |
| websocket compression             | Yes        |

## Extended Functionality

Nexus provides [extended functionality](https://github.com/gammazero/nexus/wiki/Extended-Functionality) around
subscriber black/white listing and in the information available via the session meta API.  This enhances the
ability of clients to make decisions about message recipients.
