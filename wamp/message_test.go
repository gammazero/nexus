package wamp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

func chkMsgType(mt wamp.MessageType, expect string, t *testing.T) {
	m := wamp.NewMessage(mt)
	mt2 := m.MessageType()
	require.Equal(t, mt, mt2, "Created wrong message type")
	require.Equal(t, expect, mt2.String(), "Wrong message type string")
}

func TestMessage(t *testing.T) {
	chkMsgType(wamp.HELLO, "HELLO", t)
	chkMsgType(wamp.WELCOME, "WELCOME", t)
	chkMsgType(wamp.ABORT, "ABORT", t)
	chkMsgType(wamp.CHALLENGE, "CHALLENGE", t)
	chkMsgType(wamp.AUTHENTICATE, "AUTHENTICATE", t)
	chkMsgType(wamp.GOODBYE, "GOODBYE", t)
	chkMsgType(wamp.ERROR, "ERROR", t)
	chkMsgType(wamp.PUBLISH, "PUBLISH", t)
	chkMsgType(wamp.PUBLISHED, "PUBLISHED", t)
	chkMsgType(wamp.SUBSCRIBE, "SUBSCRIBE", t)
	chkMsgType(wamp.SUBSCRIBED, "SUBSCRIBED", t)
	chkMsgType(wamp.UNSUBSCRIBE, "UNSUBSCRIBE", t)
	chkMsgType(wamp.UNSUBSCRIBED, "UNSUBSCRIBED", t)
	chkMsgType(wamp.EVENT, "EVENT", t)
	chkMsgType(wamp.CALL, "CALL", t)
	chkMsgType(wamp.CANCEL, "CANCEL", t)
	chkMsgType(wamp.RESULT, "RESULT", t)
	chkMsgType(wamp.REGISTER, "REGISTER", t)
	chkMsgType(wamp.REGISTERED, "REGISTERED", t)
	chkMsgType(wamp.UNREGISTER, "UNREGISTER", t)
	chkMsgType(wamp.UNREGISTERED, "UNREGISTERED", t)
	chkMsgType(wamp.INVOCATION, "INVOCATION", t)
	chkMsgType(wamp.INTERRUPT, "INTERRUPT", t)
	chkMsgType(wamp.YIELD, "YIELD", t)

	m := wamp.NewMessage(99999)
	require.Nil(t, m, "Message should be nil for bad type")
}
