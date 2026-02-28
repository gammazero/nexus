package serialize_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

var dataItem = []map[string]any{{
	"Arguments":   wamp.List{1, "2", true},
	"ArgumentsKw": wamp.Dict{"prop1": 1, "prop2": "2", "prop3": true},
}}

// Not used for now
//func hasRole(details wamp.Dict, role string) bool {
//	_, err := wamp.DictValue(details, []string{"roles", role})
//	return err == nil
//}

func hasFeature(details wamp.Dict, role, feature string) bool {
	b, _ := wamp.DictFlag(details, []string{"roles", role, "features", feature})
	return b
}

func detailRolesFeatures() wamp.Dict {
	return wamp.Dict{
		"roles": wamp.Dict{
			"publisher": wamp.Dict{
				"features": wamp.Dict{
					"subscriber_blackwhite_listing": true,
				},
			},
			"subscriber": wamp.Dict{},
			"callee":     wamp.Dict{},
			"caller":     wamp.Dict{},
		},
		"nothere": nil,
	}
}

func compareDeserializedSerializedDataItem(t *testing.T, item any) {
	resA, ok := item.([]any)
	require.True(t, ok, "deserialization to array error")
	resT, ok := resA[0].(map[string]any)
	require.True(t, ok, "deserialization to hash-table error")

	arr, ok := resT["Arguments"].([]any)
	require.True(t, ok, "Arguments property is missed")

	switch arr[0].(type) {
	case int64:
		require.Equal(t, int64(1), arr[0], "Arguments[0] expected 1 value")
	case uint64:
		require.Equal(t, uint64(1), arr[0], "Arguments[0] expected 1 value")
	case int:
		require.Equal(t, 1, arr[0], "Arguments[0] expected 1 value")
	default:
		require.FailNow(t, "Arguments[0] of unexpected type")
	}

	require.Equal(t, "2", arr[1], "Arguments[1] expected '2' value")
	b, _ := wamp.AsBool(arr[2])
	require.True(t, b, "Arguments[2] expected true value")

	mapV, ok := resT["ArgumentsKw"].(map[string]any)
	require.True(t, ok, "ArgumentsKw property is missed")

	val, ok := mapV["prop1"]
	require.True(t, ok, "ArgumentsKw prop1 is missing")

	switch val.(type) {
	case int64:
		require.Equal(t, int64(1), val, "ArgumentsKw prop1 expected 1 value")
	case uint64:
		require.Equal(t, uint64(1), val, "ArgumentsKw prop1 expected 1 value")
	case int:
		require.Equal(t, 1, val, "ArgumentsKw prop1 expected 1 value")
	default:
		require.FailNow(t, "ArgumentsKw prop1 of unexpected type")
	}

	val, ok = mapV["prop2"]
	require.True(t, ok, "ArgumentsKw prop2 is missing")
	require.Equal(t, string('2'), val)

	val, ok = mapV["prop3"]
	require.True(t, ok, "ArgumentsKw prop1 is missing")
	b, _ = wamp.AsBool(val)
	require.True(t, b, "expected true value")
}

func TestJSONSerialize(t *testing.T) {
	details := detailRolesFeatures()
	hello := &wamp.Hello{Realm: "nexus.realm", Details: details}

	s := &serialize.JSONSerializer{}
	b, err := s.Serialize(hello)
	require.NoError(t, err)
	require.NotZero(t, len(b), "no serialized data")

	msg, err := s.Deserialize(b)
	require.NoError(t, err)
	require.Equal(t, wamp.HELLO, msg.MessageType(), "desrialization to wrong message type")
	has := hasFeature(hello.Details, "publisher", "subscriber_blackwhite_listing")
	require.True(t, has, "did not deserialize message details")

	val, ok := hello.Details["nothere"]
	require.True(t, ok, "nil value item 'nothere' is missing")
	require.Nil(t, val, "expected nil value item 'nothere'")
}

