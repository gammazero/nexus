//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package router //nolint:testpackage

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// shortSockPath returns a Unix-socket path short enough for macOS's 104-byte
// sun_path limit. t.TempDir() under /var/folders/... regularly exceeds it,
// producing "bind: invalid argument".
func shortSockPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "nexus-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

// makeStaleSocket simulates a crashed process by binding a Unix socket and
// closing the listener without unlinking it, leaving a stale socket file.
func makeStaleSocket(t *testing.T, path string) {
	t.Helper()
	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, l.Close())

	fi, err := os.Lstat(path)
	require.NoError(t, err, "stale socket file should still exist")
	require.NotZero(t, fi.Mode()&os.ModeSocket, "should be a socket")
}

// requireSocketFile checks that path is still a socket file.
func requireSocketFile(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Lstat(path)
	require.NoError(t, err, "socket file was removed")
	require.NotZero(t, fi.Mode()&os.ModeSocket, "path is no longer a socket")
}

// requireWelcome connects a raw socket client to the Unix socket at path and
// checks that the router answers HELLO with WELCOME.
func requireWelcome(t *testing.T, r Router, path string) {
	t.Helper()
	client, err := transport.ConnectRawSocketPeer(context.Background(), "unix",
		path, serialize.JSON, nil, r.Logger(), 0)
	require.NoError(t, err)
	defer client.Close()

	client.Send() <- &wamp.Hello{Realm: testRealm, Details: clientRoles}
	msg, ok := <-client.Recv()
	require.True(t, ok, "recv chan closed")
	_, ok = msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME")
}

// TestRSUnixStaleSocketRecovery pins gammazero/nexus#272.
//
// If a previous nexus process bound a Unix domain socket and was killed
// without an opportunity to unlink it (system crash, SIGKILL), the socket
// file persists on the filesystem. A subsequent net.Listen("unix", path)
// fails with EADDRINUSE because the kernel sees the existing path. The router
// must detect the stale socket, unlink it, and retry.
func TestRSUnixStaleSocketRecovery(t *testing.T) {
	sockPath := shortSockPath(t, "nexus.sock")
	makeStaleSocket(t, sockPath)

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	closer, err := NewRawSocketServer(r).ListenAndServe("unix", sockPath)
	require.NoError(t, err, "router should detect and clean up stale socket")
	defer closer.Close()

	requireWelcome(t, r, sockPath)
}

// TestRSUnixRestartAfterClose checks that a server can listen again on the
// same path after a previous server on that path was closed.
func TestRSUnixRestartAfterClose(t *testing.T) {
	sockPath := shortSockPath(t, "nexus.sock")

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	closer, err := NewRawSocketServer(r).ListenAndServe("unix", sockPath)
	require.NoError(t, err)
	require.NoError(t, closer.Close())

	closer, err = NewRawSocketServer(r).ListenAndServe("unix", sockPath)
	require.NoError(t, err)
	defer closer.Close()

	requireWelcome(t, r, sockPath)
}

// TestRSUnixLiveSocketNotStolen verifies that the stale-socket cleanup does
// NOT steal the address from a process that is actively listening.
func TestRSUnixLiveSocketNotStolen(t *testing.T) {
	sockPath := shortSockPath(t, "live.sock")

	// Start an unrelated listener that's still alive.
	live, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	defer live.Close()

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	closer, err := NewRawSocketServer(r).ListenAndServe("unix", sockPath)
	if err == nil {
		closer.Close()
	}
	require.ErrorIs(t, err, syscall.EADDRINUSE,
		"must not steal a socket from a live listener")

	// The live listener must still own the path and accept connections.
	requireSocketFile(t, sockPath)
	accepted := make(chan error, 1)
	go func() {
		c, err := live.Accept()
		if err == nil {
			c.Close()
		}
		accepted <- err
	}()
	c, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	c.Close()
	require.NoError(t, <-accepted)
}

// TestRSUnixLockedSocketNotRemoved verifies that a socket whose lock is held
// by another server is not removed, even when nothing accepts a connection on
// it. This is the case of a busy server whose accept backlog is full, which on
// macOS refuses connections the same way that a stale socket does.
func TestRSUnixLockedSocketNotRemoved(t *testing.T) {
	sockPath := shortSockPath(t, "busy.sock")
	makeStaleSocket(t, sockPath)

	// Hold the lock as another server would.
	lock, err := os.OpenFile(sockPath+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	require.NoError(t, err)
	defer lock.Close()
	require.NoError(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_EX))

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	closer, err := NewRawSocketServer(r).ListenAndServe("unix", sockPath)
	if err == nil {
		closer.Close()
	}
	require.ErrorIs(t, err, syscall.EADDRINUSE,
		"must not remove a socket locked by another server")
	requireSocketFile(t, sockPath)
}

