package transport_test

import (
	"io"
	"log"
	"net"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
)

// closeConcurrently calls Close from several goroutines at once, and then once
// more after they return.
func closeConcurrently(closePeer func()) {
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(closePeer)
	}
	wg.Wait()
	closePeer()
}

// TestCloseRawSocketPeerConcurrently checks that Close is idempotent and safe
// to call concurrently. A second Close used to panic with "close of closed
// channel".
func TestCloseRawSocketPeerConcurrently(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	go func() {
		// Client handshake: magic byte, then max length 2^9 and JSON.
		_, _ = client.Write([]byte{0x7f, 0x01, 0, 0})
		var resp [4]byte
		_, _ = io.ReadFull(client, resp[:])
		// Keep reading so the peer never blocks writing to the pipe.
		_, _ = io.Copy(io.Discard, client)
	}()

	peer, err := transport.AcceptRawSocket(server, log.New(io.Discard, "", 0), 0, 0)
	require.NoError(t, err)

	closeConcurrently(peer.Close)
}

// TestCloseWebsocketPeerConcurrently checks that Close is idempotent and safe
// to call concurrently. Concurrent calls used to all pass the check for an
// earlier Close, and then panic with "close of closed channel".
func TestCloseWebsocketPeerConcurrently(t *testing.T) {
	// The race needs several tries to show up.
	for range 500 {
		peer := transport.NewWebsocketPeer(newMockSession(), &serialize.JSONSerializer{},
			websocket.TextMessage, log.New(io.Discard, "", 0), 0, 0)
		closeConcurrently(peer.Close)
	}
}
