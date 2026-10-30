package serialize

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

func TestAssignMap(t *testing.T) {
	// Keys of type any are converted to the string keys of a Dict, and values are
	// kept, including nil.
	var dict wamp.Dict
	src := map[any]any{
		"a": 1,
		"b": "two",
		"c": nil,
		"d": []any{1, 2},
		"e": map[any]any{"f": 3},
	}
	err := assignMap(reflect.ValueOf(&dict).Elem(), reflect.ValueOf(src))
	require.NoError(t, err)
	require.Equal(t, wamp.Dict{
		"a": 1,
		"b": "two",
		"c": nil,
		"d": []any{1, 2},
		"e": map[any]any{"f": 3},
	}, dict)

	// Values of another type are converted to the values of a Dict.
	err = assignMap(reflect.ValueOf(&dict).Elem(),
		reflect.ValueOf(map[string]int{"x": 1, "y": 2}))
	require.NoError(t, err)
	require.Equal(t, wamp.Dict{"x": 1, "y": 2}, dict)

	// The destination is replaced, not merged into.
	dict = wamp.Dict{"old": true}
	err = assignMap(reflect.ValueOf(&dict).Elem(),
		reflect.ValueOf(map[any]any{"new": true}))
	require.NoError(t, err)
	require.Equal(t, wamp.Dict{"new": true}, dict)

	// An empty source gives an empty, non-nil map.
	dict = nil
	err = assignMap(reflect.ValueOf(&dict).Elem(),
		reflect.ValueOf(map[any]any{}))
	require.NoError(t, err)
	require.NotNil(t, dict)
	require.Empty(t, dict)
}

func TestAssignMapError(t *testing.T) {
	// A key that cannot be converted to the destination key type is an error.
	var dict wamp.Dict
	err := assignMap(reflect.ValueOf(&dict).Elem(),
		reflect.ValueOf(map[any]any{1.5: "x"}))
	require.EqualError(t, err, "cannot convert src key '1.5', invalid type: "+
		"type float64 not convertible to string")

	// A value that cannot be converted to the destination value type is an error.
	var ints map[string]int
	err = assignMap(reflect.ValueOf(&ints).Elem(),
		reflect.ValueOf(map[string]string{"a": "x"}))
	require.EqualError(t, err, "cannot convert src value for key 'a', "+
		"invalid type: type string not convertible to int")
}

// TestListToMsgAssignMap checks that listToMsg uses assignMap to set a dict
// field of a message from a map that is not a Dict.
func TestListToMsgAssignMap(t *testing.T) {
	msg, err := listToMsg(wamp.HELLO, []any{
		int64(wamp.HELLO),
		"realm1",
		map[any]any{"agent": "test", "roles": map[any]any{"caller": true}},
	})
	require.NoError(t, err)
	hello, ok := msg.(*wamp.Hello)
	require.True(t, ok, "expected HELLO")
	require.Equal(t, wamp.URI("realm1"), hello.Realm)
	require.Equal(t, wamp.Dict{
		"agent": "test",
		"roles": map[any]any{"caller": true},
	}, hello.Details)

	// A map that cannot be assigned is an error.
	_, err = listToMsg(wamp.HELLO, []any{
		int64(wamp.HELLO), "realm1", map[any]any{1.5: "x"},
	})
	require.Error(t, err)
}

// TestAssignMapConvertedKeys checks that assignMap gets each value with the
// key from src, not with the key converted for dst, which src may not have.
func TestAssignMapConvertedKeys(t *testing.T) {
	var dict wamp.Dict
	err := assignMap(reflect.ValueOf(&dict).Elem(),
		reflect.ValueOf(map[wamp.URI]any{"a.uri": 1}))
	require.NoError(t, err)
	require.Equal(t, wamp.Dict{"a.uri": 1}, dict)

	err = assignMap(reflect.ValueOf(&dict).Elem(),
		reflect.ValueOf(map[any]any{wamp.URI("a.uri"): 1, "b": 2}))
	require.NoError(t, err)
	require.Equal(t, wamp.Dict{"a.uri": 1, "b": 2}, dict)
}

// TestAssignMapIntegerKeys checks that an integer is not converted to a string
// as Go does, by taking it as a rune, which gives a different key, or a key
// that collides with another.
func TestAssignMapIntegerKeys(t *testing.T) {
	var dict wamp.Dict
	err := assignMap(reflect.ValueOf(&dict).Elem(),
		reflect.ValueOf(map[any]any{65: "x"}))
	require.EqualError(t, err, "cannot convert src key '65', invalid type: "+
		"type int not convertible to string")

	err = assignMap(reflect.ValueOf(&dict).Elem(),
		reflect.ValueOf(map[uint8]any{65: "x"}))
	require.EqualError(t, err, "cannot convert src key '65', invalid type: "+
		"type uint8 not convertible to string")

	var strs map[string]string
	err = assignMap(reflect.ValueOf(&strs).Elem(),
		reflect.ValueOf(map[string]int{"a": 65}))
	require.EqualError(t, err, "cannot convert src value for key 'a', "+
		"invalid type: type int not convertible to string")

	var strList []string
	err = assignSlice(reflect.ValueOf(&strList).Elem(),
		reflect.ValueOf([]int{65}))
	require.EqualError(t, err, "cannot convert value at index 0: "+
		"type int not convertible to string")
}
