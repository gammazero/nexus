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
	// receive. Defaults to maximum allowed for protocol: 16M.
	RecvLimit int

	// KeepAlive is the TCP keep-alive period. Default is disable keep-alive.
	KeepAlive time.Duration

	// OutQueueSize is the maximum number of pending outbound messages, per client.
	// The default is defaultOutQueueSize.
	//
	// When a client's queue is full, the router does not drop replies to the
	// client's own requests. They wait until the client reads them, and the router
	// holds the client's next request until there is room. A client that reads
	// nothing for a minute while the router waits is disconnected. EVENTs,
	// INVOCATIONs, INTERRUPTs and progressive RESULTs that do not fit wait in a
	// separate backlog, set by EventBacklog in the realm's RealmConfig, and when
	// that is full too, the router drops or fails them, or closes the client, as
	// set there. Size the queue for bursts of EVENTs, INVOCATIONs and progressive
	// RESULTs, not for the number of requests a client has in flight.
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
// accepts new client connections until the returned io.Closer is closed.
func (s *RawSocketServer) ListenAndServe(network, address string) (io.Closer, error) {
	l, err := listenRawSocket(network, address)
	if err != nil {
		return nil, err
	}

	// Start request handler loop.
	go s.requestHandler(l)

	return l, nil
}

// ListenAndServeTLS listens on the specified endpoint and starts a goroutine
// that accepts new TLS client connections until the returned io.Closer is
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

	l, err := listenRawSocket(network, address)
	if err != nil {
		return nil, err
	}
	l = tls.NewListener(l, tlscfg)

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
		s.router.Logger().Printf("Error attaching to router: %v (peer %s)", err,
			peerDescription(conn.RemoteAddr().String(), nil))
	}
}
