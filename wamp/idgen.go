package wamp

import (
	"crypto/rand"
	"math/big"
	"sync"
)

// MaxID is the maximum valid WAMP ID.
const MaxID = 1 << 53

// GlobalID generates a random WAMP ID.
func GlobalID() ID {
	return ID(uint64(secureInt63n(MaxID)) + 1) //nolint:gosec // G115 ok, secureInt63n never negative.
}

// IDGen is generator for WAMP request IDs. Create with new(IDGen).
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
	next uint64
}

// Next returns next ID.
func (g *IDGen) Next() ID {
	g.next++
	if g.next > MaxID {
		g.next = 1
	}
	return ID(g.next)
}

// SyncIDGen is a concurrent-safe IDGen.
type SyncIDGen struct {
	IDGen
	lock sync.Mutex
}

// Next returns next ID.
func (g *SyncIDGen) Next() ID {
	g.lock.Lock()
	defer g.lock.Unlock()
	return g.IDGen.Next()
}

// secureInt63n generates a cryptographically secure random int64 in the range [0, n).
// It panics if n <= 0.
func secureInt63n(n int64) int64 {
	if n <= 0 {
		panic("n must be positive")
	}

	max := big.NewInt(n)
	result, err := rand.Int(rand.Reader, max)
	if err != nil {
		panic(err)
	}

	return result.Int64()
}