func TestJSONDeserialize(t *testing.T) {
	s := &serialize.JSONSerializer{}

	data := `[1,"nexus.realm",{}]`
	expect := &wamp.Hello{Realm: "nexus.realm", Details: wamp.Dict{}}
	msg, err := s.Deserialize([]byte(data))
	require.NoError(t, err)
	require.Equal(t, expect.MessageType(), msg.MessageType())
	require.Equal(t, expect, msg)

	emptyData := `[]`
	_, err = s.Deserialize([]byte(emptyData))
	require.Error(t, err, "Empty message should be errored while decoding")
}

func TestJSONSerializeDataItem(t *testing.T) {
	s := &serialize.JSONSerializer{}
	b, err := s.SerializeDataItem(dataItem)
	require.NoError(t, err)
	require.NotZero(t, len(b), "no serialized data")

	var res any
	err = s.DeserializeDataItem(b, &res)
	require.NoError(t, err)

	compareDeserializedSerializedDataItem(t, res)
}

func TestJSONDeserializeDataItem(t *testing.T) {
	s := &serialize.JSONSerializer{}

	data := `[{"Arguments":[1,"2",true],"ArgumentsKw":{"prop1":1,"prop2":"2","prop3":true}}]`
	var msg any
	err := s.DeserializeDataItem([]byte(data), &msg)
	require.NoError(t, err)
	compareDeserializedSerializedDataItem(t, msg)

	var val []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}
	err = s.DeserializeDataItem([]byte(data), &val)
	require.NoError(t, err)
	expected := []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}{
		{
			Arguments: []any{
				uint64(1), "2", true,
			},
			ArgumentsKw: map[string]any{
				"prop1": uint64(1), "prop2": "2", "prop3": true,
			},
		},
	}
	require.Equal(t, expected, val)
}

func TestCBORSerialize(t *testing.T) {
	details := detailRolesFeatures()
	hello := &wamp.Hello{Realm: "nexus.realm", Details: details}

	s := &serialize.CBORSerializer{}
	b, err := s.Serialize(hello)
	require.NoError(t, err)
	require.NotZero(t, len(b), "no serialized data")

	msg, err := s.Deserialize(b)
	require.NoError(t, err)
	require.Equal(t, wamp.HELLO, msg.MessageType(), "desrialization to wrong message type")
	has := hasFeature(hello.Details, "publisher", "subscriber_blackwhite_listing")
	require.True(t, has, "did not deserialize message details")

	val, ok := hello.Details["nothere"]
	require.True(t, ok, "nil value item 'nothere' is missing")
	require.Nil(t, val, "expected nil value item 'nothere'")
}

func TestCBORDeserialize(t *testing.T) {
	s := &serialize.CBORSerializer{}

	// this is the CBOR representation of the message above
	data := []byte{
		0x83, 0x01, 0x6b, 0x6e, 0x65, 0x78, 0x75, 0x73, 0x2e, 0x72, 0x65, 0x61,
		0x6c, 0x6d, 0xa0,
	}
	expect := &wamp.Hello{Realm: "nexus.realm", Details: wamp.Dict{}}

	msg, err := s.Deserialize(data)
	require.NoError(t, err)
	require.Equal(t, expect.MessageType(), msg.MessageType())
	require.Equal(t, expect, msg)

	emptyData := []byte{0x80}
	_, err = s.Deserialize(emptyData)
	require.Error(t, err, "Empty message should be errored while decoding")
}

func TestCBORSerializeDataItem(t *testing.T) {
	s := &serialize.CBORSerializer{}
	b, err := s.SerializeDataItem(dataItem)
	require.NoError(t, err)
	require.NotZero(t, len(b), "no serialized data")

	var res any
	err = s.DeserializeDataItem(b, &res)
	require.NoError(t, err)
	compareDeserializedSerializedDataItem(t, res)
}

