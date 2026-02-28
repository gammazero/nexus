package wamp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

type testPeer struct {
	in chan wamp.Message
}

func newTestPeer() wamp.Peer {
	return &testPeer{make(chan wamp.Message)}
}

func (p *testPeer) Send() chan<- wamp.Message { return p.in }
func (p *testPeer) Recv() <-chan wamp.Message { return p.in }
func (p *testPeer) Close()                    { close(p.in) }

func (p *testPeer) IsLocal() bool { return true }

func TestRecvTimeout(t *testing.T) {
	p := newTestPeer()
	msg, err := wamp.RecvTimeout(p, time.Millisecond)
	require.Error(t, err)
	require.Nil(t, msg)

	go func() {
		p.Send() <- &wamp.Hello{}
	}()
	msg, err = wamp.RecvTimeout(p, time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, msg, "Failed to recv message")

	p.Close()
	_, err = wamp.RecvTimeout(p, time.Millisecond)
	require.EqualError(t, err, "receive channel closed")
}
