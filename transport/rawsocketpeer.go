package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"strings"
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
)

// ConnectRawSocketPeer creates a new rawSocketPeer with the specified config,
// and connects it, to the WAMP router at the specified address.  If a non-nil
// tlsConfig is given, then TLS is used to secure the connection.
// Docuemntation of tls.Config is here:
// https://golang.org/pkg/crypto/tls/#Config
//
// The provided Context must be non-nil.  If the context expires before the
// connection is complete, an error is returned.  Once successfully connected,
// any expiration of the context will not affect the connection.
//
// If recvLimit is > 0, then the client will not receive messages with size
// larger than the nearest power of 2 greater than or equal to recvLimit.  If
// recvLimit is <= 0, then the default of 16M is used.
func ConnectRawSocketPeer(ctx context.Context, network, addr string, serialization serialize.Serialization, tlsConfig *tls.Config, logger stdlog.StdLog, recvLimit int) (wamp.Peer, error) {
	err := checkNetworkType(network)
	if err != nil {
		return nil, err
	}

	protocol, err := getProtoByte(serialization)
	if err != nil {
		return nil, err
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

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

		tlsConn := tls.Client(conn, tlsConfig)
		errChannel := make(chan error, 1)
		go func() {
			errChannel <- tlsConn.Handshake()
		}()

		// Wait for TLS handshake to complete or context to expire.
		select {
		case err = <-errChannel:
			if err != nil {
				conn.Close()
				return nil, err
			}
		case <-ctx.Done():
			conn.Close()
			return nil, ctx.Err()
		}
		conn = tlsConn
	}

	peer, err := clientHandshake(conn, logger, protocol, recvLimit)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return peer, nil
}

// AcceptRawSocket handles the client handshake and returns a rawSocketPeer.
//
// If recvLimit is > 0, then the client will not receive messages with size
// larger than the nearest power of 2 greater than or equal to recvLimit.  If
// recvLimit is <= 0, then the default of 16M is used.
func AcceptRawSocket(conn net.Conn, logger stdlog.StdLog, recvLimit, outQueueSize int) (wamp.Peer, error) {
	peer, err := serverHandshake(conn, logger, recvLimit, outQueueSize)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return peer, nil
}

