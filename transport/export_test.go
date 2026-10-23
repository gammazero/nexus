package transport

import "time"

// SetSenderStopTimeout sets how long websocket and rawsocket peers created
// afterwards wait for their sender to exit before closing the connection. It
// returns a function that restores the previous value.
func SetSenderStopTimeout(d time.Duration) func() {
	old := senderStopTimeout
	senderStopTimeout = d
	return func() { senderStopTimeout = old }
}
