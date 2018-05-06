package serialize

import (
	"encoding/base64"
	"errors"

	"github.com/gammazero/nexus/wamp"
	"github.com/ugorji/go/codec"
)

// JSONSerializer is an implementation of Serializer that handles
// serializing and deserializing json encoded payloads.
type JSONSerializer struct{}

// Serialize encodes a Message into a json payload.
func (s *JSONSerializer) Serialize(msg wamp.Message) ([]byte, error) {
	var b []byte
	jsh := &codec.JsonHandle{}
	return b, codec.NewEncoderBytes(&b, jsh).Encode(msgToList(msg))
}

// Deserialize decodes a json payload into a Message.
func (s *JSONSerializer) Deserialize(data []byte) (wamp.Message, error) {
	var v []interface{}
	jsh := &codec.JsonHandle{}
	err := codec.NewDecoderBytes(data, jsh).Decode(&v)
	if err != nil {
		return nil, err
	}
	if len(v) == 0 {
		return nil, errors.New("invalid message")
	}

	// json deserializer gives us an uint64 instead of an int64, whyever it
	// doesn't matter here, because valid values are only within an 8bit range.
	typ, ok := v[0].(uint64)
	if !ok {
		return nil, errors.New("unsupported message format")
	}
	return listToMsg(wamp.MessageType(typ), v)
}

// Binary data follows a convention for conversion to JSON strings.
//
// A byte array is converted to a JSON string as follows:
//
// 1. convert the byte array to a Base64 encoded (host language) string
// 2. prepend the string with a \0 character
// 3. serialize the string to a JSON string
type BinaryData []byte

func (b BinaryData) MarshalJSON() ([]byte, error) {
	s := base64.StdEncoding.EncodeToString([]byte(b))
	var out []byte
	jsh := &codec.JsonHandle{}
	return out, codec.NewEncoderBytes(&out, jsh).Encode("\x00" + s)
}

func (b *BinaryData) UnmarshalJSON(v []byte) error {
	var s string
	jsh := &codec.JsonHandle{}
	err := codec.NewDecoderBytes(v, jsh).Decode(&s)
	if err != nil {
		return nil
	}
	if s[0] != '\x00' {
		return errors.New("binary string does not start with NUL")
	}
	*b, err = base64.StdEncoding.DecodeString(s[1:])
	return err
}
