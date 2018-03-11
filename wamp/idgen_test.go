package wamp

import (
	"errors"
	"testing"
)

func TestIDGen(t *testing.T) {
	id1 := GlobalID()
	id2 := GlobalID()
	id3 := GlobalID()

	if id1 == id2 || id1 == id3 || id2 == id3 {
		t.Fatal("Globals should not be equal")
	}

	idgen := new(IDGen)
	id1 = idgen.Next()
	if id1 != 1 {
		t.Fatal("Sequential IDs should start at 1")
	}
	id2 = idgen.Next()
	id3 = idgen.Next()
	if id2 != 2 || id3 != 3 {
		t.Fatal("IDs are not sequential")
	}

	idgen.next = int64(1) << 53
	id1 = idgen.Next()
	if id1 != 1 {
		t.Fatal("Sequential IDs should wrap at 1 << 53")
	}
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
		if err != nil {
			t.Fatal(err.Error())
		}
	}
}
