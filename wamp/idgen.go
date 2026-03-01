package wamp

import (
	"crypto/rand"
	"math/big"
	"sync"
)

const maxID = 1 << 53

// GlobalID generates a random WAMP ID.
func GlobalID() ID {
	return ID(secureUint63n(maxID) + 1)
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
	if g.next > maxID {
		g.next = 1
	}
	return ID(g.next)
}

// SyncIDGen is a concurrent-safe IDGen. Create with new(SyncIDGen).
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

// secureUint63n generates a cryptographically secure random uint64 in the range [0, n).
// It panics if n <= 0.
func secureUint63n(n int64) uint64 {
	if n <= 0 {
		panic("n must be positive")
	}

	max := big.NewInt(n)
	result, err := rand.Int(rand.Reader, max)
	if err != nil {
		panic(err)
	}

	return result.Uint64()
}
