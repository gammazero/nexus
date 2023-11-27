package transport

import (
	"errors"
	"testing"
	"time"

	"github.com/dtegapp/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

func TestSendRecv(t *testing.T) {
	c, r := LinkedPeers()

	go func() {
		c.Send() <- &wamp.Hello{}
	}()
	select {
	case <-r.Recv():
	case <-time.After(time.Second):
		require.FailNow(t, "Router peer did not receive msg")
	}

	r.Send() <- &wamp.Welcome{}
	select {
	case <-c.Recv():
	default:
		require.FailNow(t, "Client peer did not receive msg")
	}

	r.Close()
	select {
	case msg := <-c.Recv():
		require.Nil(t, msg, "Expected nil msg on close")
	case <-time.After(time.Second):
		require.FailNow(t, "Client did not wake up when router closed.")
	}
}

func TestDropOnBlockedClient(t *testing.T) {
	const qsize = 5
	_, r := LinkedPeersQSize(qsize)

	// Check that r -> c drops when full
	for i := 0; i < qsize; i++ {
		select {
		case r.Send() <- &wamp.Publish{}:
		default:
		}
	}
	done := make(chan struct{})
	var err error
	go func() {
		select {
		case r.Send() <- &wamp.Publish{}:
		default:
			err = errors.New("blocked")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		require.FailNow(t, "Send should have dropped and not blocked")
	}
	require.EqualError(t, err, "blocked")
}

func TestBlockOnBlockedRouter(t *testing.T) {
	c, r := LinkedPeers()

	done := make(chan struct{})
	go func() {
		for i := 0; i < cap(r.Recv())+1; i++ {
			c.Send() <- &wamp.Publish{}
		}
		close(done)
	}()
	select {
	case <-done:
		require.FailNow(t, "Expected send to be blocked")
	case <-time.After(time.Second):
	}
	<-r.Recv()
	<-done
}

func BenchmarkClientToRouter(b *testing.B) {
	c, r := LinkedPeers()

	b.ResetTimer()
	go func() {
		for i := 0; i < b.N; i++ {
			c.Send() <- &wamp.Hello{}
		}
	}()
	for i := 0; i < b.N; i++ {
		<-r.Recv()
	}
}

func BenchmarkRouterToClient(b *testing.B) {
	c, r := LinkedPeers()

	b.ResetTimer()
	go func() {
		for i := 0; i < b.N; i++ {
			r.Send() <- &wamp.Hello{}
		}
	}()
	for i := 0; i < b.N; i++ {
		<-c.Recv()
	}
}
