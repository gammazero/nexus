package transport_test

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// requireDoneOnDisconnect checks that the Done channel of server, a peer that
// the router uses for a client, is closed when client disconnects, while the
// router does not read the messages that client sends. That is what happens
// while the router waits for a client to read its replies, and the router
// relies on Done to see that the client is gone.
func requireDoneOnDisconnect(t *testing.T, server, client wamp.Peer) {
	t.Helper()
	donePeer, ok := server.(wamp.DonePeer)
	require.True(t, ok, "peer does not implement wamp.DonePeer")
	done := donePeer.Done()
	defer server.Close()

	// The server does not read these, so its receiver blocks delivering one.
	for i := 1; i <= 2; i++ {
		client.Send() <- &wamp.Publish{Request: wamp.ID(i), Topic: "nexus.test"}
	}

	// Keep sending to the client, as the router does for a backlog of replies.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for i := 1; ; i++ {
			select {
			case server.Send() <- &wamp.Published{Request: wamp.ID(i), Publication: 1}:
			case <-done:
				return
			case <-stop:
				return
			}
		}
	}()

	select {
	case <-done:
		require.FailNow(t, "done before the client disconnected")
	case <-time.After(100 * time.Millisecond):
	}

	client.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "done not closed after the client disconnected")
	}
}

// TestRawSocketDoneOnDisconnect checks Done for a rawsocket peer.
func TestRawSocketDoneOnDisconnect(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	accepted := make(chan wamp.Peer, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			close(accepted)
			return
		}
		peer, err := transport.AcceptRawSocket(conn, logger, 0, 2)
		if err != nil {
			close(accepted)
			return
		}
		accepted <- peer
	}()

	client, err := transport.ConnectRawSocketPeer(context.Background(), "tcp",
		ln.Addr().String(), serialize.JSON, nil, logger, 0)
	require.NoError(t, err)
	defer client.Close()
	server, ok := <-accepted
	require.True(t, ok, "rawsocket accept failed")

	requireDoneOnDisconnect(t, server, client)
}

// TestWebsocketDoneOnDisconnect checks Done for a websocket peer.
func TestWebsocketDoneOnDisconnect(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	accepted := make(chan wamp.Peer, 1)
	upgrader := websocket.Upgrader{Subprotocols: []string{"wamp.2.json"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			close(accepted)
			return
		}
		accepted <- transport.NewWebsocketPeer(conn, &serialize.JSONSerializer{},
			websocket.TextMessage, logger, 0, 2)
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := transport.ConnectWebsocketPeer(context.Background(), url,
		serialize.JSON, nil, logger, nil)
	require.NoError(t, err)
	defer client.Close()
	server, ok := <-accepted
	require.True(t, ok, "websocket upgrade failed")

	requireDoneOnDisconnect(t, server, client)
}

// TestWebsocketKeepAliveWhileNotRead checks that a websocket peer with
// keepalive stays connected while the router does not read the messages it
// receives, as when the router waits for a client to read its replies. The
// peer cannot read the client's pongs while it waits to deliver a message, so
// it must not count them as missed, and it must not let its read deadline
// expire.
func TestWebsocketKeepAliveWhileNotRead(t *testing.T) {
	const keepAlive = 50 * time.Millisecond
	logger := log.New(io.Discard, "", 0)
	accepted := make(chan wamp.Peer, 1)
	upgrader := websocket.Upgrader{Subprotocols: []string{"wamp.2.json"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			close(accepted)
			return
		}
		accepted <- transport.NewWebsocketPeer(conn, &serialize.JSONSerializer{},
			websocket.TextMessage, logger, keepAlive, 2)
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := transport.ConnectWebsocketPeer(context.Background(), url,
		serialize.JSON, nil, logger, nil)
	require.NoError(t, err)
	defer client.Close()
	server, ok := <-accepted
	require.True(t, ok, "websocket upgrade failed")
	defer server.Close()

	// The router does not read these for many keepalive intervals.
	client.Send() <- &wamp.Publish{Request: 1, Topic: "nexus.test"}
	client.Send() <- &wamp.Publish{Request: 2, Topic: "nexus.test"}
	time.Sleep(10 * keepAlive)

	for i := 1; i <= 3; i++ {
		if i == 3 {
			select {
			case client.Send() <- &wamp.Publish{Request: 3, Topic: "nexus.test"}:
			case <-time.After(time.Second):
				require.FailNow(t, "client cannot send: connection closed while not read")
			}
		}
		msg, err := wamp.RecvTimeout(server, time.Second)
		require.NoError(t, err, "connection closed while not read")
		require.Equal(t, wamp.ID(i), msg.(*wamp.Publish).Request)
	}
	server.Send() <- &wamp.Published{Request: 3, Publication: 1}
	msg, err := wamp.RecvTimeout(client, time.Second)
	require.NoError(t, err)
	require.IsType(t, &wamp.Published{}, msg)
	select {
	case <-server.(wamp.DonePeer).Done():
		require.FailNow(t, "connection closed while not read")
	default:
	}
}
