package wamp

import (
	"testing"
	"time"
)

type testPeer struct {
	in chan Message
}

func newTestPeer() Peer {
	return &testPeer{make(chan Message, 1)}
}

func (p *testPeer) TrySend(msg Message) error { return nil }

func (p *testPeer) Send(msg Message) error {
	p.in <- msg
	return nil
}
func (p *testPeer) Recv() <-chan Message { return p.in }
func (p *testPeer) Close()               { close(p.in) }

func TestRecvTimeout(t *testing.T) {
	p := newTestPeer()
	msg, err := RecvTimeout(p, time.Millisecond)
	if err == nil {
		t.Fatal("Expected timeout error")
	}

	go func() {
		p.Send(&Hello{})
	}()
	msg, err = RecvTimeout(p, time.Millisecond)
	if err != nil || msg == nil {
		t.Fatal("Failed to recv message")
	}

	p.Close()
	_, err = RecvTimeout(p, time.Millisecond)
	if err == nil || err.Error() != "receive channel closed" {
		t.Fatal("Expected closed channel error")
	}
}
