package router //nolint:testpackage

import (
	"context"
	"log"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

const tcpAddr = "127.0.0.1:8181"

func TestRSHandshakeJSON(t *testing.T) {
	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()
	clsr, err := NewRawSocketServer(r).ListenAndServe("tcp", tcpAddr)
	require.NoError(t, err)
	defer clsr.Close()

	client, err := transport.ConnectRawSocketPeer(context.Background(), "tcp",
		tcpAddr, serialize.JSON, nil, r.Logger(), 0)
	require.NoError(t, err)
	defer client.Close()

	client.Send() <- &wamp.Hello{Realm: testRealm, Details: clientRoles}
	msg, ok := <-client.Recv()
	require.True(t, ok, "recv chan closed")

	_, ok = msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME")
}

func TestRSHandshakeMsgpack(t *testing.T) {
	r, err := NewRouter(routerConfig, nil)
	require.NoError(t, err)
	defer r.Close()
	clsr, err := NewRawSocketServer(r).ListenAndServe("tcp", tcpAddr)
	require.NoError(t, err)
	defer clsr.Close()

	client, err := transport.ConnectRawSocketPeer(context.Background(), "tcp",
		tcpAddr, serialize.MSGPACK, nil, r.Logger(), 0)
	require.NoError(t, err)
	defer client.Close()

	client.Send() <- &wamp.Hello{Realm: testRealm, Details: clientRoles}
	msg, ok := <-client.Recv()
	require.True(t, ok, "Receive buffer closed")

	_, ok = msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME")
}

// A raw socket HELLO for a realm the router does not have is ABORTed, and the
// log line names the peer.
func TestRSAttachFailureLogsPeer(t *testing.T) {
	logBuf := &lockedBuffer{}
	r, err := NewRouter(routerConfig, log.New(logBuf, "", 0))
	require.NoError(t, err)
	defer r.Close()
	clsr, err := NewRawSocketServer(r).ListenAndServe("tcp", tcpAddr)
	require.NoError(t, err)
	defer clsr.Close()

	client, err := transport.ConnectRawSocketPeer(context.Background(), "tcp",
		tcpAddr, serialize.JSON, nil, r.Logger(), 0)
	require.NoError(t, err)
	defer client.Close()

	client.Send() <- &wamp.Hello{Realm: "realm-1-1-dev", Details: clientRoles}
	msg, ok := <-client.Recv()
	require.True(t, ok, "recv chan closed")
	_, ok = msg.(*wamp.Abort)
	require.True(t, ok, "expected ABORT")

	line := requireLogLine(t, logBuf, "Error attaching to router")
	require.Contains(t, line, `no realm "realm-1-1-dev" exists`)
	require.Regexp(t, `\(peer 127\.0\.0\.1:\d+\)`, line)
}