func TestCBORDeserializeDataItem(t *testing.T) {
	s := &serialize.CBORSerializer{}

	// this is the CBOR representation of the message above
	data := []byte{
		0x81, 0xa2, 0x69, 0x41, 0x72, 0x67, 0x75, 0x6D, 0x65, 0x6E, 0x74,
		0x73, 0x83, 0x01, 0x61, 0x32, 0xf5, 0x6b, 0x41, 0x72, 0x67, 0x75,
		0x6D, 0x65, 0x6E, 0x74, 0x73, 0x4B, 0x77, 0xa3, 0x65, 0x70, 0x72,
		0x6F, 0x70, 0x31, 0x01, 0x65, 0x70, 0x72, 0x6F, 0x70, 0x32, 0x61,
		0x32, 0x65, 0x70, 0x72, 0x6F, 0x70, 0x33, 0xf5,
	}
	var msg any
	err := s.DeserializeDataItem(data, &msg)
	require.NoError(t, err)
	compareDeserializedSerializedDataItem(t, msg)

	var val []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}
	err = s.DeserializeDataItem(data, &val)
	require.NoError(t, err)
	expected := []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}{
		{
			Arguments: []any{
				uint64(1), "2", true,
			},
			ArgumentsKw: map[string]any{
				"prop1": uint64(1), "prop2": "2", "prop3": true,
			},
		},
	}
	require.Equal(t, expected, val)
}

func TestMessagePackSerialize(t *testing.T) {
	hello := &wamp.Hello{Realm: "nexus.realm", Details: detailRolesFeatures()}

	s := &serialize.MessagePackSerializer{}
	b, err := s.Serialize(hello)
	require.NoError(t, err)
	require.NotZero(t, len(b), "no serialized data")
	msg, err := s.Deserialize(b)
	require.NoError(t, err)
	require.Equal(t, wamp.HELLO, msg.MessageType(), "desrialization to wrong message type")
	has := hasFeature(hello.Details, "publisher", "subscriber_blackwhite_listing")
	require.True(t, has, "did not deserialize message details")

	val, ok := hello.Details["nothere"]
	require.True(t, ok, "nil value item 'nothere' is missing")
	require.Nil(t, val, "expected nil value item 'nothere'")
}

func TestMessagePackDeserialize(t *testing.T) {
	s := &serialize.MessagePackSerializer{}

	data := []byte{0x93, 0x01, 0xab, 0x6e, 0x65, 0x78, 0x75, 0x73, 0x2e, 0x72, 0x65, 0x61, 0x6c, 0x6d, 0x80}
	expect := &wamp.Hello{Realm: "nexus.realm", Details: wamp.Dict{}}
	msg, err := s.Deserialize(data)
	require.NoError(t, err)
	require.Equal(t, expect.MessageType(), msg.MessageType(), "Incorrect message type")
	require.Equal(t, expect, msg)

	emptyData := []byte{0x90}
	_, err = s.Deserialize(emptyData)
	require.Error(t, err, "Empty message should be errored while decoding")
}

func TestMessagePackSerializeDataItem(t *testing.T) {
	s := &serialize.MessagePackSerializer{}
	b, err := s.SerializeDataItem(dataItem)
	require.NoError(t, err)
	require.NotZero(t, len(b), "no serialized data")
	var res any
	err = s.DeserializeDataItem(b, &res)
	require.NoError(t, err)
	compareDeserializedSerializedDataItem(t, res)
}

