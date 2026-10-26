package serialize_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// countingSerializer is a JSONSerializer that counts the messages it
// serializes.
type countingSerializer struct {
	serialize.JSONSerializer
	count *atomic.Int32
}

func (s *countingSerializer) Serialize(msg wamp.Message) ([]byte, error) {
	s.count.Add(1)
	return s.JSONSerializer.Serialize(msg)
}

func sharedTestEvent() *wamp.Event {
	return &wamp.Event{
		Subscription: 1,
		Publication:  2,
		Details:      wamp.Dict{"topic": "nexus.test.topic"},
		Arguments:    wamp.List{"hello", 42},
	}
}

// TestSharedMessageSerializesOnce checks that a SharedMessage used by many
// peers at once, with serializers of the same type, is serialized once, and
// that every peer gets the same bytes as serializing the message itself.
func TestSharedMessageSerializesOnce(t *testing.T) {
	var count atomic.Int32
	ev := sharedTestEvent()
	shared := serialize.NewSharedMessage(ev)
	want, err := (&serialize.JSONSerializer{}).Serialize(ev)
	require.NoError(t, err)

	var wg sync.WaitGroup
	results := make([][]byte, 50)
	for i := range results {
		wg.Go(func() {
			b, err := serialize.SerializeMessage(&countingSerializer{count: &count}, shared)
			require.NoError(t, err)
			results[i] = b
		})
	}
	wg.Wait()

	require.Equal(t, int32(1), count.Load(), "serialized more than once")
	for _, b := range results {
		require.Equal(t, want, b)
	}
}

// TestSharedMessageSerializers checks that each type of Serializer serializes
// a SharedMessage its own way, and that SerializeMessage serializes a message
// that is not shared as usual.
func TestSharedMessageSerializers(t *testing.T) {
	ev := sharedTestEvent()
	shared := serialize.NewSharedMessage(ev)
	for _, s := range []serialize.Serializer{
		&serialize.JSONSerializer{},
		&serialize.MessagePackSerializer{},
		&serialize.CBORSerializer{},
	} {
		want, err := s.Serialize(ev)
		require.NoError(t, err)
		got, err := serialize.SerializeMessage(s, shared)
		require.NoError(t, err)
		require.Equal(t, want, got, "%T", s)

		got, err = serialize.SerializeMessage(s, ev)
		require.NoError(t, err)
		require.Equal(t, want, got, "%T", s)
	}
	require.Equal(t, wamp.EVENT, shared.MessageType())
}
