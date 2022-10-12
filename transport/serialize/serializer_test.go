package serialize

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/gammazero/nexus/v3/wamp"
)

var dataItem = []map[string]interface{}{{
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

func compareDeserializedSerializedDataItem(t *testing.T, item interface{}) {
	resA, ok := item.([]interface{})
	if !ok {
		t.Fatal("deserialization to array error: ")
	}
	resT, ok := resA[0].(map[string]interface{})
	if !ok {
		t.Fatal("deserialization to hash-table error: ")
	}

	arr, ok := resT["Arguments"].([]interface{})
	if !ok {
		t.Fatal("Arguments property is missed")
	}

	switch arr[0].(type) {
	case int64:
		if arr[0] != int64(1) {
			t.Fatal("Arguments[0] expected 1 value")
		}
	case uint64:
		if arr[0] != uint64(1) {
			t.Fatal("Arguments[0] expected 1 value")
		}
	case int:
		if arr[0] != 1 {
			t.Fatal("Arguments[0] expected 1 value")
		}
	default:
		t.Fatal("Arguments[0] of unexpected type")
	}

	if arr[1] != "2" {
		t.Fatal("Arguments[1] expected '2' value")
	}
	if arr[2] != true {
		t.Fatal("Arguments[2] expected true value")
	}

	mapV, ok := resT["ArgumentsKw"].(map[string]interface{})
	if !ok {
		t.Fatal("ArgumentsKw property is missed")
	}

	val, ok := mapV["prop1"]
	if !ok {
		t.Fatal("ArgumentsKw prop1 is missing")
	}

	switch val.(type) {
	case int64:
		if val != int64(1) {
			t.Fatal("ArgumentsKw prop1 expected 1 value")
		}
	case uint64:
		if val != uint64(1) {
			t.Fatal("ArgumentsKw prop1 expected 1 value")
		}
	case int:
		if val != 1 {
			t.Fatal("ArgumentsKw prop1 expected 1 value")
		}
	default:
		t.Fatal("ArgumentsKw prop1 of unexpected type")
	}

	val, ok = mapV["prop2"]
	if !ok {
		t.Fatal("ArgumentsKw prop2 is missing")
	}
	if val != string('2') {
		t.Fatal("expected '2' value")
	}

	val, ok = mapV["prop3"]
	if !ok {
		t.Fatal("ArgumentsKw prop1 is missing")
	}
	if val != true {
		t.Fatal("expected true value")
	}
}

func TestJSONSerialize(t *testing.T) {
	details := detailRolesFeatures()
	hello := &wamp.Hello{Realm: "nexus.realm", Details: details}

	s := &JSONSerializer{}
	b, err := s.Serialize(hello)
	if err != nil {
		t.Fatal("Serialization error: ", err)
	}
	if len(b) == 0 {
		t.Fatal("no serialized data")
	}

	msg, err := s.Deserialize(b)
	if err != nil {
		t.Fatal("desrialization error: ", err)
	}
	if msg.MessageType() != wamp.HELLO {
		t.Fatal("desrialization to wrong message type: ", msg.MessageType())
	}
	if !hasFeature(hello.Details, "publisher", "subscriber_blackwhite_listing") {
		t.Fatal("did not deserialize message details")
	}

	val, ok := hello.Details["nothere"]
	if !ok {
		t.Fatal("nil value item 'nothere' is missing")
	}
	if val != nil {
		t.Fatal("expected nil value item 'nothere'")
	}
}

func TestJSONDeserialize(t *testing.T) {
	s := &JSONSerializer{}

	data := `[1,"nexus.realm",{}]`
	expect := &wamp.Hello{Realm: "nexus.realm", Details: wamp.Dict{}}
	msg, err := s.Deserialize([]byte(data))
	if err != nil {
		t.Fatalf("Error decoding good data: %s, %s", err, data)
	}
	if msg.MessageType() != expect.MessageType() {
		t.Fatalf("Incorrect message type: have %s, want %s", msg.MessageType(),
			expect.MessageType())
	}
	if !reflect.DeepEqual(msg, expect) {
		t.Fatalf("got %+v, expected %+v", msg, expect)
	}

	emptyData := `[]`
	_, err = s.Deserialize([]byte(emptyData))
	if err == nil {
		t.Fatal("Empty message should be errored while decoding")
	}
}

func TestJSONSerializeDataItem(t *testing.T) {
	s := &JSONSerializer{}
	b, err := s.SerializeDataItem(dataItem)
	if err != nil {
		t.Fatal("Serialization error: ", err)
	}
	if len(b) == 0 {
		t.Fatal("no serialized data")
	}

	var res interface{}
	if err := s.DeserializeDataItem(b, &res); err != nil {
		t.Fatal("desrialization error: ", err)
	}

	compareDeserializedSerializedDataItem(t, res)
}

func TestJSONDeserializeDataItem(t *testing.T) {
	s := &JSONSerializer{}

	data := `[{"Arguments":[1,"2",true],"ArgumentsKw":{"prop1":1,"prop2":"2","prop3":true}}]`
	var msg interface{}
	if err := s.DeserializeDataItem([]byte(data), &msg); err != nil {
		t.Fatalf("Error decoding good data: %s, %s", err, data)
	}
	compareDeserializedSerializedDataItem(t, msg)

	var val []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}
	if err := s.DeserializeDataItem([]byte(data), &val); err != nil {
		t.Fatalf("Error decoding good data: %s, %s", err, data)
	}
	expected := []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}{
		{
			Arguments: []interface{}{
				uint64(1), "2", true,
			},
			ArgumentsKw: map[string]interface{}{
				"prop1": uint64(1), "prop2": "2", "prop3": true,
			},
		},
	}
	eq := reflect.DeepEqual(val, expected)
	if !eq {
		t.Fatalf("should be true, expected:\n%v\ngot:\n%v",
			spew.Sdump(expected), spew.Sdump(val))
	}
}

