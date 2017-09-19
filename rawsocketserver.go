package nexus

import (
	"io"
	"net"
	"time"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
)

// RawSocketServer handles socket connections.
type RawSocketServer struct {
	router Router

	serializer serialize.Serializer
	log        stdlog.StdLog
	recvLimit  int
	keepalive  bool
}

// NewRawSocketServer takes a router instance and creates a new socket server.
func NewRawSocketServer(r Router, recvLimit int, keepalive bool) *RawSocketServer {
	return &RawSocketServer{
		router:    r,
		log:       r.Logger(),
		recvLimit: recvLimit,
		keepalive: keepalive,
	}
}

// ListenAndServe listens on the specified endpoint and starts a goroutine that
// accept new client connections until the returned io.closer is closed.
func (s *RawSocketServer) ListenAndServe(network, address string) (io.Closer, error) {
	l, err := net.Listen(network, address)
	if err != nil {
		s.log.Print(err)
		return nil, err
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				// Error normal when listener closed, do not log.
				l.Close()
				return
			}
			if tcpConn, ok := conn.(*net.TCPConn); ok && s.keepalive {
				tcpConn.SetKeepAlive(true)
				tcpConn.SetKeepAlivePeriod(3 * time.Minute)
			}
			go s.handleRawSocket(conn)
		}
	}()

	return l, nil
}

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
