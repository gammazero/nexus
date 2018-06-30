package router

import (
	"sync"

	"github.com/gammazero/nexus/wamp"
)

// session is a wrapper around a wamp.Session to provide the router with a
// lockable session.
type session struct {
	wamp.Session

	rwlock sync.RWMutex
}

// newSession created a new lockable session.
func newSession(peer wamp.Peer, sid wamp.ID, details wamp.Dict) *session {
	return &session{
		Session: wamp.Session{
			Peer:    peer,
			ID:      sid,
			Details: details,
		},
	}
}

func (s *session) rLock()   { s.rwlock.RLock() }
func (s *session) rUnlock() { s.rwlock.RUnlock() }
func (s *session) lock()    { s.rwlock.Lock() }
func (s *session) unlock()  { s.rwlock.Unlock() }

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
