package wamp

import "fmt"

// Session is an active WAMP session.  It associates a session ID and details
// with a connected Peer.
type Session struct {
	// Interface for communicating with connected peer.
	Peer
	// Unique session ID.
	ID ID
	// Details about session.
	Details Dict
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
