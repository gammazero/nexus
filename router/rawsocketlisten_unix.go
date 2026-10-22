//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package router

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// staleProbeTimeout is how long to wait for a connection when checking
// whether anything is listening on an existing socket file.
const staleProbeTimeout = 100 * time.Millisecond

// listenRawSocket listens on the network address. For a Unix socket path, it
// recovers from a stale socket file left by a server that exited without
// removing it. See gammazero/nexus#272.
//
// A server holds an exclusive lock on the file path+".lock" for as long as it
// listens on path. The kernel releases the lock when the server exits, even if
// it crashes, so holding the lock means that no other nexus server uses path.
// The lock file is never removed, since removing it would let two servers lock
// different files for the same path.
//
// If path exists once the lock is held, it is removed only if it is a socket,
// not a symlink or any other file, and connecting to it is refused. The
// connection check protects a socket owned by a process that does not use the
// lock. It cannot detect such a process that is busy on macOS, which refuses
// connections when the accept backlog is full.
func listenRawSocket(network, address string) (net.Listener, error) {
	if (network != "unix" && network != "unixpacket") || address == "" ||
		(runtime.GOOS == "linux" && strings.HasPrefix(address, "@")) {
		// Not a socket path in the filesystem.
		return net.Listen(network, address)
	}

	lock, err := lockSocketPath(address)
	if err != nil {
		return nil, fmt.Errorf("listen %s %s: %w", network, address, err)
	}

	l, err := net.Listen(network, address)
	if errors.Is(err, syscall.EADDRINUSE) && isStaleSocket(network, address) {
		if rmErr := os.Remove(address); rmErr != nil {
			err = fmt.Errorf("%w (failed to remove stale socket: %w)", err, rmErr)
		} else {
			l, err = net.Listen(network, address)
		}
	}
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	return &lockedListener{Listener: l, lock: lock}, nil
}

// lockSocketPath acquires the exclusive lock for the socket at address. It
// returns an error wrapping syscall.EADDRINUSE if another server holds the
// lock.
func lockSocketPath(address string) (*os.File, error) {
	f, err := os.OpenFile(address+".lock", os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("socket locked by another server: %w",
				syscall.EADDRINUSE)
		}
		return nil, os.NewSyscallError("flock", err)
	}
	return f, nil
}

// isStaleSocket reports whether address is a socket file that nothing is
// listening on.
func isStaleSocket(network, address string) bool {
	fi, err := os.Lstat(address)
	if err != nil || fi.Mode()&os.ModeSocket == 0 {
		return false
	}
	c, err := net.DialTimeout(network, address, staleProbeTimeout)
	if err == nil {
		_ = c.Close()
		return false
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}

// lockedListener is a net.Listener that releases the lock for its socket path
// when closed.
type lockedListener struct {
	net.Listener
	lock      *os.File
	closeOnce sync.Once
	closeErr  error
}

func (l *lockedListener) Close() error {
	l.closeOnce.Do(func() {
		// Close the listener first, so that its socket file is removed while
		// the lock is still held.
		l.closeErr = l.Listener.Close()
		_ = l.lock.Close()
	})
	return l.closeErr
}
