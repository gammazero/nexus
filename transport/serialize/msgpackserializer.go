package serialize

import (
	"errors"
	"reflect"

	"github.com/ugorji/go/codec"

	"github.com/gammazero/nexus/v3/wamp"
)

var mh *codec.MsgpackHandle //nolint:gochecknoglobals

func init() {
	InitMsgpackHandle()
}

// InitMsgpackHandle creates a new global MsgpackHandle.
func InitMsgpackHandle() {
	mh = new(codec.MsgpackHandle)
	mh.WriteExt = true
	mh.MapType = reflect.TypeOf(map[string]any(nil))
}

// MsgpackRegisterExtension registers a custom type for special serialization.
//
// If the MsgpackHandle is already initialized, then call InitMsgpackHandle
// before calling MsgpackRegisterExtension.
func MsgpackRegisterExtension(rt reflect.Type, tag byte, encode func(reflect.Value) ([]byte, error), decode func(reflect.Value, []byte) error) error { //nolint:lll
	if encode == nil || decode == nil {
		return mh.SetBytesExt(rt, uint64(tag), nil)
	}
	return mh.SetBytesExt(rt, uint64(tag), bytesExtWrapper{encode, decode})
}

// MessagePackSerializer is an implementation of Serializer that handles
// serializing and deserializing msgpack encoded payloads.
type MessagePackSerializer struct{}

// Serialize encodes a Message into a msgpack payload.
func (s *MessagePackSerializer) Serialize(msg wamp.Message) ([]byte, error) {
	var b []byte
	err := codec.NewEncoderBytes(&b, mh).Encode(msgToList(msg))
	return b, err
}

// Deserialize decodes a msgpack payload into a Message.
func (s *MessagePackSerializer) Deserialize(data []byte) (wamp.Message, error) {
	var v []any
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

// SerializeDataItem encodes any object/structure into a msgpack payload.
func (s *MessagePackSerializer) SerializeDataItem(item any) ([]byte, error) {
	var b []byte
	err := codec.NewEncoderBytes(&b, mh).Encode(item)
	return b, err
}

// DeserializeDataItem decodes a json payload into an object/structure.
func (s *MessagePackSerializer) DeserializeDataItem(data []byte, v any) error {
	return codec.NewDecoderBytes(data, mh).Decode(&v)
}

type bytesExtWrapper struct {
	encFn func(reflect.Value) ([]byte, error)
	decFn func(reflect.Value, []byte) error
}

func (x bytesExtWrapper) WriteExt(v any) []byte {
	bs, err := x.encFn(reflect.ValueOf(v))
	if err != nil {
		// Panic is recovered by codec package and returned as an error.
		panic(err)
	}
	return bs
}

func (x bytesExtWrapper) ReadExt(v any, bs []byte) {
	err := x.decFn(reflect.ValueOf(v), bs)
	if err != nil {
		// Panic is recovered by codec package and returned as an error.
		panic(err)
	}
}
