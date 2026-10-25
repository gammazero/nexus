package transport

import (
	"time"

	"github.com/gammazero/nexus/v3/wamp"
)

// abortTimeout is how long a peer waits for its sender to stop, and then for
// the ABORT to be written, before it drops a connection on which it received
// a message that it cannot deserialize.
const abortTimeout = time.Second

// deserializeAbort returns the ABORT that a peer sends to the remote peer when
// it receives a message that it cannot deserialize, before dropping the
// connection. WAMP treats an error decoding a message as a protocol
// violation, which must be answered with ABORT wamp.error.protocol_violation.
func deserializeAbort(err error) *wamp.Abort {
	return &wamp.Abort{
		Details: wamp.Dict{"message": "cannot deserialize message: " + err.Error()},
		Reason:  wamp.ErrProtocolViolation,
	}
}