// TestRSUnixBusyForeignSocketNotRemoved verifies that a socket owned by a
// process that does not use the lock is not removed when the process is
// listening but not accepting connections. Linux reports a full backlog with
// EAGAIN, which is distinguishable from the ECONNREFUSED of a stale socket.
// macOS refuses the connection, so only the lock protects a busy socket there.
func TestRSUnixBusyForeignSocketNotRemoved(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("a full backlog looks like a stale socket on " + runtime.GOOS)
	}
	sockPath := shortSockPath(t, "busy.sock")

	busy, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	defer busy.Close()

	// Fill the accept backlog.
	var conns []net.Conn
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for {
		c, err := net.DialTimeout("unix", sockPath, 100*time.Millisecond)
		if err != nil {
			break
		}
		conns = append(conns, c)
		require.Less(t, len(conns), 100000, "backlog never filled")
	}

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	closer, err := NewRawSocketServer(r).ListenAndServe("unix", sockPath)
	if err == nil {
		closer.Close()
	}
	require.ErrorIs(t, err, syscall.EADDRINUSE,
		"must not steal a socket from a busy listener")
	requireSocketFile(t, sockPath)
}

// TestRSUnixConcurrentRecovery verifies that when several servers recover
// the same stale socket at the same time, exactly one of them gets the
// address and its socket is the one left on disk.
func TestRSUnixConcurrentRecovery(t *testing.T) {
	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	const servers = 8
	for range 20 {
		sockPath := shortSockPath(t, "nexus.sock")
		makeStaleSocket(t, sockPath)

		var wg sync.WaitGroup
		closers := make(chan io.Closer, servers)
		start := make(chan struct{})
		for range servers {
			wg.Go(func() {
				<-start
				closer, err := NewRawSocketServer(r).ListenAndServe("unix", sockPath)
				if err == nil {
					closers <- closer
				}
			})
		}
		close(start)
		wg.Wait()
		close(closers)

		var winners []io.Closer
		for c := range closers {
			winners = append(winners, c)
		}
		require.Len(t, winners, 1, "exactly one server should get the address")
		requireWelcome(t, r, sockPath)
		require.NoError(t, winners[0].Close())
	}
}

// TestRSUnixSymlinkNotRemoved verifies that a symlink at the address is not
// removed, even when it points to a stale socket.
func TestRSUnixSymlinkNotRemoved(t *testing.T) {
	target := shortSockPath(t, "target.sock")
	makeStaleSocket(t, target)
	sockPath := filepath.Join(filepath.Dir(target), "link.sock")
	require.NoError(t, os.Symlink(target, sockPath))

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	closer, err := NewRawSocketServer(r).ListenAndServe("unix", sockPath)
	if err == nil {
		closer.Close()
	}
	require.ErrorIs(t, err, syscall.EADDRINUSE,
		"must refuse to bind when address is a symlink")

	fi, err := os.Lstat(sockPath)
	require.NoError(t, err, "symlink was removed")
	require.NotZero(t, fi.Mode()&os.ModeSymlink, "path is no longer a symlink")
	requireSocketFile(t, target)
}

// TestRSUnixRegularFileNotRemoved verifies that the stale-socket cleanup
// refuses to remove a regular file at the address. The user might have
// pointed us at the wrong path and we should not destroy their data.
func TestRSUnixRegularFileNotRemoved(t *testing.T) {
	sockPath := shortSockPath(t, "regular")
	require.NoError(t, os.WriteFile(sockPath, []byte("important data"), 0o644))

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	closer, err := NewRawSocketServer(r).ListenAndServe("unix", sockPath)
	if err == nil {
		closer.Close()
	}
	require.ErrorIs(t, err, syscall.EADDRINUSE,
		"must refuse to bind when address is a regular file")

	// File contents must be intact.
	data, err := os.ReadFile(sockPath)
	require.NoError(t, err)
	require.Equal(t, "important data", string(data))
}

// TestRSUnixTLSStaleSocketRecovery verifies that ListenAndServeTLS also
// recovers from a stale socket file.
func TestRSUnixTLSStaleSocketRecovery(t *testing.T) {
	sockPath := shortSockPath(t, "tls.sock")
	makeStaleSocket(t, sockPath)

	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()

	tlscfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{selfSignedCert(t)},
	}
	closer, err := NewRawSocketServer(r).ListenAndServeTLS("unix", sockPath,
		tlscfg, "", "")
	require.NoError(t, err, "router should detect and clean up stale socket")
	defer closer.Close()

	conn, err := tls.Dial("unix", sockPath, &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, //nolint:gosec
	})
	require.NoError(t, err)
	require.NoError(t, conn.Close())
}

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "nexus-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
