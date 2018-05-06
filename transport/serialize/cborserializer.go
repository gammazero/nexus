package serialize

import (
	"errors"

	"github.com/gammazero/nexus/wamp"
	"github.com/ugorji/go/codec"
)

// CBORSerializer is an implementation of Serializer that handles
// serializing and deserializing cbor encoded payloads.
type CBORSerializer struct{}

// Serialize encodes a Message into a cbor payload.
func (s *CBORSerializer) Serialize(msg wamp.Message) ([]byte, error) {
	var b []byte
	cbh := &codec.CborHandle{}
	return b, codec.NewEncoderBytes(&b, cbh).Encode(msgToList(msg))
}

// Deserialize decodes a cbor payload into a Message.
func (s *CBORSerializer) Deserialize(data []byte) (wamp.Message, error) {
	var v []interface{}
	cbh := &codec.CborHandle{}
	err := codec.NewDecoderBytes(data, cbh).Decode(&v)
	if err != nil {
		return nil, err
	}
	if len(v) == 0 {
		return nil, errors.New("invalid message")
	}

	// cbor deserializer gives us an uint64 instead of an int64, whyever it
	// doesn't matter here, because valid values are only within an 8bit range.
	typ, ok := v[0].(uint64)
	if !ok {
		return nil, errors.New("unsupported message format")
	}
	return listToMsg(wamp.MessageType(typ), v)
}
