package transport_test

import (
	"io"
	"log"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
)

// TestCloseRawSocketPeerTwice checks that Close is idempotent and safe to call
// concurrently. A second Close used to panic with "close of closed channel".
func TestCloseRawSocketPeerTwice(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	go func() {
		// Client handshake: magic, max length 2^9 + JSON serializer.
		_, _ = client.Write([]byte{0x7f, 0x01, 0, 0})
		var resp [4]byte
		_, _ = io.ReadFull(client, resp[:])
		// Keep draining so the peer never blocks writing to the pipe.
		_, _ = io.Copy(io.Discard, client)
	}()

	peer, err := transport.AcceptRawSocket(server, log.New(io.Discard, "", 0), 0, 0)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			peer.Close()
		}()
	}
	wg.Wait()
	peer.Close()
}
