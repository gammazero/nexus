package router

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
)

// RawSocketServer handles socket connections.
type RawSocketServer struct {
	router Router

	log       stdlog.StdLog
	recvLimit int
	keepAlive time.Duration
}

// NewRawSocketServer takes a router instance and creates a new socket server.
func NewRawSocketServer(r Router, recvLimit int, keepAlive time.Duration) *RawSocketServer {
	return &RawSocketServer{
		router:    r,
		log:       r.Logger(),
		recvLimit: recvLimit,
		keepAlive: keepAlive,
	}
}

// ListenAndServe listens on the specified endpoint and starts a goroutine that
// accepts new client connections until the returned io.closer is closed.
func (s *RawSocketServer) ListenAndServe(network, address string) (io.Closer, error) {
	l, err := net.Listen(network, address)
	if err != nil {
		s.log.Print(err)
		return nil, err
	}

	// Start request handler loop.
	go s.requestHandler(l)

	return l, nil
}

// ListenAndServeTLS listens on the specified endpoint and starts a
// goroutine that accepts new TLS client connections until the returned
// io.closer is closed.  If tls.Config does not already contain a certificate,
// then certFile and keyFile, if specified, are used to load an X509
// certificate.
func (s *RawSocketServer) ListenAndServeTLS(network, address string, tlscfg *tls.Config, certFile, keyFile string) (io.Closer, error) {
	var hasCert bool
	if tlscfg == nil {
		tlscfg = &tls.Config{}
	} else if len(tlscfg.Certificates) > 0 || tlscfg.GetCertificate != nil {
		hasCert = true
	}

	if !hasCert || certFile != "" || keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("error loading X509 key pair: %s", err)
		}
		tlscfg.Certificates = append(tlscfg.Certificates, cert)
	}

	l, err := tls.Listen(network, address, tlscfg)
	if err != nil {
		s.log.Print(err)
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
			l.Close()
			return
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			if s.keepAlive != 0 {
				tcpConn.SetKeepAlive(true)
				tcpConn.SetKeepAlivePeriod(s.keepAlive)
			} else {
				tcpConn.SetKeepAlive(false)
			}
		}
		go s.handleRawSocket(conn)
	}
}

// handleRawSocket accpets a connection from the listening socket, handles the
// client handshake, creates a rawSocketPeer, and then attaches that peer to
// the router.
func (s *RawSocketServer) handleRawSocket(conn net.Conn) {
	peer, err := transport.AcceptRawSocket(conn, s.log, s.recvLimit)
	if err != nil {
		s.log.Println("Error accepting rawsocket client:", err)
		return
	}

	if err := s.router.Attach(peer); err != nil {
		s.log.Println("Error attaching to router:", err)
	}
}
