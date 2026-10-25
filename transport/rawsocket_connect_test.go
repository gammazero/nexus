package transport_test

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
)

// TestConnectRawSocketPeerBadNetworkType pins the early validation:
// ConnectRawSocketPeer rejects unsupported network types before
// attempting any I/O.
func TestConnectRawSocketPeerBadNetworkType(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	peer, err := transport.ConnectRawSocketPeer(t.Context(),
		"udp", "127.0.0.1:0", serialize.JSON, nil, logger, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported network type")
	require.Nil(t, peer)
}

// TestConnectRawSocketPeerBadSerialization pins the second validation
// step: an unsupported serialization value is rejected before the dial.
func TestConnectRawSocketPeerBadSerialization(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	const bogusSerialization = serialize.Serialization(99)
	peer, err := transport.ConnectRawSocketPeer(t.Context(),
		"tcp", "127.0.0.1:0", bogusSerialization, nil, logger, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "serialization not supported")
	require.Nil(t, peer)
}

// TestConnectRawSocketPeerDialFailure pins the dial-error path: the
// address is well-formed but no server is listening. net.Dial
// returns an error; ConnectRawSocketPeer surfaces it directly.
func TestConnectRawSocketPeerDialFailure(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	// Use a closed-immediately listener to get a guaranteed-unused
	// port, then dial it after closing.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	peer, err := transport.ConnectRawSocketPeer(ctx,
		"tcp", addr, serialize.JSON, nil, logger, 0)
	require.Error(t, err)
	require.Nil(t, peer)
}

// TestConnectRawSocketPeerHandshakeFailure checks that ConnectRawSocketPeer
// returns an error, and no peer, when the server answers the handshake with
// something other than a RawSocket handshake reply.
func TestConnectRawSocketPeerHandshakeFailure(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		// Send 4 bytes that are NOT the RawSocket handshake reply.
		_, _ = conn.Write([]byte{0x00, 0x00, 0x00, 0x00})
		_ = conn.Close()
	}()

	peer, err := transport.ConnectRawSocketPeer(t.Context(),
		"tcp", ln.Addr().String(), serialize.JSON, nil, logger, 0)
	require.Error(t, err, "handshake should fail when server doesn't speak RawSocket")
	require.Nil(t, peer)
}

// TestConnectRawSocketPeerTLSHandshakeFailure checks that ConnectRawSocketPeer
// returns an error, and no peer, when the TLS handshake fails because the
// server does not speak TLS.
func TestConnectRawSocketPeerTLSHandshakeFailure(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		// Read whatever the client sends (the TLS ClientHello),
		// then close. The TLS handshake on the client side errors
		// when the server doesn't reply with a ServerHello.
		buf := make([]byte, 1024)
		_, _ = conn.Read(buf)
		_ = conn.Close()
	}()

	tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	peer, err := transport.ConnectRawSocketPeer(ctx,
		"tcp", ln.Addr().String(), serialize.JSON, tlsCfg, logger, 0)
	require.Error(t, err, "TLS handshake should fail against plain-TCP server")
	require.Nil(t, peer)
}
