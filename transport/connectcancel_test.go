package transport_test

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// silentListener listens on network and accepts connections without ever
// answering, so that a client connecting to it waits forever in its handshake.
// It returns the address and a channel that receives each accepted connection.
func silentListener(t *testing.T, network string) (string, <-chan net.Conn) {
	t.Helper()
	address := "127.0.0.1:0"
	if network == "unix" {
		// t.TempDir() can exceed the 104-byte Unix socket path limit on macOS.
		dir, err := os.MkdirTemp("/tmp", "nexus-test-")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		address = filepath.Join(dir, "silent.sock")
	}
	l, err := net.Listen(network, address)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	accepted := make(chan net.Conn, 1)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
			accepted <- conn
		}
	}()
	return l.Addr().String(), accepted
}

// TestConnectCancelDuringHandshake checks that each kind of client connect
// returns the context's error when the context is canceled while waiting for
// the server to answer.
func TestConnectCancelDuringHandshake(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec

	cases := []struct {
		name    string
		network string
		connect func(ctx context.Context, addr string) (wamp.Peer, error)
	}{
		{"websocket", "tcp", func(ctx context.Context, addr string) (wamp.Peer, error) {
			return transport.ConnectWebsocketPeer(ctx, "ws://"+addr+"/", serialize.JSON, nil, logger, nil)
		}},
		{"websocket TLS", "tcp", func(ctx context.Context, addr string) (wamp.Peer, error) {
			return transport.ConnectWebsocketPeer(ctx, "wss://"+addr+"/", serialize.JSON, tlsCfg, logger, nil)
		}},
		{"websocket custom dial", "tcp", func(ctx context.Context, addr string) (wamp.Peer, error) {
			cfg := &transport.WebsocketConfig{Dial: net.Dial}
			return transport.ConnectWebsocketPeer(ctx, "ws://"+addr+"/", serialize.JSON, nil, logger, cfg)
		}},
		{"rawsocket TCP", "tcp", func(ctx context.Context, addr string) (wamp.Peer, error) {
			return transport.ConnectRawSocketPeer(ctx, "tcp", addr, serialize.JSON, nil, logger, 0)
		}},
		{"rawsocket TLS", "tcp", func(ctx context.Context, addr string) (wamp.Peer, error) {
			return transport.ConnectRawSocketPeer(ctx, "tcp", addr, serialize.JSON, tlsCfg, logger, 0)
		}},
		{"rawsocket Unix", "unix", func(ctx context.Context, addr string) (wamp.Peer, error) {
			return transport.ConnectRawSocketPeer(ctx, "unix", addr, serialize.JSON, nil, logger, 0)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Cleanups run last-in first-out, so this check runs after the listener and
			// connections are closed.
			ignore := goleak.IgnoreCurrent()
			t.Cleanup(func() { goleak.VerifyNone(t, ignore) })

			addr, accepted := silentListener(t, tc.network)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			errChan := make(chan error, 1)
			go func() {
				peer, err := tc.connect(ctx, addr)
				if peer != nil {
					peer.Close()
				}
				errChan <- err
			}()

			// Cancel only once the connect is under way.
			select {
			case <-accepted:
			case <-time.After(testTimeout):
				require.FailNow(t, "connection was never accepted")
			}
			cancel()

			select {
			case err := <-errChan:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(testTimeout):
				require.FailNow(t, "connect did not return after its context was canceled")
			}
		})
	}
}
