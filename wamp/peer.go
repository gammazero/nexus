package wamp

import (
	"errors"
	"time"
)

// Peer is the interface implemented by endpoints communicating via WAMP.
type Peer interface {
	// Closes the peer connection and the channel returned from Recv().
	Close()

	// IsLocal returns true if the session is local.
	IsLocal() bool

	// Recv returns a channel of messages from the peer.
	Recv() <-chan Message

	// Send returns the peer's outgoing message channel.
	Send() chan<- Message
}

// DonePeer is a Peer that reports when it can no longer exchange messages with
// the remote side, because the connection was lost or either side closed it.
// The router uses this to end a session that it is not reading messages from,
// such as while it waits for the session to read its replies, when the remote
// side goes away. For a Peer that does not implement DonePeer, the router does
// not see the remote side go away during such a wait, and ends the session
// only when the wait times out.
type DonePeer interface {
	Peer

	// Done returns a channel that is closed when the peer can no longer exchange
	// messages with the remote side.
	Done() <-chan struct{}
}

// RecvTimeout receives a message from a peer within the specified time.
func RecvTimeout(p Peer, timeout time.Duration) (Message, error) {
	to := time.NewTimer(timeout)
	defer to.Stop()

	select {
	case msg, open := <-p.Recv():
		if !open {
			return nil, errors.New("receive channel closed")
		}
		return msg, nil
	case <-to.C:
		return nil, errors.New("timeout waiting for message")
	}
}
