package transport

import (
	"log"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gammazero/nexus/wamp"
)

func _TestSendRecv(t *testing.T) {
	c, r := LinkedPeers(nil)

	go c.Send(&wamp.Hello{})
	select {
	case <-r.Recv():
	case <-time.After(time.Second):
		t.Fatal("Router peer did not receive msg")
	}

	r.Send(&wamp.Welcome{})
	select {
	case <-c.Recv():
	default:
		t.Fatal("Client peer did not receive msg")
	}

	r.Close()
	select {
	case msg := <-c.Recv():
		if msg != nil {
			t.Fatal("Expected nil msg on close")
		}
	case <-time.After(time.Second):
		t.Fatal("Client did not wake up when router closed.")
	}
}

func TestDropOnBlockedClient(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	_, r := LinkedPeers(logger)

	// Check that r -> c drops when full
	for i := 0; i < linkedPeersOutQueueSize; i++ {
		r.Send(&wamp.Publish{})
	}
	done := make(chan struct{})
	var err error
	go func() {
		err = r.Send(&wamp.Publish{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Send should have dropped and not blocked")
	}
	if err == nil || !strings.HasPrefix(err.Error(), "client blocked") {
		t.Fatal("Expected blocked error")
	}
}

func TestBlockOnBlockedRouter(t *testing.T) {
	c, r := LinkedPeers(nil)

	done := make(chan struct{})
	go func() {
		for i := 0; i < cap(r.Recv())+1; i++ {
			c.Send(&wamp.Publish{})
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Expected send to be blocked")
	case <-time.After(time.Second):
	}
	<-r.Recv()
	<-done
}

func BenchmarkClientToRouter(b *testing.B) {
	c, r := LinkedPeers(nil)

	b.ResetTimer()
	go func() {
		for i := 0; i < b.N; i++ {
			c.Send(&wamp.Hello{})
		}
	}()
	for i := 0; i < b.N; i++ {
		<-r.Recv()
	}
}

func BenchmarkRouterToClient(b *testing.B) {
	c, r := LinkedPeers(nil)

	b.ResetTimer()
	go func() {
		for i := 0; i < b.N; i++ {
			err := r.Send(&wamp.Hello{})
			for err != nil {
				runtime.Gosched()
				err = r.Send(&wamp.Hello{})
			}
		}
	}()
	for i := 0; i < b.N; i++ {
		<-c.Recv()
	}
}
