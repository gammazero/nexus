package wamp

import (
	"fmt"
	"sync"
)

// Session is an active WAMP session.  It associates a session ID and details
// with a connected Peer, which is the remote side of the session.  So, if the
// session owned by the router, then the Peer is the connected client.
type Session struct {
	// Interface for communicating with connected peer.
	Peer
	// Unique session ID.
	ID ID
	// Details about session.
	Details Dict

	mu      sync.Mutex
	done    chan struct{}
	goodbye *Goodbye
}

// closedchan is a reusable closed channel.
var closedchan = make(chan struct{})

func init() {
	close(closedchan)
}

// String returns the session ID as a string.
func (s Session) String() string { return fmt.Sprintf("%d", s.ID) }

// HasRole returns true if the session supports the specified role.
func (s Session) HasRole(role string) bool {
	_, err := DictValue(s.Details, []string{"roles", role})
	return err == nil
}

// HasFeature returns true if the session has the specified feature for the
// specified role.
func (s Session) HasFeature(role, feature string) bool {
	b, _ := DictFlag(s.Details, []string{"roles", role, "features", feature})
	return b
}

func (s *Session) Done() <-chan struct{} {
	s.mu.Lock()
	if s.done == nil {
		s.done = make(chan struct{})
	}
	d := s.done
	s.mu.Unlock()
	return d
}

func (s *Session) Goodbye() *Goodbye {
	s.mu.Lock()
	g := s.goodbye
	s.mu.Unlock()
	return g
}

func (s *Session) Kill(goodbye *Goodbye) bool {
	s.mu.Lock()
	if s.goodbye != nil {
		s.mu.Unlock()
		return false // already killed
	}
	s.goodbye = goodbye
	if s.done == nil {
		s.done = closedchan
	} else {
		close(s.done)
	}
	s.mu.Unlock()
	return true
}
