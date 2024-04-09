package transport_test

import (
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/transport/serialize"
)

type mockWSConnection struct{}

func (m mockWSConnection) Close() error { return nil }

func (m mockWSConnection) WriteControl(messageType int, data []byte, deadline time.Time) error {
	return nil
}

func (m mockWSConnection) WriteMessage(messageType int, data []byte) error {
	return nil
}

func (m mockWSConnection) ReadMessage() (messageType int, p []byte, err error) {
	err = fmt.Errorf("implement me")
	return
}

func (m mockWSConnection) SetPongHandler(h func(appData string) error) {}

func (m mockWSConnection) SetPingHandler(h func(appData string) error) {}

func (m mockWSConnection) Subprotocol() string { return "" }

func newMockSession() transport.WebsocketConnection {
	return &mockWSConnection{}
}

func TestCloseWebsocketPeer(t *testing.T) {
	peer := transport.NewWebsocketPeer(newMockSession(), &serialize.JSONSerializer{}, websocket.TextMessage, log.Default(), 0, 0)

	// Close the client connection.
	peer.Close()

	// Try closing the client connection again. It should not cause an error.
	peer.Close()
}