func TestMessagePackDeserializeDataItem(t *testing.T) {
	s := &serialize.MessagePackSerializer{}

	data := []byte{
		0x91, 0x82, 0xA9, 0x41, 0x72, 0x67, 0x75, 0x6D, 0x65, 0x6E,
		0x74, 0x73, 0x93, 0x01, 0xA1, 0x32, 0xC3, 0xAB, 0x41, 0x72,
		0x67, 0x75, 0x6D, 0x65, 0x6E, 0x74, 0x73, 0x4B, 0x77, 0x83,
		0xA5, 0x70, 0x72, 0x6F, 0x70, 0x31, 0x01, 0xA5, 0x70, 0x72,
		0x6F, 0x70, 0x32, 0xA1, 0x32, 0xA5, 0x70, 0x72, 0x6F, 0x70,
		0x33, 0xC3,
	}
	var msg any
	err := s.DeserializeDataItem(data, &msg)
	require.NoError(t, err)
	compareDeserializedSerializedDataItem(t, msg)

	var val []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}
	err = s.DeserializeDataItem(data, &val)
	require.NoError(t, err)
	expected := []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}{
		{
			Arguments: []any{
				int64(1), "2", true,
			},
			ArgumentsKw: map[string]any{
				"prop1": int64(1), "prop2": "2", "prop3": true,
			},
		},
	}
	require.Equal(t, expected, val)
}

func TestBinaryDataJSON(t *testing.T) {
	orig := []byte("hellowamp")

	// Calls the customer encoder: BinaryData.MarshalJSON()
	bin, err := json.Marshal(serialize.BinaryData(orig))
	require.NoError(t, err)

	expect := fmt.Sprintf(`"\u0000%s"`, base64.StdEncoding.EncodeToString(orig))
	require.Equal(t, []byte(expect), bin)

	var b serialize.BinaryData
	// Calls the customer decoder: BinaryData.UnmarshalJSON()
	err = json.Unmarshal(bin, &b)
	require.NoError(t, err)
	require.Equal(t, orig, []byte(b))
}

func TestMsgpackExtensions(t *testing.T) {
	t.Skip("MsgpackRegisterExtension not working - need fix ")
	encode := func(value reflect.Value) ([]byte, error) {
		return value.Bytes(), nil
	}
	decode := func(value reflect.Value, data []byte) error {
		value.Elem().SetBytes(data)
		return nil
	}

	err := serialize.MsgpackRegisterExtension(reflect.TypeFor[serialize.BinaryData](), 42, encode, decode)
	require.NoError(t, err)

	orig := []byte("hellowamp")
	msg := &wamp.Welcome{
		ID: wamp.ID(123),
		Details: wamp.Dict{
			"extra": serialize.BinaryData(orig),
		},
	}

	ser := serialize.MessagePackSerializer{}
	// Calls the customer encoder: BinaryData.MarshalJSON()
	bin, err := ser.Serialize(msg)
	require.NoError(t, err)

	c, err := ser.Deserialize(bin)
	require.NoError(t, err)
	m1, ok := c.(*wamp.Welcome)
	require.True(t, ok, "could not convert ot wamp.Welcome")
	require.Equal(t, m1.ID, msg.ID)
	require.Equal(t, len(m1.Details), len(msg.Details))
	v1, ok := msg.Details["extra"]
	require.True(t, ok, "msg.Details[extra] missing")
	v2, ok := m1.Details["extra"]
	require.True(t, ok, "m1.Details[extra] missing")
	vs1 := string(v1.(serialize.BinaryData))
	vs2, _ := wamp.AsString(v2)
	require.Equal(t, vs2, vs1)

	// Does not work as of commit this commit:
	// github.com/ugorji/go/codec@@20768e92ac5d44754d3ae811382dea19ec3901c
	//
	//if !reflect.DeepEqual(msg, m1) {
	//	t.Fatalf("Values are not equal: expected: %v, got: %v", msg, m1)
	//}
}

func TestMsgpackDeserializeFail(t *testing.T) {
	ser := serialize.MessagePackSerializer{}
	_, err := ser.Deserialize(nil)
	require.Error(t, err)
	_, err = ser.Deserialize([]byte{144}) // pass in a serialized empty array
	require.Error(t, err)
	_, err = ser.Deserialize([]byte{145, 161, 102}) // array containing string
	require.Error(t, err)
}

