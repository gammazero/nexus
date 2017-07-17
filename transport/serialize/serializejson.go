package serialize

import (
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/gammazero/nexus/wamp"
)

// JSONSerializer is an implementation of Serializer that handles serializing
// and deserializing JSON encoded payloads.
type JSONSerializer struct{}

// Serialize encodes a message into a JSON payload.
func (s *JSONSerializer) Serialize(msg wamp.Message) ([]byte, error) {
	return json.Marshal(msgToList(msg))
}

// Deserialize decodes a JSON payload into a message.
func (s *JSONSerializer) Deserialize(data []byte) (wamp.Message, error) {
	var v []interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	if len(v) == 0 {
		return nil, errors.New("Invalid message")
	}

	typ, ok := v[0].(float64)
	if !ok {
		return nil, errors.New("Unsupported message format")
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
	return json.Marshal("\x00" + s)
}

func (b *BinaryData) UnmarshalJSON(v []byte) error {
	var s string
	err := json.Unmarshal(v, &s)
	if err != nil {
		return nil
	}
	if s[0] != '\x00' {
		return errors.New("Binary string does not start with NUL")
	}
	*b, err = base64.StdEncoding.DecodeString(s[1:])
	return err
}
