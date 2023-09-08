package wamp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testPeer struct {
	in chan Message
}

func newTestPeer() Peer {
	return &testPeer{make(chan Message)}
}

func (p *testPeer) Send() chan<- Message { return p.in }
func (p *testPeer) Recv() <-chan Message { return p.in }
func (p *testPeer) Close()               { close(p.in) }

func (p *testPeer) IsLocal() bool { return true }

func TestRecvTimeout(t *testing.T) {
	p := newTestPeer()
	msg, err := RecvTimeout(p, time.Millisecond)
	require.Error(t, err)
	require.Nil(t, msg)

	go func() {
		p.Send() <- &Hello{}
	}()
	msg, err = RecvTimeout(p, time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, msg, "Failed to recv message")

	p.Close()
	_, err = RecvTimeout(p, time.Millisecond)
	require.EqualError(t, err, "receive channel closed")
}
