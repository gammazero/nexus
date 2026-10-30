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

	// Deserializing a slice into a message. Use uint64 for the message type
	// to simulate what JSON/CBOR deserializers produce.
	elems := wamp.List{uint64(msgType), 123, wamp.Dict{}, "some.valid.topic", pubArgs}
	msg, err := listToMsg(msgType, elems)
	require.NoError(t, err)

	// Check that message is a Publish message.
	pubMsg, ok := msg.(*wamp.Publish)
	require.True(t, ok, "got incorrect message type")

	// Check arguments.
	require.Equal(t, len(pubArgs), len(pubMsg.Arguments), "wrong number of message arguments")
	for i, pubArg := range pubArgs {
		require.Equalf(t, pubArg, pubMsg.Arguments[i], "argument %d has wrong value", i)
	}
}

// TestListToMsgFields checks how listToMsg sets each field of a message from
// the list: by assigning, by converting, or from what a pointer points to.
func TestListToMsgFields(t *testing.T) {
	// Values are assigned or converted to the field types.
	msg, err := listToMsg(wamp.PUBLISH, []any{
		uint64(wamp.PUBLISH),
		int64(7),                         // converted to ID
		map[string]any{"acknowledge": 1}, // converted to Dict
		"a.topic",                        // converted to URI
		[]any{"x", 1},                    // converted to List
		wamp.Dict{"k": "v"},              // assigned
	})
	require.NoError(t, err)
	require.Equal(t, &wamp.Publish{
		Request:     7,
		Options:     wamp.Dict{"acknowledge": 1},
		Topic:       "a.topic",
		Arguments:   wamp.List{"x", 1},
		ArgumentsKw: wamp.Dict{"k": "v"},
	}, msg)

	// A pointer is followed to its value.
	topic := "a.topic"
	msg, err = listToMsg(wamp.SUBSCRIBE, []any{
		uint64(wamp.SUBSCRIBE), uint64(1), &wamp.Dict{"match": "prefix"}, &topic,
	})
	require.NoError(t, err)
	require.Equal(t, &wamp.Subscribe{
		Request: 1,
		Options: wamp.Dict{"match": "prefix"},
		Topic:   "a.topic",
	}, msg)

	// A nil value leaves the field unset, and missing values at the end leave the
	// remaining fields unset.
	msg, err = listToMsg(wamp.PUBLISH, []any{
		uint64(wamp.PUBLISH), uint64(1), nil, "a.topic",
	})
	require.NoError(t, err)
	require.Equal(t, &wamp.Publish{Request: 1, Topic: "a.topic"}, msg)

	// Values after the last field are ignored.
	msg, err = listToMsg(wamp.UNSUBSCRIBE, []any{
		uint64(wamp.UNSUBSCRIBE), uint64(1), uint64(2), "extra", "more",
	})
	require.NoError(t, err)
	require.Equal(t, &wamp.Unsubscribe{Request: 1, Subscription: 2}, msg)
}

func TestListToMsgErrors(t *testing.T) {
	_, err := listToMsg(wamp.MessageType(999), []any{uint64(999)})
	require.EqualError(t, err, "unsupported message type")

	// A value of a different kind than its field is an error.
	_, err = listToMsg(wamp.SUBSCRIBE, []any{
		uint64(wamp.SUBSCRIBE), "not an ID", wamp.Dict{}, "a.topic",
	})
	require.EqualError(t, err,
		"field 1 not recognized, has string, want wamp.ID")

	_, err = listToMsg(wamp.SUBSCRIBE, []any{
		uint64(wamp.SUBSCRIBE), uint64(1), wamp.List{"not", "a", "dict"},
	})
	require.EqualError(t, err,
		"field 2 not recognized, has wamp.List, want wamp.Dict")

	// An integer for a string field is an error, and is not converted to a string
	// by taking it as a rune, which would make topic 65 into "A".
	_, err = listToMsg(wamp.SUBSCRIBE, []any{
		uint64(wamp.SUBSCRIBE), uint64(1), wamp.Dict{}, 65,
	})
	require.EqualError(t, err, "field 3 not recognized, has int, want wamp.URI")
	_, err = listToMsg(wamp.SUBSCRIBE, []any{
		uint64(wamp.SUBSCRIBE), uint64(1), wamp.Dict{}, uint64(65),
	})
	require.EqualError(t, err,
		"field 3 not recognized, has uint64, want wamp.URI")
}

// TestDeserializeIntegerTopic checks that each serializer rejects a message
// with an integer where a URI must be, instead of making it into a string.
func TestDeserializeIntegerTopic(t *testing.T) {
	// Each is SUBSCRIBE with the integer 65 as its topic: [32, 1, {}, 65].
	for _, tc := range []struct {
		name string
		s    Serializer
		data []byte
	}{
		{"json", &JSONSerializer{}, []byte(`[32,1,{},65]`)},
		{"msgpack", &MessagePackSerializer{}, []byte{0x94, 0x20, 0x01, 0x80, 0x41}},
		{"cbor", &CBORSerializer{}, []byte{0x84, 0x18, 0x20, 0x01, 0xa0, 0x18, 0x41}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := tc.s.Deserialize(tc.data)
			require.Error(t, err, "got %#v", msg)
		})
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