func TestCBORSerialize(t *testing.T) {
	details := detailRolesFeatures()
	hello := &wamp.Hello{Realm: "nexus.realm", Details: details}

	s := &CBORSerializer{}
	b, err := s.Serialize(hello)
	if err != nil {
		t.Fatal("Serialization error: ", err)
	}
	if len(b) == 0 {
		t.Fatal("no serialized data")
	}

	msg, err := s.Deserialize(b)
	if err != nil {
		t.Fatal("desrialization error: ", err)
	}
	if msg.MessageType() != wamp.HELLO {
		t.Fatal("desrialization to wrong message type: ", msg.MessageType())
	}
	if !hasFeature(hello.Details, "publisher", "subscriber_blackwhite_listing") {
		t.Fatal("did not deserialize message details")
	}

	val, ok := hello.Details["nothere"]
	if !ok {
		t.Fatal("nil value item 'nothere' is missing")
	}
	if val != nil {
		t.Fatal("expected nil value item 'nothere'")
	}
}

func TestCBORDeserialize(t *testing.T) {
	s := &CBORSerializer{}

	// this is the CBOR representation of the message above
	data := []byte{
		0x83, 0x01, 0x6b, 0x6e, 0x65, 0x78, 0x75, 0x73, 0x2e, 0x72, 0x65, 0x61,
		0x6c, 0x6d, 0xa0,
	}
	expect := &wamp.Hello{Realm: "nexus.realm", Details: wamp.Dict{}}

	msg, err := s.Deserialize(data)
	if err != nil {
		t.Fatalf("Error decoding good data: %s, %x", err, data)
	}
	if msg.MessageType() != expect.MessageType() {
		t.Fatalf("Incorrect message type: have %s, want %s", msg.MessageType(),
			expect.MessageType())
	}
	if !reflect.DeepEqual(msg, expect) {
		t.Fatalf("got %+v, expected %+v", msg, expect)
	}

	emptyData := []byte{0x80}
	_, err = s.Deserialize(emptyData)
	if err == nil {
		t.Fatal("Empty message should be errored while decoding")
	}
}

