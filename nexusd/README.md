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
        "allow_origins": ["*"]        // For CORS checking
    },
    "rawsocket": {
        "tcp_address": "", // Listen address. Can be in form of hostname:port, IP:port, :port (this means to listen on all interfaces)
        "tcp_keepalive_interval": 180,
        "unix_address": "", // Unix socket path to listen on. It is posible to listen both: tcp and unix socket
        "max_msg_len": 0,   // Maximum message length server can receive. Default = 16M.
        "cert_file": "",    // Specify TLS Certificate file and key paths
        "key_file": "",     // to enable secure connections
        "out_queue_size": 0 // Limit on number of pending messages to send to each client. 0 - disable queue.
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
        "mem_stats_log_sec": 0  // Interval in seconds for logging memory stats.  O to disable.
	                            // Logs Alloc, Mallocs, Frees, and NumGC.
    }
}
```
