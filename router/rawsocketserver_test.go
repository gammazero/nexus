package router //nolint:testpackage

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

const tcpAddr = "127.0.0.1:8181"

func TestRSHandshakeJSON(t *testing.T) {
	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()
	clsr, err := NewRawSocketServer(r).ListenAndServe("tcp", tcpAddr)
	require.NoError(t, err)
	defer clsr.Close()

	client, err := transport.ConnectRawSocketPeer(context.Background(), "tcp",
		tcpAddr, serialize.JSON, nil, r.Logger(), 0)
	require.NoError(t, err)
	defer client.Close()

	client.Send() <- &wamp.Hello{Realm: testRealm, Details: clientRoles}
	msg, ok := <-client.Recv()
	require.True(t, ok, "recv chan closed")

	_, ok = msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME")
}

func TestRSHandshakeMsgpack(t *testing.T) {
	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()
	clsr, err := NewRawSocketServer(r).ListenAndServe("tcp", tcpAddr)
	require.NoError(t, err)
	defer clsr.Close()

	client, err := transport.ConnectRawSocketPeer(context.Background(), "tcp",
		tcpAddr, serialize.MSGPACK, nil, r.Logger(), 0)
	require.NoError(t, err)
	defer client.Close()

	client.Send() <- &wamp.Hello{Realm: testRealm, Details: clientRoles}
	msg, ok := <-client.Recv()
	require.True(t, ok, "Receive buffer closed")

	_, ok = msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME")
}

// shortSockPath returns a Unix-socket path short enough for macOS's
// 104-byte sun_path limit. t.TempDir() under /var/folders/... regularly
// exceeds it, producing "bind: invalid argument".
func shortSockPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "nexus-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

// TestRSUnixStaleSocketRecovery pins gammazero/nexus#272.
//
// If a previous nexus process bound a Unix domain socket and was killed
// without an opportunity to unlink it (system crash, SIGKILL), the socket
// file persists on the filesystem. A subsequent net.Listen("unix", path)
// fails with EADDRINUSE because the kernel sees the existing path.
//
// The fix: detect the stale-socket case (path exists, is a socket, no
// live listener accepting connections), unlink the path, and retry.
// Importantly, we must NOT remove a regular file at the path (could be
// user data) and we must NOT remove a socket that has a live listener
// (would steal the address from a running process).
func TestRSUnixStaleSocketRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix domain sockets not supported on windows")
	}

	sockPath := shortSockPath(t, "nexus.sock")

	// Simulate a crashed process: bind a Unix socket then close the
	// listener without auto-unlinking, leaving a stale socket file.
	first, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	first.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, first.Close())

	fi, err := os.Stat(sockPath)
	require.NoError(t, err, "stale socket file should still exist")
	require.True(t, fi.Mode()&os.ModeSocket != 0, "should be a socket")

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	closer, err := NewRawSocketServer(r).ListenAndServe("unix", sockPath)
	require.NoError(t, err, "router should detect and clean up stale socket")
	defer closer.Close()

	// Confirm it actually works end-to-end after recovery.
	client, err := transport.ConnectRawSocketPeer(context.Background(), "unix",
		sockPath, serialize.JSON, nil, r.Logger(), 0)
	require.NoError(t, err)
	defer client.Close()

	client.Send() <- &wamp.Hello{Realm: testRealm, Details: clientRoles}
	msg, ok := <-client.Recv()
	require.True(t, ok, "recv chan closed")
	_, ok = msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME after stale-socket recovery")
}

// TestRSUnixLiveSocketNotStolen verifies that the stale-socket cleanup
// does NOT steal the address from a process that is actively listening.
func TestRSUnixLiveSocketNotStolen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix domain sockets not supported on windows")
	}

	sockPath := shortSockPath(t, "live.sock")

	// Start an unrelated listener that's still alive.
	live, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	defer live.Close()

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	_, err = NewRawSocketServer(r).ListenAndServe("unix", sockPath)
	require.Error(t, err, "must not steal a socket from a live listener")
}

// TestRSUnixRegularFileNotRemoved verifies that the stale-socket cleanup
// refuses to remove a regular file at the address — the user might have
// pointed us at the wrong path and we should not destroy their data.
func TestRSUnixRegularFileNotRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix domain sockets not supported on windows")
	}

	sockPath := shortSockPath(t, "regular")
	require.NoError(t, os.WriteFile(sockPath, []byte("important data"), 0o644))

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	_, err = NewRawSocketServer(r).ListenAndServe("unix", sockPath)
	require.Error(t, err, "must refuse to bind when address is a regular file")

	// File contents must be intact.
	data, err := os.ReadFile(sockPath)
	require.NoError(t, err)
	require.Equal(t, "important data", string(data))
}