func TestCBORSerializeDataItem(t *testing.T) {
	s := &CBORSerializer{}
	b, err := s.SerializeDataItem(dataItem)
	if err != nil {
		t.Fatal("Serialization error: ", err)
	}
	if len(b) == 0 {
		t.Fatal("no serialized data")
	}

	var res interface{}
	if err := s.DeserializeDataItem(b, &res); err != nil {
		t.Fatal("desrialization error: ", err)
	}
	compareDeserializedSerializedDataItem(t, res)
}

func TestCBORDeserializeDataItem(t *testing.T) {
	s := &CBORSerializer{}

	// this is the CBOR representation of the message above
	data := []byte{
		0x81, 0xa2, 0x69, 0x41, 0x72, 0x67, 0x75, 0x6D, 0x65, 0x6E, 0x74,
		0x73, 0x83, 0x01, 0x61, 0x32, 0xf5, 0x6b, 0x41, 0x72, 0x67, 0x75,
		0x6D, 0x65, 0x6E, 0x74, 0x73, 0x4B, 0x77, 0xa3, 0x65, 0x70, 0x72,
		0x6F, 0x70, 0x31, 0x01, 0x65, 0x70, 0x72, 0x6F, 0x70, 0x32, 0x61,
		0x32, 0x65, 0x70, 0x72, 0x6F, 0x70, 0x33, 0xf5,
	}
	var msg interface{}
	if err := s.DeserializeDataItem(data, &msg); err != nil {
		t.Fatalf("Error decoding good data: %s, %x", err, data)
	}
	compareDeserializedSerializedDataItem(t, msg)

	var val []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}
	if err := s.DeserializeDataItem(data, &val); err != nil {
		t.Fatalf("Error decoding good data: %s, %s", err, data)
	}
	expected := []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}{
		{
			Arguments: []interface{}{
				uint64(1), "2", true,
			},
			ArgumentsKw: map[string]interface{}{
				"prop1": uint64(1), "prop2": "2", "prop3": true,
			},
		},
	}
	eq := reflect.DeepEqual(val, expected)
	if !eq {
		t.Fatalf("should be true, expected:\n%v\ngot:\n%v",
			spew.Sdump(expected), spew.Sdump(val))
	}
}

func TestMessagePackSerialize(t *testing.T) {
	hello := &wamp.Hello{Realm: "nexus.realm", Details: detailRolesFeatures()}

	s := &MessagePackSerializer{}
	b, err := s.Serialize(hello)
	if err != nil {
		t.Fatal("Serialization error: ", err)
	}
	if len(b) == 0 {
		t.Fatal("no serialized data")
	}
	msg, err := s.Deserialize(b)
	if err != nil {
		t.Fatal("desrialization error: ", err)
	}
	if msg.MessageType() != wamp.HELLO {
		t.Fatal("desrialization to wrong message type: ", msg.MessageType())
	}
	if !hasFeature(hello.Details, "publisher", "subscriber_blackwhite_listing") {
		t.Fatal("did not deserialize message details")
	}

	val, ok := hello.Details["nothere"]
	if !ok {
		t.Fatal("nil value item 'nothere' is missing")
	}
	if val != nil {
		t.Fatal("expected nil value item 'nothere'")
	}
}

func TestMessagePackDeserialize(t *testing.T) {
	s := &MessagePackSerializer{}

	data := []byte{0x93, 0x01, 0xab, 0x6e, 0x65, 0x78, 0x75, 0x73, 0x2e, 0x72, 0x65, 0x61, 0x6c, 0x6d, 0x80}
	expect := &wamp.Hello{Realm: "nexus.realm", Details: wamp.Dict{}}
	msg, err := s.Deserialize(data)
	if err != nil {
		t.Fatalf("Error decoding good data: %s, %x", err, data)
	}
	if msg.MessageType() != expect.MessageType() {
		t.Fatalf("Incorrect message type: have %s, want %s", msg.MessageType(),
			expect.MessageType())
	}
	if !reflect.DeepEqual(msg, expect) {
		t.Fatalf("got %+v, expected %+v", msg, expect)
	}

	emptyData := []byte{0x90}
	_, err = s.Deserialize(emptyData)
	if err == nil {
		t.Fatal("Empty message should be errored while decoding")
	}

}

