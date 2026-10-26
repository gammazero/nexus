package serialize

import (
	"reflect"
	"sync"

	"github.com/gammazero/nexus/v3/wamp"
)

// SharedMessage is a message that is sent to many peers, such as an EVENT
// that the broker sends to every subscriber of a topic. Each kind of
// Serializer serializes it only once, however many peers use that serializer,
// instead of once per peer.
//
// The message must not change after it is wrapped, and the bytes that
// Serialize returns are shared and must not be changed.
type SharedMessage struct {
	wamp.Message

	mu    sync.Mutex
	bytes map[reflect.Type]*sharedBytes
}

type sharedBytes struct {
	once sync.Once
	b    []byte
	err  error
}

// NewSharedMessage wraps msg to be sent to many peers.
func NewSharedMessage(msg wamp.Message) *SharedMessage {
	return &SharedMessage{
		Message: msg,
		bytes:   map[reflect.Type]*sharedBytes{},
	}
}

// Serialize returns the message serialized by s. Serializers of the same type
// serialize the same way, so the message is serialized once for each type of
// Serializer, and the bytes are reused after that.
func (m *SharedMessage) Serialize(s Serializer) ([]byte, error) {
	t := reflect.TypeOf(s)
	m.mu.Lock()
	sb, ok := m.bytes[t]
	if !ok {
		sb = &sharedBytes{}
		m.bytes[t] = sb
	}
	m.mu.Unlock()

	sb.once.Do(func() {
		sb.b, sb.err = s.Serialize(m.Message)
	})
	return sb.b, sb.err
}

// SerializeMessage serializes msg with s, using the bytes that a SharedMessage
// has already serialized, if msg is one.
func SerializeMessage(s Serializer, msg wamp.Message) ([]byte, error) {
	if m, ok := msg.(*SharedMessage); ok {
		return m.Serialize(s)
	}
	return s.Serialize(msg)
}
