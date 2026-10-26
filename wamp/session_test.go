package wamp //nolint:testpackage

import (
	"fmt"
	"testing"

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
