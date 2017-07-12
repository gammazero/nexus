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
	aToB := make(chan wamp.Message, peerChanSize)
	bToA := make(chan wamp.Message, peerChanSize)

	// A reads from B and writes to B
	a := &localPeer{rd: bToA, wr: aToB}
	// B read from A and writes to A
	b := &localPeer{rd: aToB, wr: bToA}

	return a, b
}

// localPeer implements Peer
type localPeer struct {
	wr chan<- wamp.Message
	rd <-chan wamp.Message
}

// Recv returns the channel this peer reads incoming messages from.
func (p *localPeer) Recv() <-chan wamp.Message { return p.rd }

// Send write a message to the channel the peer sends outgoing messages to.
func (p *localPeer) Send(msg wamp.Message) { p.wr <- msg }

// Close closes the outgoing channel, waking any readers waiting on data from
// this peer.
func (p *localPeer) Close() { close(p.wr) }
