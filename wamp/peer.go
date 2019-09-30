package wamp

import (
	"context"
	"errors"
	"time"
)

// Peer is the interface implemented by endpoints communicating via WAMP.
type Peer interface {
	// Sends the message to the peer.
	Send(Message) error

	// SendCtx sends the message to the peer, and uses a context to cancel or
	// timeout sending the message when blocked waiting to write to the peer.
	SendCtx(context.Context, Message) error

	// TrySend performs a non-blocking send.  Returns error if blocked.
	TrySend(Message) error

	// Closes the peer connection and the channel returned from Recv().
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

// SendCtx sends a message to the write-only channel, using a context to cancel
// sending if blocked.
func SendCtx(ctx context.Context, wr chan<- Message, msg Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case wr <- msg:
	}
	return nil
}

// TrySend sends a message to the write-only channel and returns an error if
// the channel blocks.
func TrySend(wr chan<- Message, msg Message) error {
	select {
	case wr <- msg:
	default:
		return errors.New("blocked")
	}
	return nil
}
