package transport

import (
	"fmt"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/wamp"
)

const linkedPeersOutQueueSize = 16

// LinkedPeers creates two connected peers.  Messages sent to one peer
// appear in the Recv of the other.
//
// This is used for connecting client sessions to the router.
//
// Exported since it is used in test code for creating in-process test clients.
func LinkedPeers(logger stdlog.StdLog) (wamp.Peer, wamp.Peer) {
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
	r := &localPeer{rd: cToR, wr: rToC, log: logger}
	// client reads from and writes to router
	c := &localPeer{rd: rToC, wr: cToR, log: logger}

	return c, r
}

// localPeer implements Peer
type localPeer struct {
	wr  chan<- wamp.Message
	rd  <-chan wamp.Message
	log stdlog.StdLog
}

// Recv returns the channel this peer reads incoming messages from.
func (p *localPeer) Recv() <-chan wamp.Message { return p.rd }

// Send write a message to the channel the peer sends outgoing messages to.
func (p *localPeer) Send(msg wamp.Message) error {
	// If capacity is > 0, then wr is the rToC channel.
	if cap(p.wr) > 1 {
		select {
		case p.wr <- msg:
		default:
			err := fmt.Errorf("client blocked - dropped %s", msg.MessageType())
			p.log.Println("!!!", err)
			return err
		}
		return nil
	}
	// It is OK for the router to block a client since this will not block
	// other clients.
	p.wr <- msg
	return nil
}

// Close closes the outgoing channel, waking any readers waiting on data from
// this peer.
func (p *localPeer) Close() { close(p.wr) }
