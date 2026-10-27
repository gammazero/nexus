package wamp //nolint:testpackage

import (
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

func TestIsNewRecvIDBounds(t *testing.T) {
	testCases := [...]struct {
		last     ID
		id       ID
		expected bool
	}{
		{last: 0, id: 1, expected: true},
		{last: 1, id: 0, expected: false},
		{last: 1, id: 1, expected: false},
		{last: ID(MaxID), id: 1, expected: true},        // rollover
		{last: MaxID - 250, id: 1, expected: true},      // rollover w/ fudge
		{last: MaxID - deltaID, id: 1, expected: false}, // not yet a rollover
		// valid for rollover but new value is too far out of bounds
		{last: MaxID - deltaID + 100, id: deltaID + 100, expected: false},
		{last: MaxID - deltaID + 1, id: 1, expected: false},
		{last: 2, id: 1, expected: false},
		// just within bounds after rollover
		{last: MaxID - deltaID + 2, id: 1, expected: true},
		// id is technically out of bounds but that's OK
		{last: ID(MaxID), id: deltaID - 1, expected: true},
		// Jump forward any amount.
		{last: 1, id: MaxID - deltaID + 1, expected: true},
		{last: 100, id: 100 + deltaID, expected: true},
		// illegal id values
		{last: MaxID, id: MaxID + 1, expected: false},
		{last: MaxID, id: 0, expected: false},
	}

	s := Session{lastRecvID: 0}
	for i, tc := range testCases {
		name := fmt.Sprintf("%02d_%d-%d-%v", i, tc.last, tc.id, tc.expected)
		t.Run(name, func(t *testing.T) {
			s.lastRecvID = tc.last
			require.Equal(t, tc.expected, s.IsNewRecvID(tc.id))
		})
	}
}

func TestSessionEndRecvAbort(t *testing.T) {
	sess := NewSession(nil, 1, nil, nil)
	require.Nil(t, sess.Abort())

	abort := &Abort{Reason: ErrProtocolViolation}
	require.True(t, sess.EndRecvAbort(abort))
	select {
	case <-sess.RecvDone():
	default:
		require.FailNow(t, "RecvDone not closed")
	}
	require.Same(t, abort, sess.Abort())

	// The session can only be ended once.
	require.False(t, sess.EndRecvAbort(&Abort{}))
	require.False(t, sess.EndRecv(nil))
	require.Same(t, abort, sess.Abort())

	ended := NewSession(nil, 2, nil, nil)
	require.True(t, ended.EndRecv(nil))
	require.False(t, ended.EndRecvAbort(abort))
	require.Nil(t, ended.Abort())
}

type chanPeer struct {
	ch     chan Message
	closed bool
}

func (p *chanPeer) Recv() <-chan Message { return p.ch }
func (p *chanPeer) Send() chan<- Message { return p.ch }
func (p *chanPeer) Close()               { p.closed = true }
func (p *chanPeer) IsLocal() bool        { return true }

func TestSendReplyBacklog(t *testing.T) {
	const queueSize = 2
	peer := &chanPeer{ch: make(chan Message, queueSize)}
	s := NewSession(peer, 1, nil, nil)

	require.True(t, s.TrySend(&Event{Publication: 1}))
	require.Nil(t, s.ReplyRoom())

	// Fill the queue and put replies in the backlog.
	for i := 1; i <= queueSize+2; i++ {
		s.SendReply(&Published{Request: ID(i)})
	}
	room := s.ReplyRoom()
	require.NotNil(t, room)

	// Must not send ahead of the backlog.
	require.False(t, s.TrySend(&Event{Publication: 2}))

	// Read everything in order.
	msg := <-peer.ch
	require.Equal(t, &Event{Publication: 1}, msg)
	for i := 1; i <= queueSize+2; i++ {
		msg = <-peer.ch
		require.Equal(t, &Published{Request: ID(i)}, msg)
	}
	<-room

	// Wait for the backlog goroutine to finish, then send directly again.
	s.sendWG.Wait()
	require.True(t, s.TrySend(&Event{Publication: 3}))
	require.Equal(t, &Event{Publication: 3}, <-peer.ch)
}

func TestSendLimitedBacklog(t *testing.T) {
	const queueSize = 2
	const limit = 3
	peer := &chanPeer{ch: make(chan Message, queueSize)}
	s := NewSession(peer, 1, nil, nil)
	s.SetSendLimit(limit)

	// Fill the queue, then the backlog up to its limit.
	for i := 1; i <= queueSize+limit; i++ {
		require.True(t, s.SendLimited(&Event{Publication: ID(i)}))
	}
	require.False(t, s.SendLimited(&Event{Publication: 99}), "backlog over limit")

	// Messages from SendLimited do not count against room for replies, and a
	// reply waits behind them.
	require.Nil(t, s.ReplyRoom())
	s.SendReply(&Published{Request: 1})
	require.False(t, s.TrySend(&Event{Publication: 99}), "sent ahead of backlog")
	require.False(t, s.SendLimited(&Event{Publication: 99}), "backlog over limit")

	// Read everything in order.
	for i := 1; i <= queueSize+limit; i++ {
		require.Equal(t, &Event{Publication: ID(i)}, <-peer.ch)
	}
	require.Equal(t, &Published{Request: 1}, <-peer.ch)

	// Once the backlog is sent, it has room again.
	s.sendWG.Wait()
	for i := 1; i <= queueSize+limit; i++ {
		require.True(t, s.SendLimited(&Event{Publication: ID(i)}))
	}
	s.Close()
	require.False(t, s.SendLimited(&Event{Publication: 99}), "sent after close")
}

// TestSendLimitedDefaultLimit checks that, with no limit set, SendLimited lets
// as many messages wait in the backlog as the send queue holds.
func TestSendLimitedDefaultLimit(t *testing.T) {
	const queueSize = 2
	peer := &chanPeer{ch: make(chan Message, queueSize)}
	s := NewSession(peer, 1, nil, nil)
	defer s.Close()
	for i := 1; i <= 2*queueSize; i++ {
		require.True(t, s.SendLimited(&Event{Publication: ID(i)}))
	}
	require.False(t, s.SendLimited(&Event{Publication: 99}))
}

func TestSessionCloseDiscardsBacklog(t *testing.T) {
	peer := &chanPeer{ch: make(chan Message, 1)}
	s := NewSession(peer, 1, nil, nil)

	for i := 1; i <= 3; i++ {
		s.SendReply(&Published{Request: ID(i)})
	}
	room := s.ReplyRoom()
	require.NotNil(t, room)

	s.Close()
	require.True(t, peer.closed)
	<-room
	require.Nil(t, s.ReplyRoom())
	require.False(t, s.TrySend(&Event{Publication: 1}))
	s.SendReply(&Published{Request: 4})

	// Only the message that fit in the queue was sent.
	require.Equal(t, &Published{Request: 1}, <-peer.ch)
	require.Empty(t, peer.ch)

	// Closing again does not close the peer again.
	peer.closed = false
	s.Close()
	require.True(t, peer.closed)
}

// TestStopSendingSendsNothingMore checks that once StopSending returns,
// nothing more is sent from the backlog, so that the GOODBYE or ABORT that the
// router sends after it is the last message the peer gets.
func TestStopSendingSendsNothingMore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		peer := &chanPeer{ch: make(chan Message, 1)}
		s := NewSession(peer, 1, nil, nil)
		for i := 1; i <= 3; i++ {
			s.SendReply(&Published{Request: ID(i)})
		}

		s.StopSending()

		// Reading the queued reply makes room, which the backlog must not use, so that
		// the GOODBYE goes in next, as the router's sendLast does.
		require.Equal(t, &Published{Request: 1}, <-peer.ch)
		synctest.Wait()
		select {
		case peer.ch <- &Goodbye{}:
		default:
			require.FailNow(t, "backlog sent after StopSending", "got %T", <-peer.ch)
		}
		require.IsType(t, &Goodbye{}, <-peer.ch)
		require.Nil(t, s.ReplyRoom())

		s.SendReply(&Published{Request: 4})
		require.False(t, s.TrySend(&Event{Publication: 1}))
		synctest.Wait()
		select {
		case msg := <-peer.ch:
			require.FailNow(t, "sent after StopSending", "got %T", msg)
		default:
		}

		s.Close()
		require.True(t, peer.closed)
	})
}

