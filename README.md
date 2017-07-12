# nexus
Go Implementation of [WAMP](http://wamp-proto.org/)v2

The Web Application Messaging Protocol (WAMP) is an open standard WebSocket subprotocol that provides two application messaging patterns in one unified protocol:
[Remote Procedure Calls](http://wamp-proto.org/faq/#rpc) and [Publish & Subscribe](http://wamp-proto.org/faq/#pubsub)

Using WAMP you can build distributed systems out of application components which are loosely coupled and communicate in (soft) real-time.

The nexus project provides a WAMP router library, client library, and stand-alone WAMP router service.
 - The router library can be used to build custom WAMP routers or to embed a WAMP router in an aplication.
 - The client libaray can be used to build clients that connect to any WAMP server.
 - The router service can be run as-is to provide WAMP routing.

## Project Status
Nearly complete - currently in active development.

#### Items to complete:
- websocket server
- websocket client
- additional transports
- examples
- documentation
- cool logo

#### Features to complete:
- publisher trust levels                                                     
- event history                                                              
- testament_meta_api                                                         
- session_meta_api
- call timeout (need timeout goroutine)                                      
- call trust levels                                                          
- session_meta_api                                                           
- registration_meta_api   

## Advanced Profile Feature Support

### RPC Features

| Feature | Supported |
| ------- | --------- |
| progressive_call_results | Yes |
| progressive_calls |  Yes |
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

