package wamp

import "testing"

func TestIDGen(t *testing.T) {
	id1 := GlobalID()
	id2 := GlobalID()
	id3 := GlobalID()

	if id1 == id2 || id1 == id3 || id2 == id3 {
		t.Fatal("Globals should not be equal")
	}

	idgen := NewIDGen()
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
