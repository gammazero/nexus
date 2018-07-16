package serialize

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/gammazero/nexus/wamp"
)

func hasRole(details wamp.Dict, role string) bool {
	_, err := wamp.DictValue(details, []string{"roles", role})
	return err == nil
}

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

func CBORDeserialize(t *testing.T) {
	s := &CBORSerializer{}

	// this is the CBOR representation of the message above
	data := []byte{
		0x83, 0x01, 0x6b, 0x6e, 0x65, 0x78, 0x75, 0x73, 0x2e, 0x72, 0x65, 0x61,
		0x6c, 0x6d, 0xa1, 0x65, 0x72, 0x6f, 0x6c, 0x65, 0x73, 0xa4, 0x6a, 0x73,
		0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x72, 0xa0, 0x66, 0x63,
		0x61, 0x6c, 0x6c, 0x65, 0x65, 0xa0, 0x66, 0x63, 0x61, 0x6c, 0x6c, 0x65,
		0x72, 0xa0, 0x69, 0x70, 0x75, 0x62, 0x6c, 0x69, 0x73, 0x68, 0x65, 0x72,
		0xa1, 0x68, 0x66, 0x65, 0x61, 0x74, 0x75, 0x72, 0x65, 0x73, 0xa1, 0x78,
		0x1d, 0x73, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x72, 0x5f,
		0x62, 0x6c, 0x61, 0x63, 0x6b, 0x77, 0x68, 0x69, 0x74, 0x65, 0x5f, 0x6c,
		0x69, 0x73, 0x74, 0x69, 0x6e, 0x67, 0xf5,
	}
	details := detailRolesFeatures()
	expect := &wamp.Hello{Realm: "nexus.realm", Details: details}

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
	encode := func(value reflect.Value) ([]byte, error) {
		return value.Bytes(), nil
	}
	decode := func(value reflect.Value, data []byte) error {
		value.Elem().SetBytes(data)
		return nil
	}
	MsgpackRegisterExtension(reflect.TypeOf(BinaryData{}), 42, encode, decode)

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
	//fmt.Printf("%v\n", bin)
	expect := []byte{147, 2, 123, 129, 165, 101, 120, 116, 114, 97, 199, 9, 42, 104, 101, 108, 108, 111, 119, 97, 109, 112}
	if !bytes.Equal(expect, bin) {
		t.Fatalf("got %v, expected %v", bin, expect)
	}

	c, err := ser.Deserialize(bin)
	if err != nil {
		t.Fatal("Error deserializing msg: ", err)
	}
	if !reflect.DeepEqual(msg, c) {
		t.Fatalf("Values are not equal: expected: %v, got: %v", msg, c)
	}
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
	if string(a) != arg {
		t.Fatal("JSON deserialize error: did not get argument, got:", e2.Arguments[0])
	}
	arr, ok := e2.Arguments[1].(map[string]interface{})
	if !ok || !reflect.DeepEqual(arr, arg2) {
		spew.Dump(e2.Arguments[1])
		t.Fatal("JSON deserialize error, expected dict, got: ", e2.Arguments[1])
	}
}

func BenchmarkJSON(b *testing.B) {
	details := detailRolesFeatures()
	hello := &wamp.Hello{Realm: "nexus.realm", Details: details}
	s := &JSONSerializer{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, err := s.Serialize(hello)
		if err != nil {
			panic("serialization error: " + err.Error())
		}

		_, err = s.Deserialize(data)
		if err != nil {
			panic("desrialization error: " + err.Error())
		}
	}
}
