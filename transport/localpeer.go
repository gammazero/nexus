package transport

import (
	"sync"

	"github.com/gammazero/nexus/v3/wamp"
)

const defaultRToCQueueSize = 64

// LinkedPeers creates two connected peers. Messages sent to one peer appear in
// the Recv of the other. This is used for connecting client sessions to the
// router.
func LinkedPeers() (wamp.Peer, wamp.Peer) {
	return LinkedPeersQSize(defaultRToCQueueSize)
}

// LinkedPeersQSize is the same as LinkedPeers with the ability to specify the
// router-to-client queue size. Specifying size 0 uses default size.
func LinkedPeersQSize(queueSize int) (wamp.Peer, wamp.Peer) {
	if queueSize == 0 {
		queueSize = defaultRToCQueueSize
	}

	// The channel used for the router to send messages to the client should be
	// large enough to prevent blocking while waiting for a slow client, as a
	// client may block on I/O. If the client does block, then the message
	// should be dropped.
	rToC := make(chan wamp.Message, queueSize)

	// The router will read from this channel and immediately dispatch the
	// message to the broker or dealer. Therefore, this channel can be
	// unbuffered.
	cToR := make(chan wamp.Message)

	// Closing either peer means the two can no longer exchange messages.
	done := &linkDone{ch: make(chan struct{})}

	// router reads from and writes to client
	r := &localPeer{rd: cToR, wr: rToC, done: done}
	// client reads from and writes to router
	c := &localPeer{rd: rToC, wr: cToR, done: done}

	return c, r
}

// linkDone is closed when either of two linked peers is closed.
type linkDone struct {
	ch   chan struct{}
	once sync.Once
}

func (d *linkDone) close() { d.once.Do(func() { close(d.ch) }) }

// localPeer implements Peer
type localPeer struct {
	rd   <-chan wamp.Message
	wr   chan<- wamp.Message
	done *linkDone

	closeOnce sync.Once
}

// IsLocal returns true is the wamp.Peer is a localPeer.
func (p *localPeer) IsLocal() bool { return true }

// Recv returns the channel this peer reads incoming messages from.
func (p *localPeer) Recv() <-chan wamp.Message { return p.rd }

// Send returns the peer's outbound message channel.
func (p *localPeer) Send() chan<- wamp.Message { return p.wr }

// Done returns a channel that is closed when this peer or the peer linked to
// it is closed, after which the two cannot exchange messages.
func (p *localPeer) Done() <-chan struct{} { return p.done.ch }

// Close closes the outgoing channel, waking any readers waiting on data from
// this peer. Calling Close more than once, including concurrently, is safe.
func (p *localPeer) Close() {
	p.closeOnce.Do(func() {
		close(p.wr)
		p.done.close()
	})
}
