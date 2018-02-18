package wamp

import (
	"errors"
	"time"
)

// Peer is the interface implemented by endpoints communicating via WAMP.
type Peer interface {
	// Sends the message to the peer.
	Send(Message) error

	// TrySend performs a non-blocking send.  Returns error if blocked.
	TrySend(Message) error

	// Closes the peer connection and and channel returned from Recv().
	Close()

	// Recv returns a channel of messages from the peer.
	Recv() <-chan Message
}

// RecvTimeout receives a message from a peer within the specified time.
func RecvTimeout(p Peer, t time.Duration) (Message, error) {
	select {
	case msg, open := <-p.Recv():
		if !open {
			return nil, errors.New("receive channel closed")
		}
		return msg, nil
	case <-time.After(t):
		return nil, errors.New("timeout waiting for message")
	}
}
