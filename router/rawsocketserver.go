package router

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/gammazero/nexus/v3/transport"
)

// RawSocketServer handles socket connections.
type RawSocketServer struct {
	// RecvLimit is the maximum length of messages the server is willing to
	// receive. Defaults to maximum allowed for protocol: 16M.
	RecvLimit int

	// KeepAlive is the TCP keep-alive period. Default is disable keep-alive.
	KeepAlive time.Duration

	// OutQueueSize is the maximum number of pending outbound messages, per
	// client. The default is defaultOutQueueSize.
	OutQueueSize int

	router Router
}

// NewRawSocketServer takes a router instance and creates a new socket server.
func NewRawSocketServer(r Router) *RawSocketServer {
	return &RawSocketServer{
		router: r,
	}
}

// ListenAndServe listens on the specified endpoint and starts a goroutine that
// accepts new client connections until the returned io.closer is closed.
func (s *RawSocketServer) ListenAndServe(network, address string) (io.Closer, error) {
	l, err := listenWithStaleSockRecovery(network, address)
	if err != nil {
		return nil, err
	}

	// Start request handler loop.
	go s.requestHandler(l)

	return l, nil
}

// listenWithStaleSockRecovery wraps net.Listen with a Unix-socket-only
// recovery path: if the bind fails with "address already in use", the
// path on disk is a socket file, and nothing is currently listening on
// it (a fresh dial is refused), then the file is treated as a leftover
// from a crashed prior process — unlink it and retry. Per
// gammazero/nexus#272.
//
// Safety properties:
//   - Other errors are returned untouched.
//   - Non-unix networks are returned untouched.
//   - A regular file at the path is never removed: protects user data.
//   - A socket with a live listener is never removed: refuses to steal the
//     address from another running process.
func listenWithStaleSockRecovery(network, address string) (net.Listener, error) {
	l, err := net.Listen(network, address)
	if err == nil {
		return l, nil
	}
	if network != "unix" && network != "unixpacket" {
		return nil, err
	}
	// Only attempt recovery for "address already in use" (substring check
	// for portability across Linux/Darwin error wrappings).
	if !strings.Contains(err.Error(), "address already in use") {
		return nil, err
	}
	fi, statErr := os.Stat(address)
	if statErr != nil || fi.Mode()&os.ModeSocket == 0 {
		// Not a socket file (regular file, dir, broken symlink, etc.) —
		// don't touch it; surface the original listen error.
		return nil, err
	}
	// Probe: a quick dial. If it succeeds, a live listener exists; bail
	// without removing the address.
	if c, dialErr := net.DialTimeout(network, address, 100*time.Millisecond); dialErr == nil {
		_ = c.Close()
		return nil, err
	}
	// Stale socket from a crashed prior process. Unlink and retry.
	if rmErr := os.Remove(address); rmErr != nil {
		return nil, fmt.Errorf("listen %s %s: %w (failed to remove stale socket: %w)",
			network, address, err, rmErr)
	}
	return net.Listen(network, address)
}

// ListenAndServeTLS listens on the specified endpoint and starts a goroutine
// that accepts new TLS client connections until the returned io.closer is
// closed. If tls.Config does not already contain a certificate, then certFile
// and keyFile, if specified, are used to load an X509 certificate.
func (s *RawSocketServer) ListenAndServeTLS(network, address string, tlscfg *tls.Config, certFile, keyFile string) (io.Closer, error) { //nolint:lll
	var hasCert bool
	if tlscfg == nil {
		tlscfg = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	} else if len(tlscfg.Certificates) > 0 || tlscfg.GetCertificate != nil {
		hasCert = true
	}

	if !hasCert || certFile != "" || keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("error loading X509 key pair: %w", err)
		}
		tlscfg.Certificates = append(tlscfg.Certificates, cert)
	}

	l, err := tls.Listen(network, address, tlscfg)
	if err != nil {
		return nil, err
	}

	// Start request handler loop.
	go s.requestHandler(l)

	return l, nil
}

func (s *RawSocketServer) requestHandler(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			// Error normal when listener closed, do not log.
			_ = l.Close()
			return
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			if s.KeepAlive != 0 {
				if err = tcpConn.SetKeepAlive(true); err != nil {
					s.router.Logger().Println("Error enabling keepalive:", err)
				}
				if err = tcpConn.SetKeepAlivePeriod(s.KeepAlive); err != nil {
					s.router.Logger().Println("Error setting keepalive period:", err)
				}
			} else {
				if err = tcpConn.SetKeepAlive(false); err != nil {
					s.router.Logger().Println("Error disabling keepalive:", err)
				}
			}
		}
		go s.handleRawSocket(conn)
	}
}

// handleRawSocket accepts a connection from the listening socket, handles the
// client handshake, creates a rawSocketPeer, and then attaches that peer to
// the router.
func (s *RawSocketServer) handleRawSocket(conn net.Conn) {
	qsize := s.OutQueueSize
	if qsize == 0 {
		qsize = defaultOutQueueSize
	}
	peer, err := transport.AcceptRawSocket(conn, s.router.Logger(), s.RecvLimit, qsize)
	if err != nil {
		s.router.Logger().Println("Error accepting rawsocket client:", err)
		return
	}

	if err := s.router.Attach(peer); err != nil {
		s.router.Logger().Println("Error attaching to router:", err)
	}
}
