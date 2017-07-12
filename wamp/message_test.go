package wamp

import "testing"

func TestMessage(t *testing.T) {
	h := NewMessage(HELLO)
	mt := h.MessageType()
	if mt.String() != "HELLO" {
		t.Error("Wrong message type string")
	}
}
