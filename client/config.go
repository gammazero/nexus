package client

import (
	"crypto/tls"
	"time"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
)

// Config configures a client with everything needed to begin a session
// with a WAMP router.
type Config struct {
	// Realm is the URI of the realm the client will join.
	Realm string

	// HelloDetails contains details about the client.  The client provides the
	// roles, unless already supplied by the user.
	HelloDetails wamp.Dict

	// AuthHandlers is a map of authmethod to AuthFunc.  All authmethod keys
	// from this map are automatically added to HelloDetails["authmethods"]
	AuthHandlers map[string]AuthFunc

	// ResponseTimeout specifies the amount of time that the client will block
	// waiting for a response from the router.  A value of 0 uses the default.
	ResponseTimeout time.Duration

	// Enable debug logging for client.
	Debug bool

	// Set to JSON or MSGPACK.  Default (zero-value) is JSON.
	Serialization serialize.Serialization

	// Provide a tls.Config to connect the client using TLS.  The zero
	// configuration specifies using defaults.  A nil tls.Config means do not
	// use TLS.
	TlsCfg *tls.Config

	// Supplies alternate Dial function for the websocket dialer.
	// See https://godoc.org/github.com/gorilla/websocket#Dialer
	Dial transport.DialFunc

	// Client receive limit for use with RawSocket transport.
	// If recvLimit is > 0, then the client will not receive messages with size
	// larger than the nearest power of 2 greater than or equal to recvLimit.
	// If recvLimit is <= 0, then the default of 16M is used.
	RecvLimit int

	// Logger for client to use.  If not set, client logs to os.Stderr.
	Logger stdlog.StdLog

	// Websocket transport configuration.
	WsCfg transport.WebsocketConfig
}

// Deprecated: replaced by Config
//
// ClientConfig is a type alias for the deprecated ClientConfig.
type ClientConfig = Config
