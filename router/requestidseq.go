package router

import (
	"fmt"

	"github.com/gammazero/nexus/v3/wamp"
)

// requestIDSeq checks that the request IDs a client allocates are session
// scope IDs: they start at 1, increment by 1, and wrap to 1 after wamp.MaxID.
//
// The CALL messages that continue a progressive call reuse the request ID of
// the first CALL, so a CALL with an ID that was already used is a continuation
// candidate rather than a protocol violation. The dealer decides whether it
// continues a progressive call that is still in progress.
type requestIDSeq struct {
	// last is the last request ID allocated by the client, or 0 if none.
	last wamp.ID
}

// check checks the request ID of a message that allocates one. isCall is true
// if the message is a CALL. It returns true if the message is a continuation
// candidate, and an error if the ID is out of sequence.
func (s *requestIDSeq) check(id wamp.ID, isCall bool) (bool, error) {
	next := s.last + 1
	if next > wamp.MaxID {
		next = 1
	}
	if id == next {
		s.last = id
		return false, nil
	}
	if isCall && id != 0 && id < next {
		return true, nil
	}
	return false, fmt.Errorf("non-sequential session scope request ID: got %d, want %d",
		id, next)
}