func TestSessionRoles(t *testing.T) {
	details := Dict{
		"roles": Dict{
			"subscriber": Dict{"features": Dict{
				"publisher_identification":   true,
				"pattern_based_subscription": false,
			}},
			"caller": map[string]any{"features": map[string]any{
				"progressive_call_results": true,
			}},
			"callee":    Dict{"features": Dict{}, "other": 1},
			"publisher": Dict{"features": "none"},
			"dealer":    true,
		},
	}
	expected := Dict{
		"subscriber": Dict{"features": Dict{"publisher_identification": true}},
		"caller":     Dict{"features": Dict{"progressive_call_results": true}},
		"callee":     Dict{"features": Dict{}},
		"publisher":  Dict{},
		"dealer":     Dict{},
	}
	sess := NewSession(nil, 1, nil, details)
	require.Equal(t, expected, sess.Roles())

	// Changing the returned dict does not change the session.
	roles := sess.Roles()
	roles["caller"].(Dict)["features"].(Dict)["call_canceling"] = true
	delete(roles["subscriber"].(Dict)["features"].(Dict),
		"publisher_identification")
	roles["callee"].(Dict)["features"] = Dict{"shared_registration": true}
	roles["publisher"].(Dict)["features"] = Dict{"x": true}
	delete(roles, "dealer")
	roles["broker"] = Dict{}
	require.Equal(t, expected, sess.Roles())
	require.False(t, sess.HasFeature("caller", "call_canceling"))
	require.True(t, sess.HasFeature("subscriber", "publisher_identification"))
	require.False(t, sess.HasFeature("callee", "shared_registration"))
	require.True(t, sess.HasRole("dealer"))
	require.False(t, sess.HasRole("broker"))

	for _, details := range []Dict{
		nil,
		{},
		{"roles": nil},
		{"roles": Dict{}},
		{"roles": map[string]any{}},
		{"roles": "caller"},
	} {
		require.Nil(t, NewSession(nil, 1, nil, details).Roles(),
			"details %v", details)
	}
}

func TestSessionRolesConcurrent(t *testing.T) {
	details := Dict{
		"roles": Dict{
			"caller": Dict{"features": Dict{"call_canceling": true}},
		},
	}
	sess := NewSession(nil, 1, nil, details)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for range 100 {
				roles := sess.Roles()
				roles["caller"].(Dict)["features"].(Dict)["call_canceling"] = i
				roles["callee"] = Dict{}
			}
		})
	}
	wg.Wait()

	require.Equal(t, details["roles"], sess.Roles())
	require.True(t, sess.HasFeature("caller", "call_canceling"))
}
