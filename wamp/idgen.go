package wamp

import (
	"math/rand"
	"time"
)

const maxID int64 = 1 << 53

func init() {
	rand.Seed(time.Now().UnixNano())
}

// NewID generates a random WAMP ID.
func GlobalID() ID {
	return ID(rand.Int63n(maxID))
}

// ID generator for WAMP request IDs.
//
// WAMP request IDs are sequential per WAMP session, starting at 1 and wrapping
// around at 2**53 (both value are inclusive [1, 2**53]).
//
// The reason to choose the specific upper bound is that 2^53 is the largest
// integer such that this integer and all (positive) smaller integers can be
// represented exactly in IEEE-754 doubles. Some languages (e.g. JavaScript)
// use doubles as their sole number type.
//
// See https://github.com/wamp-proto/wamp-proto/blob/master/spec/basic.md#ids
type IDGen struct {
	next int64
}

// NewIDGen a new sequential ID generator.
func NewIDGen() *IDGen {
	return &IDGen{}
}

// Next returns next ID.
func (g *IDGen) Next() ID {
	g.next++
	if g.next > maxID {
		g.next = 1
	}
	return ID(g.next)
}
