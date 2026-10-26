package wamp //nolint:testpackage

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIDGen(t *testing.T) {
	id1 := GlobalID()
	id2 := GlobalID()
	id3 := GlobalID()

	errMsg := "Globals should not be equal"
	require.NotEqual(t, id1, id2, errMsg)
	require.NotEqual(t, id1, id3, errMsg)
	require.NotEqual(t, id2, id3, errMsg)

	require.GreaterOrEqual(t, id1, ID(1), "GlobalID must be >= 1")
	require.GreaterOrEqual(t, id2, ID(1), "GlobalID must be >= 1")
	require.GreaterOrEqual(t, id3, ID(1), "GlobalID must be >= 1")

	idgen := new(IDGen)
	id1 = idgen.Next()
	require.Equal(t, ID(1), id1, "Sequential IDs should start at 1")
	id2 = idgen.Next()
	id3 = idgen.Next()
	errMsg = "IDs are not sequential"
	require.Equal(t, ID(2), id2, errMsg)
	require.Equal(t, ID(3), id3, errMsg)

	idgen.next = uint64(1) << 53
	id1 = idgen.Next()
	require.Equal(t, ID(1), id1, "Sequential IDs should wrap at 1 << 53")
}

func TestSyncIDGen(t *testing.T) {
	start := make(chan struct{})
	errs := make(chan error, 4)
	idgen := new(SyncIDGen)
	f := func() {
		var i, prev ID
		<-start
		for range 100000 {
			i = idgen.Next()
			if i <= prev {
				errs <- errors.New("Bad sequential ID")
				return
			}
			prev = i
		}
		errs <- nil
	}

	go f()
	go f()
	go f()
	go f()
	close(start)
	for range 4 {
		err := <-errs
		require.NoError(t, err)
	}
}

func TestIDGenRelease(t *testing.T) {
	var g SyncIDGen
	require.Equal(t, ID(1), g.Next())
	id := g.Next()
	require.Equal(t, ID(2), id)

	// Giving back the last ID makes Next return it again.
	g.Release(id)
	require.Equal(t, ID(2), g.Next())

	// Giving back an ID that is not the last one does nothing.
	g.Release(1)
	require.Equal(t, ID(3), g.Next())

	// Giving back the first ID works too.
	var g2 IDGen
	g2.Release(g2.Next())
	require.Equal(t, ID(1), g2.Next())
}
