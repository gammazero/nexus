package wamp

import (
	"fmt"
	"sync"

	"github.com/gammazero/deque"
)

const deltaID = ID(500) // maximum allowed wraparound distance

// Session is an active WAMP session. It associates a session ID and details
// with a connected Peer, which is the remote side of the session. So, if the
// session is owned by the router, then the Peer is the connected client.
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

	// Messages from SendReply and SendLimited that wait for room in the peer's
	// send queue, the number of them that are from SendLimited, and the most
	// of those allowed, where 0 means the capacity of the send queue.
	sendMu         sync.Mutex
	backlog        deque.Deque[backlogMsg]
	backlogLimited int
	sendLimit      int
	backlogRoom    chan struct{}
	backlogSent    uint64
	sendStop       chan struct{}
	sendClosed     bool
	sendWG         sync.WaitGroup
}

// backlogMsg is a message in the backlog. The limited field is true if the
// message is from SendLimited.
type backlogMsg struct {
	msg     Message
	limited bool
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

// Roles returns the roles and features that the session declared in its HELLO
// or WELCOME details, the same ones that HasRole and HasFeature report, as a
// new Dict that maps each role name to a role Dict. The caller can read or
// change the result without holding the session lock and without affecting the
// session.
//
// A role Dict has a "features" Dict that maps each feature whose value was
// true to true. Features whose value was not true are left out, and a role
// whose value or "features" value was not a dict has no "features" entry.
// Other entries of a role's dict are not kept. Roles returns nil if the
// session declared no roles.
func (s *Session) Roles() Dict {
	if len(s.roles) == 0 {
		return nil
	}
	roles := make(Dict, len(s.roles))
	for role, features := range s.roles {
		roleDict := Dict{}
		if features != nil {
			featDict := make(Dict, len(features))
			for feature := range features {
				featDict[feature] = true
			}
			roleDict["features"] = featDict
		}
		roles[role] = roleDict
	}
	return roles
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
// Goodbye returns the GOODBYE message that was supplied when EndRecv was
// called.
func (s *Session) Goodbye() *Goodbye {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.goodbye
}

// EndRecv tells the session to signal message handlers to stop receiving
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
// and the message is dropped, if the peer's send queue is full or messages in
// the backlog are still waiting to be sent. Use TrySend for messages that the
// peer can do without, and that must not wait, such as an ERROR for a YIELD
// from a callee that may send them without reading.
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
	}
	s.pushBacklog(msg, false)
}

// SendLimited sends the message to the peer without blocking. If the peer's
// send queue is full, the message waits in the backlog and is sent, in order
// with the messages from SendReply, as soon as the queue has room. It returns
// false, and the message is dropped, if the backlog already holds the limit
// set by SetSendLimit of messages from SendLimited, or if the session is
// closed. Use SendLimited for messages that the router can drop or fail, but
// that the peer should get if it is only slow for a while, such as EVENTs and
// INVOCATIONs.
func (s *Session) SendLimited(msg Message) bool {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	if s.sendClosed {
		return false
	}
	if s.backlog.Len() == 0 {
		select {
		case s.Send() <- msg:
			return true
		default:
		}
	}
	limit := s.sendLimit
	if limit == 0 {
		limit = s.backlogLimit()
	}
	if s.backlogLimited >= limit {
		return false
	}
	s.pushBacklog(msg, true)
	return true
}

// SetSendLimit sets the most messages from SendLimited that may wait in the
// backlog. A limit of zero, the default, uses the capacity of the peer's send
// queue.
func (s *Session) SetSendLimit(limit int) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.sendLimit = limit
}

// pushBacklog adds msg to the end of the backlog, and starts the sendBacklog
// goroutine if the backlog was empty.
//
// Call this function when sendMu is held.
func (s *Session) pushBacklog(msg Message, limited bool) {
	if s.backlog.Len() == 0 {
		if s.sendStop == nil {
			s.sendStop = make(chan struct{})
		}
		s.sendWG.Add(1)
		go s.sendBacklog(s.sendStop)
	}
	// The sendBacklog goroutine cannot read the backlog until sendMu is
	// released, after msg is added.
	s.backlog.PushBack(backlogMsg{msg: msg, limited: limited})
	if limited {
		s.backlogLimited++
	}
}

// ReplyRoom returns nil if the number of messages from SendReply in the
// backlog is smaller than the peer's send queue. Otherwise, it returns a
// channel that is closed when it becomes smaller than the send queue, or when
// the session is closed. Messages from SendLimited do not count, since
// SendLimited bounds them itself.
func (s *Session) ReplyRoom() <-chan struct{} {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	if s.sendClosed || s.backlogReplies() < s.backlogLimit() {
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

// BacklogSent returns the number of messages sent from the backlog. A caller
// waiting on ReplyRoom can use it to tell whether the peer is still reading.
func (s *Session) BacklogSent() uint64 {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.backlogSent
}

// StopSending stops sending the backlog, discarding any messages that remain,
// and returns once nothing more can be sent from it. After that, SendReply,
// SendLimited and TrySend send nothing. Call StopSending before sending
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
		s.backlogLimited = 0
	}
	s.sendMu.Unlock()

	s.sendWG.Wait()
}

// Close calls StopSending and then closes the peer.
func (s *Session) Close() {
	s.StopSending()
	s.Peer.Close()
}

// backlogLimit is the number of messages from SendReply in the backlog at
// which ReplyRoom reports that there is no room. It is the capacity of the
// peer's send queue, so that a peer that keeps up with its replies has at most
// twice that many replies pending.
//
// Call this function when sendMu is held.
func (s *Session) backlogLimit() int {
	return max(cap(s.Send()), 1)
}

// backlogReplies returns the number of messages from SendReply in the
// backlog.
//
// Call this function when sendMu is held.
func (s *Session) backlogReplies() int {
	return s.backlog.Len() - s.backlogLimited
}

// sendBacklog sends messages from the backlog to the peer, blocking until the
// peer's send queue has room for each, until the backlog is empty or the
// session is closed. A message stays at the front of the backlog until it is
// sent, so that TrySend, SendReply and SendLimited do not send ahead of it.
func (s *Session) sendBacklog(stop <-chan struct{}) {
	defer s.sendWG.Done()
	for {
		s.sendMu.Lock()
		if s.sendClosed {
			// Close cleared the backlog.
			s.sendMu.Unlock()
			return
		}
		front := s.backlog.Front()
		s.sendMu.Unlock()

		select {
		case s.Send() <- front.msg:
		case <-stop:
			return
		}

		s.sendMu.Lock()
		if s.sendClosed {
			s.sendMu.Unlock()
			return
		}
		s.backlog.PopFront()
		if front.limited {
			s.backlogLimited--
		}
		s.backlogSent++
		n := s.backlog.Len()
		if s.backlogRoom != nil && s.backlogReplies() < s.backlogLimit() {
			close(s.backlogRoom)
			s.backlogRoom = nil
		}
		s.sendMu.Unlock()
		if n == 0 {
			return
		}
	}
}