func TestMessagePackSerializeDataItem(t *testing.T) {
	s := &MessagePackSerializer{}
	b, err := s.SerializeDataItem(dataItem)
	if err != nil {
		t.Fatal("Serialization error: ", err)
	}
	if len(b) == 0 {
		t.Fatal("no serialized data")
	}
	var res interface{}
	if err := s.DeserializeDataItem(b, &res); err != nil {
		t.Fatal("desrialization error: ", err)
	}
	compareDeserializedSerializedDataItem(t, res)
}

func TestMessagePackDeserializeDataItem(t *testing.T) {
	s := &MessagePackSerializer{}

	data := []byte{
		0x91, 0x82, 0xA9, 0x41, 0x72, 0x67, 0x75, 0x6D, 0x65, 0x6E,
		0x74, 0x73, 0x93, 0x01, 0xA1, 0x32, 0xC3, 0xAB, 0x41, 0x72,
		0x67, 0x75, 0x6D, 0x65, 0x6E, 0x74, 0x73, 0x4B, 0x77, 0x83,
		0xA5, 0x70, 0x72, 0x6F, 0x70, 0x31, 0x01, 0xA5, 0x70, 0x72,
		0x6F, 0x70, 0x32, 0xA1, 0x32, 0xA5, 0x70, 0x72, 0x6F, 0x70,
		0x33, 0xC3,
	}
	var msg interface{}
	if err := s.DeserializeDataItem(data, &msg); err != nil {
		t.Fatalf("Error decoding good data: %s, %x", err, data)
	}
	compareDeserializedSerializedDataItem(t, msg)

	var val []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}
	if err := s.DeserializeDataItem(data, &val); err != nil {
		t.Fatalf("Error decoding good data: %s, %s", err, data)
	}
	expected := []struct {
		Arguments   wamp.List
		ArgumentsKw wamp.Dict
	}{
		{
			Arguments: []interface{}{
				int64(1), "2", true,
			},
			ArgumentsKw: map[string]interface{}{
				"prop1": int64(1), "prop2": "2", "prop3": true,
			},
		},
	}
	eq := reflect.DeepEqual(val, expected)
	if !eq {
		t.Fatalf("should be true, expected:\n%v\ngot:\n%v",
			spew.Sdump(expected), spew.Sdump(val))
	}
}

