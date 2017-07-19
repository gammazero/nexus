package router

import (
	"fmt"

	"github.com/gammazero/nexus/wamp"
)

const peerChanSize = 16

// Session is an active WAMP session.
type Session struct {
	wamp.Peer
	ID      wamp.ID
	Details map[string]interface{}

	stop chan wamp.URI
}

// String returns the session ID as a string.
func (s Session) String() string { return fmt.Sprintf("%d", s.ID) }

// HasRole returns true if the session supports the specified role.
func (s Session) HasRole(role string) bool {
	_, err := wamp.DictValue(s.Details, []string{"roles", role})
	return err == nil
}

// HasFeature returns true if the session has the specified feature for the
// specified role.
func (s Session) HasFeature(role, feature string) bool {
	b, _ := wamp.DictFlag(s.Details, []string{"roles", role, "features", feature})
	return b
}

// CheckFeature returns true all the sessions have the feature for the role.
func CheckFeature(role, feature string, sessions ...Session) bool {
	if len(sessions) == 0 {
		return false
	}
	for i := range sessions {
		if !sessions[i].HasFeature(role, feature) {
			return false
		}
	}
	return true
}

// LinkedPeers creates two connected peers.  Messages sent to one peer
// appear in the Recv of the other.
//
// This is used for connecting client sessions to the router.
//
// Exported since it is used in test code for creating in-process test clients.
func LinkedPeers() (*localPeer, *localPeer) {
	// The channel used for the router to send messages to the client should be
	// large enough to prevent blocking while waiting for a slow client, as a
	// client may block on I/O.  If the client does block, then the message
	// should be dropped.
	rToC := make(chan wamp.Message, 16)

	// Messages read from a client can usually be handled immediately, since
	// routing is fast and does not block on I/O.  Therefore this channle does
	// not need to be more than size 1.
	cToR := make(chan wamp.Message, 1)

	// router reads from and writes to client
	r := &localPeer{rd: cToR, wr: rToC, wrRtoC: true}
	// client reads from and writes to router
	c := &localPeer{rd: rToC, wr: cToR}

	return c, r
}

// localPeer implements Peer
type localPeer struct {
	wr     chan<- wamp.Message
	rd     <-chan wamp.Message
	wrRtoC bool
}

// Recv returns the channel this peer reads incoming messages from.
func (p *localPeer) Recv() <-chan wamp.Message { return p.rd }

// Send write a message to the channel the peer sends outgoing messages to.
func (p *localPeer) Send(msg wamp.Message) {
	if p.wrRtoC {
		select {
		case p.wr <- msg:
		default:
			fmt.Println("WARNING: client blocked router.  Dropped:",
				msg.MessageType())
		}
		return
	}
	// It is OK for the router to block a client since this will not block
	// other clients.
	p.wr <- msg
}

// Close closes the outgoing channel, waking any readers waiting on data from
// this peer.
func (p *localPeer) Close() { close(p.wr) }
