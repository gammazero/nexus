package serialize

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

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

func TestBinaryData(t *testing.T) {
	orig := []byte("hellowamp")

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
	err = json.Unmarshal(bin, &b)
	if err != nil {
		t.Fatal("Error unmarshalling marshalled BinaryData: ", err)
	}
	if !bytes.Equal([]byte(b), orig) {
		t.Fatalf("got %s, expected %s", string(b), string(orig))
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
