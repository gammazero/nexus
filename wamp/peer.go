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
