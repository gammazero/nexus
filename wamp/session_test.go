package wamp

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
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
		{last: ID(MaxID), id: 1, expected: true},      // rollover
		{last: rollHighID + 1, id: 1, expected: true}, // rollover w/ fudge
		{last: rollHighID, id: 1, expected: false},    // not yet a rollover
		// valid for rollover but new value is too far out of bounds
		{last: rollHighID + 1, id: rollLowID, expected: false},
		{last: rollHighID + 1, id: rollLowID - 1, expected: true},
		// id is technically out of bounds but that's OK
		{last: ID(MaxID), id: ID(MaxID + 5), expected: true},
		// Can't jump more than deltaID (prevents flip-fop after rollover)
		{last: 1, id: rollHighID + 1, expected: false},
		{last: 100, id: 100 + deltaID, expected: false},
		{last: 100, id: 100 + deltaID - 1, expected: true},
	}

	s := Session{lastRecvID: 0}
	for i, tc := range testCases {
		tc := tc
		name := fmt.Sprintf("%02d_%d-%d-%v", i, tc.last, tc.id, tc.expected)
		t.Run(name, func(t *testing.T) {
			s.lastRecvID = tc.last
			require.Equal(t, tc.expected, s.IsNewRecvID(tc.id))
		})
	}
}
