package router

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/gammazero/nexus/v3/transport"
)

// RawSocketServer handles socket connections.
type RawSocketServer struct {
	// RecvLimit is the maximum length of messages the server is willing to
	// receive.  Defaults to maximum allowed for protocol: 16M.
	RecvLimit int

	// KeepAlive is the TCP keep-alive period.  Default is disable keep-alive.
	KeepAlive time.Duration

	// OutQueueSize is the maximum number of pending outbound messages, per
	// client.  The default is defaultOutQueueSize.
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
	l, err := net.Listen(network, address)
	if err != nil {
		s.router.Logger().Print(err)
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
		s.router.Logger().Print(err)
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
			if s.KeepAlive != 0 {
				tcpConn.SetKeepAlive(true)
				tcpConn.SetKeepAlivePeriod(s.KeepAlive)
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
