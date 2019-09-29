package transport

import (
	"context"

	"github.com/gammazero/nexus/v3/wamp"
)

const linkedPeersOutQueueSize = 16

// LinkedPeers creates two connected peers.  Messages sent to one peer appear
// in the Recv of the other.  This is used for connecting client sessions to
// the router.
func LinkedPeers() (wamp.Peer, wamp.Peer) {
	// The channel used for the router to send messages to the client should be
	// large enough to prevent blocking while waiting for a slow client, as a
	// client may block on I/O.  If the client does block, then the message
	// should be dropped.
	rToC := make(chan wamp.Message, linkedPeersOutQueueSize)

	// The router will read from this channen and immediately dispatch the
	// message to the broker or dealer.  Therefore this channel can be
	// unbuffered.
	cToR := make(chan wamp.Message)

	// router reads from and writes to client
	r := &localPeer{rd: cToR, wr: rToC}
	// client reads from and writes to router
	c := &localPeer{rd: rToC, wr: cToR}

	return c, r
}

// IsLocal returns true is the wamp.Peer is a localPeer.  These do not need
// authentication since they are part of the same process.
func IsLocal(p wamp.Peer) bool {
	_, ok := p.(*localPeer)
	return ok
}

// localPeer implements Peer
type localPeer struct {
	rd <-chan wamp.Message
	wr chan<- wamp.Message
}

// Recv returns the channel this peer reads incoming messages from.
func (p *localPeer) Recv() <-chan wamp.Message { return p.rd }

// TrySend writes a message to the peer's outbound message channel.
func (p *localPeer) TrySend(msg wamp.Message) error {
	return wamp.TrySend(p.wr, msg)
}

func (p *localPeer) SendCtx(ctx context.Context, msg wamp.Message) error {
	return wamp.SendCtx(ctx, p.wr, msg)
}

// Send writes a message to the peer's outbound message channel.
// Typically called by clients, since it is OK for the router to block a client
// since this will not block other clients.
func (p *localPeer) Send(msg wamp.Message) error {
	p.wr <- msg
	return nil
}

// Close closes the outgoing channel, waking any readers waiting on data from
// this peer.
func (p *localPeer) Close() { close(p.wr) }