func TestBinaryDataJSON(t *testing.T) {
	orig := []byte("hellowamp")

	// Calls the customer encoder: BinaryData.MarshalJSON()
	bin, err := json.Marshal(BinaryData(orig))
	if err != nil {
		t.Fatal("Error marshalling BinaryData: ", err)
	}

	expect := fmt.Sprintf(`"\u0000%s"`,
		base64.StdEncoding.EncodeToString(orig))
	if !bytes.Equal([]byte(expect), bin) {
		t.Fatalf("got %s, expected %s", string(bin), expect)
	}

	var b BinaryData
	// Calls the customer decoder: BinaryData.UnmarshalJSON()
	err = json.Unmarshal(bin, &b)
	if err != nil {
		t.Fatal("Error unmarshalling marshalled BinaryData: ", err)
	}
	if !bytes.Equal([]byte(b), orig) {
		t.Fatalf("got %s, expected %s", string(b), string(orig))
	}
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

	err := MsgpackRegisterExtension(reflect.TypeOf(BinaryData{}), 42, encode, decode)
	if err != nil {
		t.Fatalf("Error registering msgpack extension: %s", err)
	}

	orig := []byte("hellowamp")
	msg := &wamp.Welcome{
		ID: wamp.ID(123),
		Details: wamp.Dict{
			"extra": BinaryData(orig),
		},
	}

	ser := MessagePackSerializer{}
	// Calls the customer encoder: BinaryData.MarshalJSON()
	bin, err := ser.Serialize(msg)
	if err != nil {
		t.Fatal("Error marshalling msg: ", err)
	}

	c, err := ser.Deserialize(bin)
	if err != nil {
		t.Fatal("Error deserializing msg: ", err)
	}
	m1, ok := c.(*wamp.Welcome)
	if !ok {
		t.Fatal("could not convert ot wamp.Welcome")
	}
	if msg.ID != m1.ID {
		t.Fatal("IDs not equal")
	}
	if len(msg.Details) != len(m1.Details) {
		t.Fatal("message details have different sizes")
	}
	v1, ok := msg.Details["extra"]
	if !ok {
		t.Fatal("msg.Details[extra] missing")
	}
	v2, ok := m1.Details["extra"]
	if !ok {
		t.Fatal("m1.Details[extra] missing")
	}
	vs1 := string(v1.(BinaryData))
	vs2, _ := wamp.AsString(v2)
	if vs1 != vs2 {
		t.Fatalf("details extras not equal: %q != %q", vs1, vs2)
	}

	// Does not work as of commit this commit:
	// github.com/ugorji/go/codec@@20768e92ac5d44754d3ae811382dea19ec3901c
	//
	//if !reflect.DeepEqual(msg, m1) {
	//	t.Fatalf("Values are not equal: expected: %v, got: %v", msg, m1)
	//}
}

func TestAssignSlice(t *testing.T) {
	const msgType = wamp.PUBLISH

	pubArgs := []string{"hello", "nexus", "wamp", "router"}

	// Deserializing a slice into a message.
	elems := wamp.List{msgType, 123, wamp.Dict{},
		"some.valid.topic", pubArgs}
	msg, err := listToMsg(msgType, elems)
	if err != nil {
		t.Fatal(err)
	}

	// Check that message is a Publish message.
	pubMsg, ok := msg.(*wamp.Publish)
	if !ok {
		t.Fatal("got incorrect message type:", msg.MessageType())
	}

	// Check arguments.
	if len(pubMsg.Arguments) != len(pubArgs) {
		t.Fatal("wrong number of message arguments")
	}
	for i := 0; i < len(pubArgs); i++ {
		if pubMsg.Arguments[i] != pubArgs[i] {
			t.Fatalf("argument %d has wrong value", i)
		}
	}
}

