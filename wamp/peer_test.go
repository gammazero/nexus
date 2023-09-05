package wamp

import (
	"context"
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

func (p *testPeer) TrySend(msg Message) error {
	return TrySend(p.in, msg)
}

func (p *testPeer) Send(msg Message) error {
	p.in <- msg
	return nil
}

func (p *testPeer) SendCtx(ctx context.Context, msg Message) error {
	return SendCtx(ctx, p.in, msg)
}

func (p *testPeer) Recv() <-chan Message { return p.in }
func (p *testPeer) Close()               { close(p.in) }

func (p *testPeer) IsLocal() bool { return true }

func TestRecvTimeout(t *testing.T) {
	p := newTestPeer()
	msg, err := RecvTimeout(p, time.Millisecond)
	require.Error(t, err)

	go func() {
		p.Send(&Hello{})
	}()
	msg, err = RecvTimeout(p, time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, msg, "Failed to recv message")

	p.Close()
	_, err = RecvTimeout(p, time.Millisecond)
	require.EqualError(t, err, "receive channel closed")
}

func TestTrySend(t *testing.T) {
	p := newTestPeer()
	err := p.TrySend(&Hello{})
	require.Error(t, err)

	ready := make(chan struct{})
	go func() {
		close(ready)
		<-p.Recv()
	}()
	<-ready

	err = p.TrySend(&Hello{})
	require.NoError(t, err)

	p.Close()
}

func TestSendCtx(t *testing.T) {
	p := newTestPeer()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-p.Recv()
	}()
	err := p.SendCtx(ctx, &Hello{})
	require.NoError(t, err)

	cancel()
	err = p.SendCtx(ctx, &Hello{})
	require.ErrorIs(t, err, context.Canceled)
	p.Close()
}
