package wamp

import (
	"fmt"
	"sync"
)

// Session is an active WAMP session. It associates a session ID and details
// with a connected Peer, which is the remote side of the session. So, if the
// session owned by the router, then the Peer is the connected client.
type Session struct {
	// Interface for communicating with connected peer.
	Peer
	// Unique session ID.
	ID ID
	// Details about session.
	Details Dict
	IDGen   SyncIDGen
	// Greatest value of Dealer's end of Session Scope ID. Use IsNewRecvID()
	// and UpdateLastRecvID() for comparison/update.
	lastRecvID ID

	// Roles and features supported by peer.
	roles map[string]map[string]struct{}

	mu      sync.Mutex
	done    chan struct{}
	goodbye *Goodbye
}

// NoGoodbye indicates that no Goodbye message was sent out
var NoGoodbye = &Goodbye{} //nolint:gochecknoglobals

// NewSession creates a new session. The greetDetails is the details from the
// HELLO or WELCOME message, from which roles and features are extracted.
func NewSession(peer Peer, id ID, details Dict, greetDetails Dict) *Session {
	s := &Session{
		Peer:    peer,
		ID:      id,
		Details: details,
	}
	s.setRoles(greetDetails)
	return s
}

// Lock locks the session to protect against concurrent updates.
func (s *Session) Lock() { s.mu.Lock() }

// Unlock unlocks the session.
func (s *Session) Unlock() { s.mu.Unlock() }

// String returns the session ID as a string.
func (s *Session) String() string { return fmt.Sprintf("%d", s.ID) }

// HasRole returns true if the session supports the specified role.
func (s *Session) HasRole(role string) bool {
	_, ok := s.roles[role]
	return ok
}

// HasFeature returns true if the session has the specified feature for the
// specified role.
func (s *Session) HasFeature(role, feature string) bool {
	features, ok := s.roles[role]
	if !ok {
		return false
	}
	_, ok = features[feature]
	return ok
}

// RecvDone returns a channel that is closed when this session has been ended
// by calling EndRecv.
func (s *Session) RecvDone() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done == nil {
		s.done = make(chan struct{})
	}
	return s.done
}

// If RecvDone is not yet closed, Goodbye returns nil. If RecvDone is closed,
// Goodbye returns the GOODBYE message that was supplied when RecvEnd was
// called.
func (s *Session) Goodbye() *Goodbye {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.goodbye
}

// EndRecv tells the session to signal messages handlers to stop receiving
// messages from this session. An optional goodbye message may be provided for
// the message handler to send to the peer. It is the responsibility of the
// message handler to send the goodbye message, so that this can be coordinated
// with exiting the message handler for other reasons.
func (s *Session) EndRecv(goodbye *Goodbye) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.goodbye != nil {
		return false // already ended
	}

	if goodbye == nil {
		s.goodbye = NoGoodbye
	} else {
		s.goodbye = goodbye
	}

	if s.done == nil {
		s.done = make(chan struct{})
	}
	close(s.done)

	return true
}

// setRoles extracts the specified roles from HELLO or WELCOME details, and
// configures the session with the roles and features for each role.
func (s *Session) setRoles(details Dict) {
	_roles, ok := details["roles"]
	if !ok {
		s.roles = nil // no roles
		return
	}
	roles, ok := AsDict(_roles)
	if !ok || len(roles) == 0 {
		s.roles = nil // no roles
		return
	}

	roleMap := make(map[string]map[string]struct{})
	for role, _roleDict := range roles {
		roleMap[role] = nil
		roleDict, ok := _roleDict.(Dict)
		if !ok {
			roleDict = NormalizeDict(_roleDict)
			if roleDict == nil {
				continue
			}
		}
		_features, ok := roleDict["features"]
		if !ok {
			continue
		}
		features, ok := _features.(Dict)
		if !ok {
			features = NormalizeDict(_features)
			if features == nil {
				continue
			}
		}
		featMap := make(map[string]struct{})
		for feature, iface := range features {
			if b, _ := iface.(bool); !b {
				continue
			}
			featMap[feature] = struct{}{}
		}
		roleMap[role] = featMap
	}
	s.roles = roleMap
}

// UpdateLastRecvID updates the Session's lastRecvID with IDs coming from the
// other end. Returns true if this is a new ID.
//
// This is our only way to track the session-based request IDs to determine if
// we're responding to a new request.
func (s *Session) UpdateLastRecvID(id ID) bool {
	s.Lock()
	defer s.Unlock()
	return s.UpdateLastRecvIDLocked(id)
}

// UpdateLastRecvIDLocked works just like UpdateLastRecvID but expects that the
// caller holds the session lock.
func (s *Session) UpdateLastRecvIDLocked(id ID) bool {
	if s.IsNewRecvID(id) {
		s.lastRecvID = id
		return true
	}
	return false
}

// IsNewRecvID returns true if the ID is considered new.
//
// It is not guaranteed that when lastRecvID is MaxID the next ID will be 1,
// because it is possible for a Dealer to skip IDs if there are internal errors
// or if it incorrectly uses Dealer scoped IDs instead of Session scoped. To
// maximize compatibility, allow ID rollover/wraparound as long as the
// wraparound distance is within an allowed limit.
//
// Call this function when the Session.Lock is held.
func (s *Session) IsNewRecvID(id ID) bool {
	const deltaID = ID(500) // maximum allowed wraparound distance

	last := s.lastRecvID

	// Have not seen any previous id, so must be new.
	if last == 0 {
		return true
	}

	if id == last || id == 0 {
		return false
	}
	// If the new id is less than the last id, and the wrap-around distance
	// from the last id to the new id is within delta, then this is a
	// legitimate new id within the allowed wraparound.
	if id < last {
		return (MaxID - (last - id)) < deltaID
	}

	// Can only advance at most deltaID.
	return id > last && (id-last) < deltaID
}