func TestMsgPackToJSON(t *testing.T) {
	arg := "this is a test"
	arg2 := map[string]any{
		"hello": "world",
	}
	pub := &wamp.Publish{
		Request:   123,
		Topic:     "msgpack.to.json",
		Arguments: wamp.List{arg, arg2},
	}
	ms := &serialize.MessagePackSerializer{}
	b, err := ms.Serialize(pub)
	require.NoError(t, err)
	require.NotZero(t, len(b), "no serialized data")
	msg, err := ms.Deserialize(b)
	require.NoError(t, err)
	p2 := msg.(*wamp.Publish)
	event := &wamp.Event{
		Subscription: 987,
		Publication:  p2.Request,
		Details:      wamp.Dict{"hello": "world"},
		Arguments:    p2.Arguments,
	}

	js := &serialize.JSONSerializer{}
	b, err = js.Serialize(event)
	require.NoError(t, err)
	require.NotZero(t, len(b), "no serialized data")
	msg, err = js.Deserialize(b)
	require.NoError(t, err)
	require.Equal(t, wamp.EVENT, msg.MessageType())

	e2 := msg.(*wamp.Event)
	require.Equal(t, wamp.ID(987), e2.Subscription, "JSON deserialization error: wrong subscription ID")
	require.Equal(t, wamp.ID(123), e2.Publication, "JSON deserialization error: wrong publication ID")
	require.Equal(t, 2, len(e2.Arguments), "JSON deserialization error: wrong number of arguments")
	a, _ := wamp.AsString(e2.Arguments[0])
	require.Equal(t, arg, a, "JSON deserialize error: did not get argument")
	arr, ok := e2.Arguments[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, arg2, arr)
}

func TestMessagePackBytesString(t *testing.T) {
	msgBytes := []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0x10}
	msgString := "hello world"
	call := &wamp.Call{
		Request:     123456,
		Options:     wamp.Dict{},
		Procedure:   wamp.URI("test.echo.payload"),
		Arguments:   wamp.List{msgBytes, msgString},
		ArgumentsKw: wamp.Dict{},
	}
	s := &serialize.MessagePackSerializer{}
	b, err := s.Serialize(call)
	require.NoError(t, err)
	require.NotZero(t, len(b), "no serialized data")

	msg, err := s.Deserialize(b)
	require.NoError(t, err)
	call, ok := msg.(*wamp.Call)
	require.True(t, ok, "wrong message type; expected CALL")
	require.Equal(t, 2, len(call.Arguments))

	arg1, ok := call.Arguments[0].([]byte)
	require.True(t, ok, "1st argument is not []byte")
	require.Equal(t, msgBytes, arg1, "wrong value for 1st argument")

	arg2, ok := call.Arguments[1].(string)
	require.True(t, ok, "2nd argument is not string")
	require.Equal(t, msgString, arg2, "wrong value for 2nd argument")
}

func BenchmarkSerialize(b *testing.B) {
	msgBytes := []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0x10}
	msgString := "hello world"
	call := &wamp.Call{
		Request:     123456,
		Options:     wamp.Dict{},
		Procedure:   wamp.URI("test.echo.payload"),
		Arguments:   wamp.List{msgBytes, msgString},
		ArgumentsKw: wamp.Dict{},
	}

	b.Run("JSON", func(b *testing.B) {
		benchmarkSerialize(b, &serialize.JSONSerializer{}, call)
	})

	b.Run("MSGPACK", func(b *testing.B) {
		benchmarkSerialize(b, &serialize.MessagePackSerializer{}, call)
	})

	b.Run("CBOR", func(b *testing.B) {
		benchmarkSerialize(b, &serialize.CBORSerializer{}, call)
	})
}

func benchmarkSerialize(b *testing.B, s serialize.Serializer, msg wamp.Message) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, err := s.Serialize(msg)
		if err != nil {
			panic("serialization error: " + err.Error())
		}

		_, err = s.Deserialize(data)
		if err != nil {
			panic("desrialization error: " + err.Error())
		}
	}
}
