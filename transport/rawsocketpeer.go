package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// rawSocketPeer implements the Peer interface, connecting the Send and Recv
// methods to a socket.
type rawSocketPeer struct {
	conn       net.Conn
	serializer serialize.Serializer
	sendLimit  int
	recvLimit  int

	// Used to signal the socket is closed explicitly.
	closed chan struct{}

	// Channels communicate with router.
	rd chan wamp.Message
	wr chan wamp.Message

	cancelSender context.CancelFunc
	ctxSender    context.Context

	writerDone chan struct{}
	closeOnce  sync.Once

	// done is closed when the sender or the receiver stops, after which the peer
	// cannot exchange messages with the remote side.
	done     chan struct{}
	doneOnce sync.Once

	// PONG bodies queued by recvHandler, for sendHandler to write, so that only
	// sendHandler writes frames. pongBytes is the size of the queued frames, and
	// pongReady is signaled when a PONG is queued. pongDropped is set when a
	// PONG is dropped, so that a flood of PINGs is logged once each time the
	// queue fills.
	pongMu      sync.Mutex
	pongs       [][]byte
	pongBytes   int
	pongDropped bool
	pongReady   chan struct{}

	// How long to wait for the sender to exit before closing conn.
	stopTimeout time.Duration
	// How long a write to conn may take before it fails.
	writeTimeout time.Duration

	log stdlog.StdLog
}

const (
	// Serializers
	rawsocketJSON    = 1
	rawsocketMsgpack = 2
	// compatibility with crossbar.io router.
	rawsocketCBOR = 3

	// RawSocket header ID.
	magic = 0x7f

	// Frame types, in the low 3 bits of the first byte of a frame header.
	frameMessage = 0
	framePing    = 1
	framePong    = 2

	// frameHeaderSize is the size of a frame header.
	frameHeaderSize = 4

	// maxQueuedPongBytes limits the size of the PONG frames queued for
	// sendHandler. A PONG for a PING that arrives when the queue is full is
	// dropped, so that a remote peer that sends PINGs faster than it reads
	// PONGs cannot grow the queue without bound. A PONG is always queued when
	// the queue is empty, however large its body.
	maxQueuedPongBytes = 64 << 10
)

// rawSocketWriteTimeout is how long a write to the socket may take before it
// fails, so that a remote peer that stops reading cannot block the sender
// forever. It is the same as the default for websocket peers.
var rawSocketWriteTimeout = defaultWriteTimeout

