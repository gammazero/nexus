package router

import (
	"context"
	"testing"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
)

const tcpAddr = "127.0.0.1:8181"

func TestRSHandshakeJSON(t *testing.T) {
	checkGoLeaks(t)

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

	client.Send(&wamp.Hello{Realm: testRealm, Details: clientRoles})
	msg, ok := <-client.Recv()
	require.True(t, ok, "recv chan closed")

	_, ok = msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME")
}

func TestRSHandshakeMsgpack(t *testing.T) {
	checkGoLeaks(t)

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

	client.Send(&wamp.Hello{Realm: testRealm, Details: clientRoles})
	msg, ok := <-client.Recv()
	require.True(t, ok, "Receive buffer closed")

	_, ok = msg.(*wamp.Welcome)
	require.True(t, ok, "expected WELCOME")
}
