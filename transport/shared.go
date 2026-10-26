package transport

import "github.com/gammazero/nexus/v3/wamp"

// AcceptsSharedMessage reports whether peer accepts a
// *serialize.SharedMessage, which it serializes with the bytes already
// serialized for other peers. The websocket and rawsocket peers do. Other
// peers, such as local peers, which pass messages on without serializing them,
// and peers implemented outside this package, must be sent the message
// itself.
func AcceptsSharedMessage(peer wamp.Peer) bool {
	switch peer.(type) {
	case *websocketPeer, *rawSocketPeer:
		return true
	}
	return false
}
