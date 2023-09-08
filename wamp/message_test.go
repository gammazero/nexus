package wamp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func chkMsgType(mt MessageType, expect string, t *testing.T) {
	m := NewMessage(mt)
	mt2 := m.MessageType()
	require.Equal(t, mt, mt2, "Created wrong message type")
	require.Equal(t, expect, mt2.String(), "Wrong message type string")
}

func TestMessage(t *testing.T) {
	chkMsgType(HELLO, "HELLO", t)
	chkMsgType(WELCOME, "WELCOME", t)
	chkMsgType(ABORT, "ABORT", t)
	chkMsgType(CHALLENGE, "CHALLENGE", t)
	chkMsgType(AUTHENTICATE, "AUTHENTICATE", t)
	chkMsgType(GOODBYE, "GOODBYE", t)
	chkMsgType(ERROR, "ERROR", t)
	chkMsgType(PUBLISH, "PUBLISH", t)
	chkMsgType(PUBLISHED, "PUBLISHED", t)
	chkMsgType(SUBSCRIBE, "SUBSCRIBE", t)
	chkMsgType(SUBSCRIBED, "SUBSCRIBED", t)
	chkMsgType(UNSUBSCRIBE, "UNSUBSCRIBE", t)
	chkMsgType(UNSUBSCRIBED, "UNSUBSCRIBED", t)
	chkMsgType(EVENT, "EVENT", t)
	chkMsgType(CALL, "CALL", t)
	chkMsgType(CANCEL, "CANCEL", t)
	chkMsgType(RESULT, "RESULT", t)
	chkMsgType(REGISTER, "REGISTER", t)
	chkMsgType(REGISTERED, "REGISTERED", t)
	chkMsgType(UNREGISTER, "UNREGISTER", t)
	chkMsgType(UNREGISTERED, "UNREGISTERED", t)
	chkMsgType(INVOCATION, "INVOCATION", t)
	chkMsgType(INTERRUPT, "INTERRUPT", t)
	chkMsgType(YIELD, "YIELD", t)

	m := NewMessage(99999)
	require.Nil(t, m, "Message should be nil for bad type")
}
