package nexus

import (
	"net"
	"time"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/transport/serialize"
)

// RawSocketServer handles socket connections.
type RawSocketServer struct {
	router Router

	listener net.Listener
	addr     net.Addr

	serializer serialize.Serializer
	log        stdlog.StdLog
	recvLimit  int
	stop       chan struct{}
}

// NewRawSocketServer takes a router instance and creates a new socket server.
func NewRawSocketServer(r Router, network, address string, recvLimit int) (*RawSocketServer, error) {
	s := &RawSocketServer{
		router:    r,
		log:       r.Logger(),
		recvLimit: recvLimit,
		stop:      make(chan struct{}, 1),
	}

	l, err := net.Listen(network, address)
	if err != nil {
		s.log.Print(err)
		return nil, err
	}
	s.listener = l
	if tcpListener, ok := l.(*net.TCPListener); ok {
		s.addr = tcpListener.Addr()
	} else if unixListener, ok := l.(*net.UnixListener); ok {
		s.addr = unixListener.Addr()
	}

	return s, nil
}

// Serve continues to accept new client connections until the server is closed.
func (s *RawSocketServer) Serve(keepalive bool) {
	go func(listener net.Listener) {
		<-s.stop
		listener.Close()
	}(s.listener)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.listener.Close()
			return
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok && keepalive {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(3 * time.Minute)
		}

		go s.handleRawSocket(conn)
	}
}

// Close stops the server from accepting connections, and causes Serve to exit.
func (s *RawSocketServer) Close() {
	close(s.stop)
}

// Addr returns the net.Addr that the server is listening on.
func (s *RawSocketServer) Addr() net.Addr {
	return s.addr
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