// newRawSocketPeer creates a rawsocket peer from an existing socket
// connection.  This is used by clients connecting to the WAMP router, and by
// servers to handle connections from clients.
func newRawSocketPeer(conn net.Conn, serializer serialize.Serializer, logger stdlog.StdLog, sendLimit, recvLimit, outQueueSize int) *rawSocketPeer {
	rs := &rawSocketPeer{
		conn:       conn,
		serializer: serializer,
		sendLimit:  sendLimit,
		recvLimit:  recvLimit,

		closed:     make(chan struct{}),
		writerDone: make(chan struct{}),

		// The router will read from this channel and immediately dispatch the
		// message to the broker or dealer.  Therefore this channel can be
		// unbuffered.
		rd: make(chan wamp.Message),

		// The channel for messages being written to the socket should be
		// large enough to prevent blocking while waiting for a slow socket
		// to send messages.  For this reason it may be necessary for these
		// messages to be put into an outbound queue that can grow.
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

func (rs *rawSocketPeer) TrySend(msg wamp.Message) error {
	return wamp.TrySend(rs.wr, msg)
}

func (rs *rawSocketPeer) SendCtx(ctx context.Context, msg wamp.Message) error {
	return wamp.SendCtx(ctx, rs.wr, msg)
}

func (rs *rawSocketPeer) Send(msg wamp.Message) error {
	return wamp.SendCtx(rs.ctxSender, rs.wr, msg)
}

// Close closes the rawsocket peer.  This closes the local send channel, and
// sends a close control message to the socket to tell the other side to
// close.
//
// *** Do not call Send after calling Close. ***
func (rs *rawSocketPeer) Close() {
	// Tell sendHandler to exit, and discard any queued messages.  Do not close
	// wr channel in case there are incoming messages during close.
	rs.cancelSender()
	<-rs.writerDone

	// Tell recvHandler to close.
	close(rs.closed)

	// Ignore errors since socket may have been closed by other side first in
	// response to a goodbye message.
	rs.conn.Close()
}

// sendHandler pulls messages from the write channel, and pushes them to the
// socket.
func (rs *rawSocketPeer) sendHandler() {
	defer close(rs.writerDone)
	defer rs.cancelSender()

	senderDone := rs.ctxSender.Done()
sendLoop:
	for {
		select {
		case msg := <-rs.wr:
			b, err := rs.serializer.Serialize(msg)
			if err != nil {
				rs.log.Print(err)
				continue sendLoop
			}
			if len(b) > rs.sendLimit {
				rs.log.Println("Message size", len(b), "exceeds limit of",
					rs.sendLimit)
				continue sendLoop
			}
			lenBytes := intToBytes(len(b))
			header := []byte{0x0, lenBytes[0], lenBytes[1], lenBytes[2]}
			if _, err = rs.conn.Write(header); err != nil {
				if !wamp.IsGoodbyeAck(msg) {
					rs.log.Println("Error writing header:", err)
				}
				continue sendLoop
			}
			if _, err = rs.conn.Write(b); err != nil {
				if !wamp.IsGoodbyeAck(msg) {
					rs.log.Println("Error writing message:", msg, err)
				}
				continue sendLoop
			}
		case <-senderDone:
			return
		}
	}
}

// recvHandler pulls messages from the socket and pushes them to the read
// channel.
func (rs *rawSocketPeer) recvHandler() {
	// When done, close read channel to cause router to remove session if not
	// already removed.
	defer close(rs.rd)
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
				// Peer received control message to close.  Cause sendHandler
				// to exit without closing the write channel (in case writes
				// still happening) and discard any queued messages.
				rs.cancelSender()
				<-rs.writerDone

				// Close socket connection.
				rs.conn.Close()
			}
			return
		}

		length := bytesToInt(header[1:])
		if length > rs.recvLimit {
			rs.log.Print("Received message that exceeded size limit, closing")
			rs.conn.Close()
			break
		}

		var msg wamp.Message
		switch header[0] & 0x07 {
		case 0: // WAMP message
			buf := make([]byte, length)
			_, err = io.ReadFull(rs.conn, buf)
			if err != nil {
				rs.log.Println("Error reading message:", err)
				rs.conn.Close()
				return
			}
			msg, err = rs.serializer.Deserialize(buf)
			if err != nil {
				// TODO: something more than merely logging?
				rs.log.Println("Cannot deserialize peer message:", err)
				continue MsgLoop
			}
		case 1: // PING
			header[0] = 0x02
			if _, err = rs.conn.Write(header[:]); err != nil {
				rs.log.Println("Error writing header responding to PING:", err)
				rs.conn.Close()
				return
			}
			if _, err = io.CopyN(rs.conn, rs.conn, int64(length)); err != nil {
				rs.log.Println("Error responding to PING:", err)
				rs.conn.Close()
				return
			}
			continue MsgLoop
		case 2: // PONG
			_, err = io.CopyN(ioutil.Discard, rs.conn, int64(length))
			if err != nil {
				rs.log.Println("Error reading PONG:", err)
				rs.conn.Close()
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
			// If closed, try for one second to send the last message and then
			// exit recvHandler.
			select {
			case rs.rd <- msg:
			case <-time.After(time.Second):
				rs.conn.Close()
				return
			}
		}
	}
}

// clientHandshake handles the client-side of a RawSocket transport handshake.
func clientHandshake(conn net.Conn, logger stdlog.StdLog, protocol byte, recvLimit int) (*rawSocketPeer, error) {
	maxRecvLen := fitRecvLimit(recvLimit)

	_, err := conn.Write([]byte{magic, (maxRecvLen&0xf)<<4 | protocol, 0, 0})
	if err != nil {
		return nil, fmt.Errorf("error sending handshake: %s", err)
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
		conn.Write([]byte{magic, byte(0x3 << 4), 0, 0})
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
		conn.Write([]byte{magic, byte(0x1 << 4), 0, 0})
		return nil, errors.New("serializer unsupported")
	}

	maxRecvLen := fitRecvLimit(recvLimit)

	_, err := conn.Write([]byte{magic, maxRecvLen<<4 | serialization, 0, 0})
	if err != nil {
		return nil, fmt.Errorf("error sending handshake: %s", err)
	}

	sendLimit := byteToLength(buf[1] >> 4)
	recvLimit = byteToLength(maxRecvLen)
	return newRawSocketPeer(conn, serializer, logger, sendLimit, recvLimit, outQueueSize), nil
}

// fitRecvLimit finds the power of 2 that is greater than or equal to the
// specified receive limit.  This value is returned as the RawSocket transport
// byte representation of this value.
func fitRecvLimit(recvLimit int) byte {
	if recvLimit > 0 {
		for b := byte(0); b < 0xf; b++ {
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
	for i := len(b) - 1; i >= 0; i-- {
		n |= uint(b[i]) << shift
		shift += 8
	}
	return int(n)
}

// byteToLength returns the value corresponding to a RawSocket lenght byte.
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
	case serialize.JSON:
		return rawsocketJSON, nil
	case serialize.MSGPACK:
		return rawsocketMsgpack, nil
	case serialize.CBOR:
		return rawsocketCBOR, nil
	default:
		return 0, errors.New("serialization not supported by rawsocket")
	}
}
