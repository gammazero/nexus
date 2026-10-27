package wamp

import (
	"fmt"
	"sync"

	"github.com/gammazero/deque"
)

const deltaID = ID(500) // maximum allowed wraparound distance

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
	abort   *Abort

	// Messages from SendReply that wait for room in the peer's send queue.
	sendMu      sync.Mutex
	backlog     deque.Deque[Message]
	backlogRoom chan struct{}
	backlogSent uint64
	sendStop    chan struct{}
	sendClosed  bool
	sendWG      sync.WaitGroup
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

// EndRecvAbort ends the session like EndRecv, but for the message handler to
// send the peer abort instead of a GOODBYE, for example because the peer
// violated the protocol. It returns false if the session was already ended.
//
// Use this, instead of closing the peer, to abort a session from code other
// than its message handler, so that the message handler can stop routing
// messages to the session before the peer is closed.
func (s *Session) EndRecvAbort(abort *Abort) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.goodbye != nil {
		return false // already ended
	}
	s.goodbye = NoGoodbye
	s.abort = abort

	if s.done == nil {
		s.done = make(chan struct{})
	}
	close(s.done)

	return true
}

// Abort returns the ABORT message supplied to EndRecvAbort, or nil if the
// session was not ended by EndRecvAbort.
func (s *Session) Abort() *Abort {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.abort
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
	// Reject invalid IDs: WAMP request IDs must be > 0 and <= MaxID.
	if id == 0 || id > MaxID {
		return false
	}

	last := s.lastRecvID

	// Have not seen any previous id, so any valid id must be new.
	if last == 0 {
		return true
	}
	// For forward progress, accept any strictly increasing ID.
	if id > last {
		return true
	}
	if id == last {
		return false
	}
	// If the new id is less than the last id, and the wrap-around distance
	// from the last id to the new id is within delta, then this is a
	// legitimate new id within the allowed wraparound.
	return (MaxID - (last - id)) < deltaID
}

// TrySend sends the message to the peer without blocking. It returns false,
// and the message is dropped, if the peer's send queue is full or messages
// from SendReply are still waiting to be sent. Use TrySend for messages that
// the peer can do without, such as events for a slow subscriber.
func (s *Session) TrySend(msg Message) bool {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	if s.sendClosed || s.backlog.Len() != 0 {
		return false
	}
	select {
	case s.Send() <- msg:
		return true
	default:
		return false
	}
}

// SendReply sends the message to the peer without blocking and without
// dropping it. If the peer's send queue is full, the message waits in a
// backlog and is sent, in order, as soon as the queue has room. Use SendReply
// for replies to the peer's own requests, which the peer cannot tell are
// missing.
//
// The backlog is not bounded here. Callers bound it by not making requests
// on behalf of the peer while ReplyRoom returns a non-nil channel.
func (s *Session) SendReply(msg Message) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	if s.sendClosed {
		return
	}
	if s.backlog.Len() == 0 {
		select {
		case s.Send() <- msg:
			return
		default:
		}
		if s.sendStop == nil {
			s.sendStop = make(chan struct{})
		}
		s.sendWG.Add(1)
		go s.sendBacklog(s.sendStop)
	}
	// The sendBacklog goroutine cannot read the backlog until sendMu is
	// released, after msg is added.
	s.backlog.PushBack(msg)
}

// ReplyRoom returns nil if the backlog of messages from SendReply is smaller
// than the peer's send queue. Otherwise, it returns a channel that is closed
// when the backlog becomes smaller than the send queue, or when the session
// is closed.
func (s *Session) ReplyRoom() <-chan struct{} {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	if s.sendClosed || s.backlog.Len() < s.backlogLimit() {
		return nil
	}
	if s.backlogRoom == nil {
		s.backlogRoom = make(chan struct{})
	}
	return s.backlogRoom
}

// PeerDone returns the channel from Done if the session's peer is a DonePeer,
// and nil otherwise. Receiving from a nil channel blocks forever, so a caller
// can select on the result either way.
func (s *Session) PeerDone() <-chan struct{} {
	if p, ok := s.Peer.(DonePeer); ok {
		return p.Done()
	}
	return nil
}

// BacklogSent returns the number of messages sent from the backlog of messages
// from SendReply. A caller waiting on ReplyRoom can use it to tell whether the
// peer is still reading.
func (s *Session) BacklogSent() uint64 {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.backlogSent
}

// StopSending stops sending the backlog of messages from SendReply, discarding
// any that remain, and returns once nothing more can be sent from it. After
// that, SendReply and TrySend send nothing. Call StopSending before sending
// the GOODBYE or ABORT that ends the session, so that no reply follows it.
func (s *Session) StopSending() {
	s.sendMu.Lock()
	if !s.sendClosed {
		s.sendClosed = true
		if s.sendStop != nil {
			close(s.sendStop)
		}
		if s.backlogRoom != nil {
			close(s.backlogRoom)
			s.backlogRoom = nil
		}
		s.backlog.Clear()
	}
	s.sendMu.Unlock()

	s.sendWG.Wait()
}

// Close calls StopSending and then closes the peer.
func (s *Session) Close() {
	s.StopSending()
	s.Peer.Close()
}

// backlogLimit is the backlog size at which ReplyRoom reports that there is
// no room. It is the capacity of the peer's send queue, so that a peer that
// keeps up with its replies has at most twice that many messages pending.
//
// Call this function when sendMu is held.
func (s *Session) backlogLimit() int {
	return max(cap(s.Send()), 1)
}

// sendBacklog sends messages from the backlog to the peer, blocking until the
// peer's send queue has room for each, until the backlog is empty or the
// session is closed. A message stays at the front of the backlog until it is
// sent, so that TrySend and SendReply do not send ahead of it.
func (s *Session) sendBacklog(stop <-chan struct{}) {
	defer s.sendWG.Done()
	for {
		s.sendMu.Lock()
		if s.sendClosed {
			// Close cleared the backlog.
			s.sendMu.Unlock()
			return
		}
		msg := s.backlog.Front()
		s.sendMu.Unlock()

		select {
		case s.Send() <- msg:
		case <-stop:
			return
		}

		s.sendMu.Lock()
		if s.sendClosed {
			s.sendMu.Unlock()
			return
		}
		s.backlog.PopFront()
		s.backlogSent++
		n := s.backlog.Len()
		if s.backlogRoom != nil && n < s.backlogLimit() {
			close(s.backlogRoom)
			s.backlogRoom = nil
		}
		s.sendMu.Unlock()
		if n == 0 {
			return
		}
	}
}
