# Getting Started

To get started using nexus as a stand-alone service, without writing any code, follow these steps:

1. [Install](#install-the-nexus-package) the nexus package.
2. [Build](#build-the-router) `nexusd`, the nexus router service.
3. [Configure](#configure-the-router) `nexusd` by editing the configuration file, if needed.
4. [Run](#run-the-router) the `nexusd` router service.

## Install the nexus package

```
git clone git@github.com:gammazero/nexus.git
```

## Build the Router

```
cd nexus/cmd/nexusd/
make build
```

## Configure the Router

Edit the file `cmd/nexusd/etc/nexus.json` to configure the router.
This is a JSON file that specifies settings for a router and for the realms within that router.
It is not necessary to include items that have an empty, `0`, or `false` value.
They are included in the default config so that you are aware of their presence.
The default config file looks like this:

```json
{
    "websocket": {
        "address": ":8080",
        "cert_file": "",
        "key_file": "",
        "keep_alive": 30,
        "enable_compression": false,
        "allow_origins": ["*"]
    },
    "rawsocket": {
        "tcp_address": "",
        "tcp_keepalive_interval": 180,
        "unix_address": "",
        "max_msg_len": 0,
        "cert_file": "",
        "key_file": ""
    },
    "log_path": "",
    "router": {
        "realms": [
            {
                "uri": "realm1",
                "strict_uri": false,
                "allow_disclose": true,
                "anonymous_auth": true,
                "debug": true,
                "meta_strict": false,
                "meta_include_session_details": [],
                "enable_meta_kill": false,
                "enable_meta_modify": false,
                "event_history": [
                    {
                        "topic": "exact.topic.uri",
                        "match": "exact",
                        "limit": 100
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
        "mem_stats_log_sec": 0
    }
}
```

If `log_path` is not specified or is "", then nexusd will log to stdout.

Automatic creation of realms is enabled by specifying a `realm_template` in the config file.
This means that when a client joins a realm, that the realm is automatically created within the router if it does not already exist.
Here is an example realm template config:

```json
{
    ...
    "router": {
        "realm_template": {
            "anonymous_auth": true,
            "allow_disclose": true
        }
    }
}
```

## Run the Router

Use the `-c` flag to give the path to the config file.
From the `cmd/nexusd` directory, run the router with the default config file:

```
./nexusd -c etc/nexus.json
```

nexusd does not read a config file unless `-c` is given.
Without a config file, it needs at least one of the `-ws`, `-tcp` or `-unix` flags to give an address to listen on, and the `-realm` flag to give the URI of a realm to create.
For example:

```
./nexusd -ws :8080 -realm realm1
```

The `-verbose` flag enables debug logging for every realm.
Run `./nexusd -h` to see all the flags.

## Configuration Details

The configuration file is loaded into the following structure:

```go
type Config struct {
	// Websocket configuration parameters.
	WebSocket struct {
		// String form of address (example, "192.0.2.1:25", "[2001:db8::1]:80")
		Address string `json:"address"`
		// Files containing a certificate and matching private key.
		CertFile string `json:"cert_file"`
		KeyFile  string `json:"key_file"`
		// Heartbeat ("pings") interval in seconds. Set to 0 to disable.
		KeepAlive time.Duration `json:"keep_alive"`
		// Enable per message write compression.
		EnableCompression bool `json:"enable_compression"`
		// Enable sending cookie to identify client in later connections.
		EnableTrackingCookie bool `json:"enable_tracking_cookie"`
		// Enable reading HTTP header from client requests.
		EnableRequestCapture bool `json:"enable_request_capture"`
		// Allow origins that match these glob patterns when an origin header
		// is present in the websocket upgrade request.
		AllowOrigins []string `json:"allow_origins"`
		// Limit on number of pending messages to send to each client. 0 uses the
		// default of 64. Replies to a client's own requests are never dropped when the
		// queue is full. EVENTs, INVOCATIONs, INTERRUPTs and progressive RESULTs wait
		// in a backlog, set by the realm's event_backlog and event_overflow. See
		// OutQueueSize in router.WebsocketServer for details.
		OutQueueSize int `json:"out_queue_size"`
	}

	// RawSocket configuration parameters.
	RawSocket struct {
		// String form of address (example, "192.0.2.1:25", "[2001:db8::1]:80")
		TCPAddress string `json:"tcp_address"`
		// TCP keepalive interval in seconds. Set to 0 to disable.
		TCPKeepAliveInterval time.Duration `json:"tcp_keepalive_interval"`
		// Path to Unix domain socket.
		UnixAddress string `json:"unix_address"`
		// Maximum message length server can receive. Default = 16M.
		MaxMsgLen int `json:"max_msg_len"`
		// Files containing a certificate and matching private key.
		CertFile string `json:"cert_file"`
		KeyFile  string `json:"key_file"`
		// Limit on number of pending messages to send to each client. 0 uses the
		// default of 64. Replies to a client's own requests are never dropped when the
		// queue is full. EVENTs, INVOCATIONs, INTERRUPTs and progressive RESULTs wait
		// in a backlog, set by the realm's event_backlog and event_overflow. See
		// OutQueueSize in router.RawSocketServer for details.
		OutQueueSize int `json:"out_queue_size"`
	}

	// File to write log data to. If not specified, log to stdout.
	LogPath string `json:"log_path"`
	// Router configuration parameters.
	// See https://pkg.go.dev/github.com/gammazero/nexus/v3/router#Config
	Router router.Config
}
```

Note: All boolean values default to `false` if not specified.

Within the `"router"` section of the config, the value `"realms"` is a list of realms to be configured on the router.
Each realm in the list has the form:

```json
{
    "uri": string,
    "strict_uri": boolean,
    "allow_disclose": boolean,
    "anonymous_auth": boolean,
    "debug": boolean,
    "require_local_auth": boolean,
    "require_local_authz": boolean,
    "meta_strict": boolean,
    "meta_include_session_details": [string, ..],
    "enable_meta_kill": boolean,
    "enable_meta_modify": boolean,
    "strict_request_ids": boolean,
    "event_backlog": number,
    "event_overflow": string,
    "event_history": [{"topic": string, "match": string, "limit": number}, ..]
}
```

Documentation of these options is in the godoc reference for [`RealmConfig`](https://pkg.go.dev/github.com/gammazero/nexus/v3/router#RealmConfig), and in the [nexusd README](../cmd/nexusd/README.md).
