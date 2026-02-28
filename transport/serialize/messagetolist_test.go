package serialize //nolint:testpackage

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

func TestListToMsg(t *testing.T) {
	const msgType = wamp.PUBLISH

	pubArgs := []string{"hello", "nexus", "wamp", "router"}

	// Deserializing a slice into a message.
	elems := wamp.List{msgType, 123, wamp.Dict{},
		"some.valid.topic", pubArgs}
	msg, err := listToMsg(msgType, elems)
	require.NoError(t, err)

	// Check that message is a Publish message.
	pubMsg, ok := msg.(*wamp.Publish)
	require.True(t, ok, "got incorrect message type")

	// Check arguments.
	require.Equal(t, len(pubArgs), len(pubMsg.Arguments), "wrong number of message arguments")
	for i := 0; i < len(pubArgs); i++ {
		require.Equalf(t, pubArgs[i], pubMsg.Arguments[i], "argument %d has wrong value", i)
	}
}

func TestMsgToList(t *testing.T) {
	testMsgToList := func(args wamp.List, kwArgs wamp.Dict, omit int, message string) error {
		msg := &wamp.Event{Subscription: 0, Publication: 0, Details: nil, Arguments: args, ArgumentsKw: kwArgs}
		numField := reflect.ValueOf(msg).Elem().NumField() + 1 // +1 for type
		expect := numField - omit
		list := msgToList(msg)
		if len(list) != expect {
			return fmt.Errorf(
				"Wrong number of fields: got %d, expected %d, for %s",
				len(list), expect, message)
		}
		return nil
	}

	err := testMsgToList(nil, nil, 2, "nil args, nil kwArgs")
	require.NoError(t, err)

	err = testMsgToList(wamp.List{}, make(wamp.Dict), 2, "empty args, empty kwArgs")
	require.NoError(t, err)

	err = testMsgToList(wamp.List{1}, nil, 1, "non-empty args, nil kwArgs")
	require.NoError(t, err)

	err = testMsgToList(nil, wamp.Dict{"a": nil}, 0, "nil args, non-empty kwArgs")
	require.NoError(t, err)

	err = testMsgToList(wamp.List{1}, make(wamp.Dict), 1, "non-empty args, empty kwArgs")
	require.NoError(t, err)

	err = testMsgToList(wamp.List{}, wamp.Dict{"a": nil}, 0, "empty args, non-empty kwArgs")
	require.NoError(t, err)

	err = testMsgToList(wamp.List{1}, wamp.Dict{"a": nil}, 0, "test message one")
	require.NoError(t, err)
}