func TestMsgpackDeserializeFail(t *testing.T) {
	ser := MessagePackSerializer{}
	res, err := ser.Deserialize(nil)
	if err == nil {
		t.Fatalf("Expected error, got result: %v", res)
	}
	res, err = ser.Deserialize([]byte{144}) // pass in a serialized empty array
	if err == nil {
		t.Fatalf("Expected error, got result: %v", res)
	}
	res, err = ser.Deserialize([]byte{145, 161, 102}) // array containing string
	if err == nil {
		t.Fatalf("Expected error, got result: %v", res)
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
	if err != nil {
		t.Error(err.Error())
	}

	err = testMsgToList(wamp.List{}, make(wamp.Dict), 2,
		"empty args, empty kwArgs")
	if err != nil {
		t.Error(err.Error())
	}

	err = testMsgToList(wamp.List{1}, nil, 1, "non-empty args, nil kwArgs")
	if err != nil {
		t.Error(err.Error())
	}

	err = testMsgToList(nil, wamp.Dict{"a": nil}, 0,
		"nil args, non-empty kwArgs")
	if err != nil {
		t.Error(err.Error())
	}

	err = testMsgToList(wamp.List{1}, make(wamp.Dict), 1,
		"non-empty args, empty kwArgs")
	if err != nil {
		t.Error(err.Error())
	}

	err = testMsgToList(wamp.List{}, wamp.Dict{"a": nil}, 0,
		"empty args, non-empty kwArgs")
	if err != nil {
		t.Error(err.Error())
	}

	err = testMsgToList(wamp.List{1}, wamp.Dict{"a": nil}, 0,
		"test message one")
	if err != nil {
		t.Error(err.Error())
	}
}

func TestMsgPackToJSON(t *testing.T) {
	arg := "this is a test"
	arg2 := map[string]interface{}{
		"hello": "world",
	}
	pub := &wamp.Publish{
		Request:   123,
		Topic:     "msgpack.to.json",
		Arguments: wamp.List{arg, arg2},
	}
	ms := &MessagePackSerializer{}
	b, err := ms.Serialize(pub)
	if err != nil {
		t.Fatal("Serialization error: ", err)
	}
	if len(b) == 0 {
		t.Fatal("no serialized data")
	}
	msg, err := ms.Deserialize(b)
	if err != nil {
		t.Fatal("desrialization error: ", err)
	}
	p2 := msg.(*wamp.Publish)
	event := &wamp.Event{
		Subscription: 987,
		Publication:  p2.Request,
		Details:      wamp.Dict{"hello": "world"},
		Arguments:    p2.Arguments,
	}

	js := &JSONSerializer{}
	b, err = js.Serialize(event)
	if err != nil {
		t.Fatal("JSON serialization error: ", err)
	}
	if len(b) == 0 {
		t.Fatal("no serialized data")
	}
	msg, err = js.Deserialize(b)
	if err != nil {
		t.Fatal("JSON desrialization error: ", err)
	}
	if msg.MessageType() != wamp.EVENT {
		t.Fatal("JSON desrialization to wrong message type: ", msg.MessageType())
	}
	e2 := msg.(*wamp.Event)
	//fmt.Printf("Before:\n%+v\n", event)
	//fmt.Printf("After:\n%+v\n", e2)
	if e2.Subscription != wamp.ID(987) {
		t.Fatal("JSON deserialization error: wrong subscription ID")
	}
	if e2.Publication != wamp.ID(123) {
		t.Fatal("JSON deserialization error: wrong publication ID")
	}
	if len(e2.Arguments) != 2 {
		t.Fatal("JSON deserialization error: wrong number of arguments")
	}
	a, _ := wamp.AsString(e2.Arguments[0])
	if a != arg {
		t.Fatal("JSON deserialize error: did not get argument, got:", e2.Arguments[0])
	}
	arr, ok := e2.Arguments[1].(map[string]interface{})
	if !ok || !reflect.DeepEqual(arr, arg2) {
		spew.Dump(e2.Arguments[1])
		t.Fatal("JSON deserialize error, expected dict, got: ", e2.Arguments[1])
	}
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
	s := &MessagePackSerializer{}
	b, err := s.Serialize(call)
	if err != nil {
		t.Fatal("Serialization error: ", err)
	}
	if len(b) == 0 {
		t.Fatal("no serialized data")
	}

	msg, err := s.Deserialize(b)
	if err != nil {
		t.Fatal("desrialization error: ", err)
	}
	call, ok := msg.(*wamp.Call)
	if !ok {
		t.Fatal("wrong message type; expected CALL, got", msg.MessageType())
	}
	if len(call.Arguments) != 2 {
		t.Fatal("expected 1 argument, got", len(call.Arguments))
	}

	arg1, ok := call.Arguments[0].([]byte)
	if !ok {
		t.Error("1st argument is not []byte")
	} else if !bytes.Equal(arg1, msgBytes) {
		t.Error("wrong value for 1st argument")
	}

	arg2, ok := call.Arguments[1].(string)
	if !ok {
		t.Error("2nd argument is not string")
	} else if arg2 != msgString {
		t.Error("wrong value for 2nd argument")
	}
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
		benchmarkSerialize(b, &JSONSerializer{}, call)
	})

	b.Run("MSGPACK", func(b *testing.B) {
		benchmarkSerialize(b, &MessagePackSerializer{}, call)
	})

	b.Run("CBOR", func(b *testing.B) {
		benchmarkSerialize(b, &CBORSerializer{}, call)
	})
}

func benchmarkSerialize(b *testing.B, s Serializer, msg wamp.Message) {
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
