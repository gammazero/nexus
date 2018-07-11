package router

import (
	"sync"

	"github.com/gammazero/nexus/wamp"
)

// session is a wrapper around a wamp.session to provide the router with a
// lockable killable session.
type session struct {
	wamp.Session

	killChan chan *wamp.Goodbye
	rwlock   sync.RWMutex
}

// newSession created a new lockable session.
func newSession(peer wamp.Peer, sid wamp.ID, details wamp.Dict) *session {
	return &session{
		Session: wamp.Session{
			Peer:    peer,
			ID:      sid,
			Details: details,
		},
		killChan: make(chan *wamp.Goodbye),
	}
}

func (s *session) RLock()   { s.rwlock.RLock() }
func (s *session) RUnlock() { s.rwlock.RUnlock() }
func (s *session) Lock()    { s.rwlock.Lock() }
func (s *session) Unlock()  { s.rwlock.Unlock() }

func (s *session) Kill(goodbye *wamp.Goodbye) bool {
	if s.killChan == nil {
		return false
	}
	if goodbye == nil {
		close(s.killChan)
	} else {
		s.killChan <- goodbye
	}
	s.killChan = nil // prevent subsequent kill from using chan
	return true
}

// String returns the session ID as a string.
func (s *session) String() string { return s.Session.String() }

// HasRole returns true if the session supports the specified role.
func (s *session) HasRole(role string) bool {
	s.rwlock.RLock()
	ok := s.Session.HasRole(role)
	s.rwlock.RUnlock()
	return ok
}

// HasFeature returns true if the session has the specified feature for the
// specified role.
func (s *session) HasFeature(role, feature string) bool {
	s.rwlock.RLock()
	ok := s.Session.HasFeature(role, feature)
	s.rwlock.RUnlock()
	return ok
}
