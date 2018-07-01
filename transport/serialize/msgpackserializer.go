package serialize

import (
	"errors"
	"reflect"

	"github.com/gammazero/nexus/wamp"
	"github.com/ugorji/go/codec"
)

func init() {
	mh = &codec.MsgpackHandle{
		RawToString: true,
	}
	mh.MapType = reflect.TypeOf(map[string]interface{}(nil))
}

var mh *codec.MsgpackHandle

// MessagePackSerializer is an implementation of Serializer that handles
// serializing and deserializing msgpack encoded payloads.
type MessagePackSerializer struct{}

// Serialize encodes a Message into a msgpack payload.
func (s *MessagePackSerializer) Serialize(msg wamp.Message) ([]byte, error) {
	var b []byte
	return b, codec.NewEncoderBytes(&b, mh).Encode(
		msgToList(msg))
}

// Deserialize decodes a msgpack payload into a Message.
func (s *MessagePackSerializer) Deserialize(data []byte) (wamp.Message, error) {
	var v []interface{}
	err := codec.NewDecoderBytes(data, mh).Decode(&v)
	if err != nil {
		return nil, err
	}
	if len(v) == 0 {
		return nil, errors.New("invalid message")
	}

	typ, ok := v[0].(int64)
	if !ok {
		return nil, errors.New("unsupported message format")
	}
	return listToMsg(wamp.MessageType(typ), v)
}
