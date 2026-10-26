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

// SetRawSocketWriteTimeout sets how long a write by rawsocket peers created
// afterwards may take before it fails. It returns a function that restores the
// previous value.
func SetRawSocketWriteTimeout(d time.Duration) func() {
	old := rawSocketWriteTimeout
	rawSocketWriteTimeout = d
	return func() { rawSocketWriteTimeout = old }
}
