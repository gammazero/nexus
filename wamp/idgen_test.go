package wamp

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

	idgen := new(IDGen)
	id1 = idgen.Next()
	require.Equal(t, ID(1), id1, "Sequential IDs should start at 1")
	id2 = idgen.Next()
	id3 = idgen.Next()
	errMsg = "IDs are not sequential"
	require.Equal(t, ID(2), id2, errMsg)
	require.Equal(t, ID(3), id3, errMsg)

	idgen.next = int64(1) << 53
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
		for j := 0; j < 100000; j++ {
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
	for g := 0; g < 4; g++ {
		err := <-errs
		require.NoError(t, err)
	}
}
