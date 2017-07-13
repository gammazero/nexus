package router

import (
	"fmt"

	"github.com/gammazero/nexus/wamp"
)

const peerChanSize = 16

// Session is an active WAMP session.
type Session struct {
	wamp.Peer
	ID       wamp.ID
	AuthID   string
	AuthRole string
	Realm    wamp.URI
	Details  map[string]interface{}

	kill chan wamp.URI
}

// String returns the session ID as a string.
func (s Session) String() string { return fmt.Sprintf("%d", s.ID) }

// HasRole returns true if the session supports the specified role.
func (s Session) HasRole(role string) bool {
	roles, ok := s.Details["roles"]
	if !ok {
		fmt.Println("no roles")
		return false
	}

	switch roles := roles.(type) {
	case map[string]interface{}:
		_, ok = roles[role]
	case map[string]map[string]interface{}:
		_, ok = roles[role]
	case map[string]map[string]struct{}:
		_, ok = roles[role]
	default:
		ok = false
	}
	return ok
}

// HasFeature returns true if the session has the specified feature for the
// specified role.
func (s Session) HasFeature(role, feature string) bool {
	_roles, ok := s.Details["roles"]
	if !ok {
		// Session has no roles.
		return false
	}
	roles, ok := _roles.(map[string]map[string]interface{})
	if !ok {
		// type cannot support role data
		return false
	}
	roleData, ok := roles[role]
	if !ok {
		// Session does not support specified role.
		return false
	}
	if roleData == nil {
		// no data for the role
		return false
	}
	features, ok := roleData["features"]
	if !ok {
		// role has no features
		return false
	}

	switch features := features.(type) {
	case map[string]interface{}:
		_, ok = features[feature]
	case map[string]map[string]interface{}:
		_, ok = features[feature]
	case map[string]map[string]struct{}:
		_, ok = features[feature]
	default:
		ok = false
	}
	return ok
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
	// large enough to prevent blocking while waiting for a slow client. If the
	// client does block, then the message should be dropped and the client
	// removed.
	rToC := make(chan wamp.Message, 16)

	// Messages read from the websocket can be handled immediately, since
	// they have traveled over the websocket and the read channel does not
	// need to be more than size 1.
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
	// It is OK for the router to block a client since routing should be very
	// quick compared to the time to transfer a message over websocket, and a
	// blocked client will not block other clients.
	p.wr <- msg
}

// Close closes the outgoing channel, waking any readers waiting on data from
// this peer.
func (p *localPeer) Close() { close(p.wr) }
