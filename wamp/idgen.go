package wamp

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
)

// MaxID is the maximum valid WAMP ID.
const MaxID = 1 << 53

// GlobalID generates a random WAMP ID in the range [1, MaxID].
func GlobalID() ID {
	var b [8]byte
	rand.Read(b[:])
	return ID(binary.LittleEndian.Uint64(b[:])&(MaxID-1) + 1)
}

// IDGen is generator for WAMP request IDs. Create with new(IDGen).
//
// WAMP request IDs are sequential per WAMP session, starting at 1 and wrapping
// around at 2**53 (both values are inclusive [1, 2**53]).
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

// Release gives back id, which must be the ID that Next last returned, so that
// the next call to Next returns it again. It is for a request that was not
// sent after all, so that request IDs stay sequential. Release does nothing if
// id is not the ID that Next last returned.
func (g *IDGen) Release(id ID) {
	if g.next == uint64(id) {
		g.next = uint64(id) - 1
	}
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

// Release gives back id, as IDGen.Release does.
func (g *SyncIDGen) Release(id ID) {
	g.lock.Lock()
	defer g.lock.Unlock()
	g.IDGen.Release(id)
}