// ConnectRawSocketPeer creates a new rawSocketPeer with the specified config,
// and connects it, to the WAMP router at the specified address. If a non-nil
// tlsConfig is given, then TLS is used to secure the connection. Documentation
// of tls.Config is here: https://golang.org/pkg/crypto/tls/#Config
//
// The provided Context must be non-nil. If the context expires before the
// connection is complete, an error is returned. Once successfully connected,
// any expiration of the context will not affect the connection.
//
// If recvLimit is > 0, then the client will not receive messages with size
// larger than the nearest power of 2 greater than or equal to recvLimit. If
// recvLimit is <= 0, then the default of 16M is used.
func ConnectRawSocketPeer(ctx context.Context, network, addr string, serialization serialize.Serialization, tlsConfig *tls.Config, logger stdlog.StdLog, recvLimit int) (wamp.Peer, error) { //nolint:lll
	err := checkNetworkType(network)
	if err != nil {
		return nil, err
	}

	protocol, err := getProtoByte(serialization)
	if err != nil {
		return nil, err
	}

	var d net.Dialer
	netConn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	// Close the connection if ctx is done before the TLS and rawsocket handshakes
	// complete, since neither stops otherwise. Once connected, ctx no longer
	// affects the connection.
	stop := context.AfterFunc(ctx, func() { _ = netConn.Close() })

	// conn is the connection that the rawsocket handshake and the peer use:
	// netConn, or with TLS the TLS connection wrapping it. The AfterFunc closes
	// netConn and not conn, since conn is reassigned below and reading it from the
	// AfterFunc would race, and since closing the underlying connection is what
	// unblocks either handshake.
	conn := netConn

	if tlsConfig != nil {
		colonPos := strings.LastIndex(addr, ":")
		if colonPos == -1 {
			colonPos = len(addr)
		}
		hostname := addr[:colonPos]

		// If no ServerName, infer ServerName from hostname to connect to.
		if tlsConfig.ServerName == "" {
			// Make a copy to avoid polluting argument.
			c := tlsConfig.Clone()
			c.ServerName = hostname
			tlsConfig = c
		}

		tlsConn := tls.Client(netConn, tlsConfig)
		err = tlsConn.Handshake()
		if err != nil {
			stop()
			_ = netConn.Close()
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%w: %w", ctx.Err(), err)
			}
			return nil, err
		}
		conn = tlsConn
	}

	peer, err := clientHandshake(conn, logger, protocol, recvLimit)
	if !stop() {
		// ctx was done before the handshake completed, so the connection was closed,
		// or is being closed.
		if err == nil {
			peer.Close()
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %w", ctx.Err(), err)
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return peer, nil
}

// AcceptRawSocket handles the client handshake and returns a rawSocketPeer.
//
// If recvLimit is > 0, then the client will not receive messages with size
// larger than the nearest power of 2 greater than or equal to recvLimit. If
// recvLimit is <= 0, then the default of 16M is used.
func AcceptRawSocket(conn net.Conn, logger stdlog.StdLog, recvLimit, outQueueSize int) (wamp.Peer, error) {
	peer, err := serverHandshake(conn, logger, recvLimit, outQueueSize)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return peer, nil
}

// newRawSocketPeer creates a rawsocket peer from an existing socket
// connection. This is used by clients connecting to the WAMP router, and by
// servers to handle connections from clients.
func newRawSocketPeer(conn net.Conn, serializer serialize.Serializer, logger stdlog.StdLog, sendLimit, recvLimit, outQueueSize int) *rawSocketPeer { //nolint:lll
	rs := &rawSocketPeer{
		conn:       conn,
		serializer: serializer,
		sendLimit:  sendLimit,
		recvLimit:  recvLimit,

		closed:      make(chan struct{}),
		writerDone:  make(chan struct{}),
		done:        make(chan struct{}),
		pongReady:   make(chan struct{}, 1),
		stopTimeout: senderStopTimeout,

		writeTimeout: rawSocketWriteTimeout,

		// The router will read from this channel and immediately dispatch the
		// message to the broker or dealer. Therefore this channel can be
		// unbuffered.
		rd: make(chan wamp.Message),

		// The channel for messages being written to the socket should be large
		// enough to prevent blocking while waiting for a slow socket to send
		// messages. For this reason it may be necessary for these messages to
		// be put into an outbound queue that can grow.
		wr: make(chan wamp.Message, outQueueSize),

		log: logger,
	}
	rs.ctxSender, rs.cancelSender = context.WithCancel(context.Background())

	// Sending to and receiving from socket is handled concurrently.
	go rs.recvHandler()
	go rs.sendHandler()

	return rs
}

func (rs *rawSocketPeer) Recv() <-chan wamp.Message { return rs.rd }

func (rs *rawSocketPeer) Send() chan<- wamp.Message { return rs.wr }

func (rs *rawSocketPeer) IsLocal() bool { return false }

// Done returns a channel that is closed when the peer can no longer exchange
// messages with the remote side, because the connection was lost or the peer
// was closed.
func (rs *rawSocketPeer) Done() <-chan struct{} { return rs.done }

// setDone closes the channel that Done returns.
func (rs *rawSocketPeer) setDone() { rs.doneOnce.Do(func() { close(rs.done) }) }

// Close closes the rawsocket peer. This closes the local send channel, and
// sends a close control message to the socket to tell the other side to close.
// Calling Close more than once, including concurrently, is safe. Calls after
// the first wait for it to finish, and then do nothing.
//
// *** Do not call Send after calling Close. ***
func (rs *rawSocketPeer) Close() {
	rs.closeOnce.Do(rs.close)
}

// close does the work of Close. It must be called only once.
func (rs *rawSocketPeer) close() {
	// Tell sendHandler to exit, and discard any queued messages. Do not close
	// wr channel in case there are incoming messages during close.
	rs.stopSender()
drainLoop:
	for {
		select {
		case <-rs.wr:
		default:
			break drainLoop
		}
	}

	// Tell recvHandler to close.
	close(rs.closed)

	// Ignore errors since socket may have been closed by other side first in
	// response to a goodbye message.
	_ = rs.conn.Close()
}

// stopSender tells the sender to exit and waits for it. If the sender does not
// exit within stopTimeout, because it is stuck writing to a remote peer that
// is not reading, then conn is closed to unblock the write.
func (rs *rawSocketPeer) stopSender() {
	rs.cancelSender()
	timer := time.NewTimer(rs.stopTimeout)
	defer timer.Stop()
	select {
	case <-rs.writerDone:
	case <-timer.C:
		rs.log.Print("rawsocket write not completing, closing socket")
		_ = rs.conn.Close()
		<-rs.writerDone
	}
}

// sendHandler pulls messages from the write channel, and pushes them to the
// socket. It also writes the PONGs that recvHandler queues. It is the only
// goroutine that writes frames while it runs, so that frames do not
// interleave.
func (rs *rawSocketPeer) sendHandler() {
	defer close(rs.writerDone)
	defer rs.cancelSender()
	defer rs.setDone()

	senderDone := rs.ctxSender.Done()
	for {
		select {
		case msg := <-rs.wr:
			if !rs.sendMessage(msg) {
				return
			}
		case <-rs.pongReady:
			for _, body := range rs.takePongs() {
				if err := rs.writeFrameWithDeadline(framePong, body); err != nil {
					rs.log.Println("Error responding to PING:", err)
					rs.writeFailed()
					return
				}
			}
		case <-senderDone:
			// Write the messages already queued, such as an ABORT or GOODBYE
			// queued just before Close, which would otherwise be lost if the
			// stop was seen first. Each write has a deadline, and stopSender
			// closes the connection if this takes too long.
			for {
				select {
				case msg := <-rs.wr:
					if !rs.sendMessage(msg) {
						return
					}
				default:
					return
				}
			}
		}
	}
}

// sendMessage serializes msg and writes it to the socket. It returns false if
// the write failed, which closes the socket, so that the sender must stop.
func (rs *rawSocketPeer) sendMessage(msg wamp.Message) bool {
	b, err := serialize.SerializeMessage(rs.serializer, msg)
	if err != nil {
		rs.log.Print(err)
		return true
	}
	if len(b) > rs.sendLimit {
		rs.log.Println("Message size", len(b), "exceeds limit of", rs.sendLimit)
		return true
	}
	if err = rs.writeFrameWithDeadline(frameMessage, b); err != nil {
		if !wamp.IsGoodbyeAck(msg) {
			rs.log.Println("Error writing message:", msg, err)
		}
		rs.writeFailed()
		return false
	}
	return true
}

// writeFrameWithDeadline writes a frame like writeFrame, but fails if the
// write does not complete within writeTimeout, because the remote peer stopped
// reading.
func (rs *rawSocketPeer) writeFrameWithDeadline(frameType byte, b []byte) error {
	if err := rs.conn.SetWriteDeadline(time.Now().Add(rs.writeTimeout)); err != nil {
		return err
	}
	return rs.writeFrame(frameType, b)
}

// writeFailed closes the socket after a write to it failed or timed out. A
// frame may have been partly written, so nothing more can be written. Closing
// the socket also ends recvHandler, which closes Recv, so that the user of the
// peer learns that the connection is gone.
func (rs *rawSocketPeer) writeFailed() {
	_ = rs.conn.Close()
}

// writeFrame writes a frame of the given type, holding body b, to the socket.
func (rs *rawSocketPeer) writeFrame(frameType byte, b []byte) error {
	lenBytes := intToBytes(len(b))
	header := []byte{frameType, lenBytes[0], lenBytes[1], lenBytes[2]}
	if _, err := rs.conn.Write(header); err != nil {
		return err
	}
	_, err := rs.conn.Write(b)
	return err
}

// abort sends the remote peer an ABORT for the message that could not be
// deserialized, and closes the socket. It stops sendHandler first, so that the
// ABORT is not written in the middle of another message. A write deadline
// keeps a remote peer that is not reading from blocking this, or the stopping
// sendHandler, for longer than abortTimeout.
func (rs *rawSocketPeer) abort(deserializeErr error) {
	select {
	case <-rs.closed:
		// Peer was closed explicitly, and sendHandler has already exited.
		_ = rs.conn.Close()
		return
	default:
	}
	_ = rs.conn.SetWriteDeadline(time.Now().Add(abortTimeout))
	rs.cancelSender()
	<-rs.writerDone

	b, err := rs.serializer.Serialize(deserializeAbort(deserializeErr))
	if err == nil && len(b) <= rs.sendLimit {
		if err = rs.writeFrame(frameMessage, b); err != nil {
			rs.log.Println("Error writing ABORT:", err)
		}
	}
	_ = rs.conn.Close()
}

// recvHandler pulls messages from the socket and pushes them to the read
// channel.
func (rs *rawSocketPeer) recvHandler() {
	// When done, close read channel to cause router to remove session if not
	// already removed.
	defer close(rs.rd)
	defer rs.setDone()
MsgLoop:
	for {
		var header [4]byte
		_, err := io.ReadFull(rs.conn, header[:])
		if err != nil {
			select {
			case <-rs.closed:
				// Peer was closed explicitly. sendHandler should have already
				// been told to exit.
			default:
				// Peer received control message to close. Cause sendHandler to
				// exit without closing the write channel (in case writes still
				// happening) and discard any queued messages. Do not wait
				// forever for a write that is stuck.
				rs.stopSender()

				// Close socket connection.
				_ = rs.conn.Close()
			}
			return
		}

		length := bytesToInt(header[1:])
		if length > rs.recvLimit {
			rs.log.Print("Received message that exceeded size limit, closing")
			_ = rs.conn.Close()
			break
		}

		var msg wamp.Message
		switch header[0] & 0x07 {
		case frameMessage:
			buf := make([]byte, length)
			_, err = io.ReadFull(rs.conn, buf)
			if err != nil {
				rs.log.Println("Error reading message:", err)
				_ = rs.conn.Close()
				return
			}
			msg, err = rs.serializer.Deserialize(buf)
			if err != nil {
				// WAMP treats a message that cannot be deserialized as a
				// protocol violation, so abort instead of skipping it.
				rs.log.Println("Cannot deserialize peer message:", err)
				rs.abort(err)
				return
			}
		case framePing:
			body := make([]byte, length)
			if _, err = io.ReadFull(rs.conn, body); err != nil {
				rs.log.Println("Error reading PING:", err)
				_ = rs.conn.Close()
				return
			}
			rs.queuePong(body)
			continue MsgLoop
		case framePong:
			_, err = io.CopyN(io.Discard, rs.conn, int64(length))
			if err != nil {
				rs.log.Println("Error reading PONG:", err)
				_ = rs.conn.Close()
				return
			}
			continue MsgLoop
		}

		// It is OK for the router to block a client since routing should be
		// very quick compared to the time to transfer a message over the
		// socket, and a blocked client will not block other clients.
		//
		// Need to wake up on rs.closed so this goroutine can exit in the case
		// that messages are not being read from the peer and prevent this
		// write from completing.
		select {
		case rs.rd <- msg:
		case <-rs.closed:
			// The peer was closed, so its user has stopped reading. Drop the message and
			// exit, instead of waiting for it to be read, which would leave this goroutine
			// running after Close.
			_ = rs.conn.Close()
			return
		}
	}
}

// queuePong queues a PONG holding body, the body of a PING from the remote
// peer, for sendHandler to write. It does not block, so that reading from the
// remote peer does not wait for writing to it. The PONG is dropped if the
// queue is full, or if it is over the remote peer's receive limit.
func (rs *rawSocketPeer) queuePong(body []byte) {
	if len(body) > rs.sendLimit {
		rs.log.Println("PONG size", len(body), "exceeds limit of", rs.sendLimit)
		return
	}
	size := frameHeaderSize + len(body)
	rs.pongMu.Lock()
	if len(rs.pongs) != 0 && rs.pongBytes+size > maxQueuedPongBytes {
		logDrop := !rs.pongDropped
		rs.pongDropped = true
		rs.pongMu.Unlock()
		if logDrop {
			rs.log.Print("Too many PINGs waiting for PONG, dropping PINGs")
		}
		return
	}
	rs.pongs = append(rs.pongs, body)
	rs.pongBytes += size
	rs.pongMu.Unlock()

	select {
	case rs.pongReady <- struct{}{}:
	default:
	}
}

// takePongs removes and returns the queued PONG bodies.
func (rs *rawSocketPeer) takePongs() [][]byte {
	rs.pongMu.Lock()
	defer rs.pongMu.Unlock()
	pongs := rs.pongs
	rs.pongs = nil
	rs.pongBytes = 0
	rs.pongDropped = false
	return pongs
}

// clientHandshake handles the client-side of a RawSocket transport handshake.
func clientHandshake(conn net.Conn, logger stdlog.StdLog, protocol byte, recvLimit int) (*rawSocketPeer, error) {
	maxRecvLen := fitRecvLimit(recvLimit)

	_, err := conn.Write([]byte{magic, (maxRecvLen&0xf)<<4 | protocol, 0, 0})
	if err != nil {
		return nil, fmt.Errorf("error sending handshake: %w", err)
	}

	var buf [4]byte
	if _, err = io.ReadFull(conn, buf[:]); err != nil {
		return nil, err
	}

	if buf[0] != magic {
		return nil, errors.New("not a rawsocket handshake")
	}

	repSerializer := buf[1] & 0xf
	if repSerializer == 0 {
		errCode := buf[1] >> 4
		switch errCode {
		case 0:
			return nil, errors.New("illegal error code")
		case 1:
			return nil, errors.New("serializer unsupported")
		case 2:
			return nil, errors.New("maximum message length unacceptable")
		case 3:
			return nil, errors.New("use of reserved bits (unsupported feature)")
		case 4:
			return nil, errors.New("maximum connection count reached")
		default:
			return nil, fmt.Errorf("unknown error: %d", errCode)
		}
	}

	if repSerializer != protocol {
		return nil, errors.New("serializer mismatch")
	}

	var serializer serialize.Serializer
	switch protocol {
	case rawsocketJSON:
		serializer = &serialize.JSONSerializer{}
	case rawsocketMsgpack:
		serializer = &serialize.MessagePackSerializer{}
	case rawsocketCBOR:
		serializer = &serialize.CBORSerializer{}
	}

	sendLimit := byteToLength(buf[1] >> 4)
	recvLimit = byteToLength(maxRecvLen)
	return newRawSocketPeer(conn, serializer, logger, sendLimit, recvLimit, 0), nil
}

// serverHandshake handles the server-side of a RawSocket transport handshake.
func serverHandshake(conn net.Conn, logger stdlog.StdLog, recvLimit, outQueueSize int) (*rawSocketPeer, error) {
	var buf [4]byte
	if _, err := io.ReadFull(conn, buf[:]); err != nil {
		return nil, err
	}

	if buf[0] != magic {
		return nil, errors.New("not a rawsocket handshake")
	}
	if buf[2] != 0 || buf[3] != 0 {
		_, _ = conn.Write([]byte{magic, byte(0x3 << 4), 0, 0})
		return nil, errors.New("use of reserved bits (unsupported feature)")
	}

	serialization := buf[1] & 0xf
	var serializer serialize.Serializer
	switch serialization {
	case 0:
		return nil, errors.New("illegal serializer value")
	case rawsocketJSON:
		serializer = &serialize.JSONSerializer{}
	case rawsocketMsgpack:
		serializer = &serialize.MessagePackSerializer{}
	case rawsocketCBOR:
		serializer = &serialize.CBORSerializer{}
	default:
		_, _ = conn.Write([]byte{magic, byte(0x1 << 4), 0, 0})
		return nil, errors.New("serializer unsupported")
	}

	maxRecvLen := fitRecvLimit(recvLimit)

	_, err := conn.Write([]byte{magic, maxRecvLen<<4 | serialization, 0, 0})
	if err != nil {
		return nil, fmt.Errorf("error sending handshake: %w", err)
	}

	sendLimit := byteToLength(buf[1] >> 4)
	recvLimit = byteToLength(maxRecvLen)
	return newRawSocketPeer(conn, serializer, logger, sendLimit, recvLimit, outQueueSize), nil
}

// fitRecvLimit finds the power of 2 that is greater than or equal to the
// specified receive limit. This value is returned as the RawSocket transport
// byte representation of this value.
func fitRecvLimit(recvLimit int) byte {
	if recvLimit > 0 {
		for b := range byte(0xf) {
			if byteToLength(b) >= recvLimit {
				return b
			}
		}
	}
	return 0xf
}

// intToBytes encodes a 24-bit integer into 3 bytes.
func intToBytes(i int) [3]byte {
	return [3]byte{
		byte((i >> 16) & 0xff),
		byte((i >> 8) & 0xff),
		byte(i & 0xff),
	}
}

// bytesToInt decodes a slice of bytes into an int value.
func bytesToInt(b []byte) int {
	var n, shift uint
	for _, v := range slices.Backward(b) {
		n |= uint(v) << shift
		shift += 8
	}
	return int(n) //nolint:gosec // if uint bigger than in, let int be negative.
}

// byteToLength returns the value corresponding to a RawSocket length byte.
func byteToLength(b byte) int {
	return int(1 << (b + 9))
}

// checkNetworkType checks for acceptable network types, and returns if
func checkNetworkType(network string) error {
	switch network {
	case "tcp", "tcp4", "tcp6", "unix":
	default:
		return errors.New("unsupported network type: " + network)
	}
	return nil
}

// getProtoByte returns the RawSocket byte value for a serialization protocol.
func getProtoByte(serialization serialize.Serialization) (byte, error) {
	switch serialization {
	case serialize.AUTO, serialize.JSON:
		return rawsocketJSON, nil
	case serialize.MSGPACK:
		return rawsocketMsgpack, nil
	case serialize.CBOR:
		return rawsocketCBOR, nil
	default:
		return 0, errors.New("serialization not supported by rawsocket")
	}
}
