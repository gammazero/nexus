# Running Nexus as a daemon

It is possible to run Nexus as standalone daemon. [Makefile](./Makefile)
includes targets for installing and uninstalling service.

Nexusd supports next command line arguments:

* `-c path/to/config/file`. Path to json configuration file.
* `--realm my.awesome.realm`. Realm uri to use for first realm.
* `--ws 0.0.0.0:8080`. Websocket address:port to listen on.
* `--tcp 127.0.0.1:8081`. TCP address:port to listen on.
* `--unix /tmp/nexus.sock`. Unix socket path to listen on.
* `--version`. Print current version.
* `--verbose`. Enable verbose logging.

Command line options take precedence over specified in config file.

Annotated config file example:
```json
{
    "websocket": {
        "address": ":8080",           // Listen address. Can be in form of hostname:port, IP:port, :port (this means to listen on all interfaces)
        "cert_file": "",              // Specify TLS Certificate file and key paths
        "key_file": "",               // to enable secure connections
        "keep_alive": 30,
        "enable_compression": false,
        "allow_origins": ["*"],       // For CORS checking
        "out_queue_size": 0           // Limit on number of pending messages to send to each client. 0 - default (64).
    },
    "rawsocket": {
        "tcp_address": "", // Listen address. Can be in form of hostname:port, IP:port, :port (this means to listen on all interfaces)
        "tcp_keepalive_interval": 180,
        "unix_address": "", // Unix socket path to listen on. It is posible to listen both: tcp and unix socket
        "max_msg_len": 0,   // Maximum message length server can receive. Default = 16M.
        "cert_file": "",    // Specify TLS Certificate file and key paths
        "key_file": "",     // to enable secure connections
        "out_queue_size": 0 // Limit on number of pending messages to send to each client. 0 - default (64).
    },
    "log_path": "",         // File to write log data to. If not specified, log to stdout.
    "router": {             // Router configuration parameters.
        "realm_template": null, // if defined, is used by the router to create new realms
                                // when a client requests to join a realm that does not yet exist.
                                // If not defined, then clients must join existing realms.
                                // If defined it must be an object as in realms option below.
        "realms": [         // Realm configurations array. Router must have at least 1 realm to run
            {               // or you can specify realm template and them realms will be populated on first request
                "uri": "realm1",
                "strict_uri": false,    // Enforce strict URI format validation.
                "allow_disclose": true, // Allow publisher and caller identity disclosure when requested.
                "anonymous_auth": true, // Allow anonymous authentication.
                "meta_strict": false,   // When true, only include standard session details in on_join event
                "meta_include_session_details": [], // When MetaStrict is true, specifies session details
	                                                // to include in addition to the standard details
                "enable_meta_kill": false,   // enables the wamp.session.kill* session meta procedures.
                "enable_meta_modify": false, // enables the wamp.session.modify_details session meta procedures.
                "event_backlog": 0,          // Messages that wait for a slow client beyond its out_queue_size.
                                             // 0 - default (out_queue_size).
                "event_overflow": "drop",    // When an EVENT does not fit: "drop" (default) drops it,
                                             // "close" closes the client. See Outbound queue size.
                "event_history": [           // Configurations for event history stores
                    {
                        "topic": "exact.topic.uri",
                        "match": "exact",   // Matching policy (exact|prefix|wildcard)
                        "limit": 100        // Number of most recent events to store
                    },
                    {
                        "topic": "prefix.topic.uri",
                        "match": "prefix",
                        "limit": 500
                    },
                    {
                        "topic": "wildcard.topic.uri",
                        "match": "wildcard",
                        "limit": 250
                    }
                ]
            }
        ],
        "debug": true,
        "mem_stats_log_sec": 0  // Interval in seconds for logging memory stats. 0 to disable.
	                            // Logs Alloc, Mallocs, Frees, and NumGC.
    }
}
```

## Outbound queue size

`out_queue_size` is the number of messages the router queues for each client
to read. When a client's queue is full:

* Replies to the client's own requests, such as PUBLISHED, SUBSCRIBED,
  REGISTERED and RESULT, are never dropped. They wait until the client reads
  them, and the router holds the client's next request until there is room.
* A client that reads nothing for a minute while the router waits is
  disconnected.
* While the router holds a websocket client's request, it does not read the
  client's pongs, so `keep_alive` does not count missed pongs until the router
  takes the request.
* EVENTs, and the INVOCATIONs, INTERRUPTs and progressive RESULTs for the
  client, wait in its backlog, which holds up to the realm's `event_backlog` of
  them, by default as many as `out_queue_size`. They are delivered in order,
  with the client's replies, once the client reads.
* When the backlog is full too, the realm's `event_overflow` selects what
  happens to an EVENT. With `"drop"`, the default, the router drops the EVENT and logs
  each drop, and the client stays connected without knowing that it missed it.
  With `"close"`, the router closes the client, with a GOODBYE carrying
  `nexus.close.slow_subscriber` if there is room for it, so that the client
  knows that it fell behind and can reconnect.
* A call fails, and its caller gets an ERROR, if its INVOCATION does not fit
  in the callee's backlog (`wamp.error.network_failure`), or if the caller
  stays blocked on its progressive RESULTs (`wamp.error.canceled`).

Size the queue for bursts of EVENTs, INVOCATIONs and progressive RESULTs, not
for the number of requests a client has in flight. Size `event_backlog` for
the longest burst of EVENTs, INVOCATIONs and progressive RESULTs that a client
should be able to fall behind by and still receive all of them.
