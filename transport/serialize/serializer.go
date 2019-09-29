/*
Package serialize provides a Serializer interface with implementations that
encode and decode message data in various ways.

*/
package serialize

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/gammazero/nexus/v3/wamp"
)

const (
	// Use JSON-encoded strings as a payload.
	JSON Serialization = iota
	// Use msgpack-encoded strings as a payload.
	MSGPACK
	// Use CBOR encoding as a payload
	CBOR
)

// Serialization indicates the data serialization format used in a WAMP session
type Serialization int

// Serializer is the interface implemented by an object that can serialize and
// deserialize WAMP messages
type Serializer interface {
	Serialize(wamp.Message) ([]byte, error)
	Deserialize([]byte) (wamp.Message, error)
}

// listToMessage takes a list of values from a WAMP message and populates the
// fields of a message type.
func listToMsg(msgType wamp.MessageType, vlist []interface{}) (wamp.Message, error) {
	msg := wamp.NewMessage(msgType)
	if msg == nil {
		return nil, errors.New("unsupported message type")
	}
	val := reflect.ValueOf(msg)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	// Iterate each field of the target message and populate the field with
	// corresponding value from the WAMP message.
	for i := 0; i < val.NumField() && i < len(vlist)-1; i++ {
		f := val.Field(i)
		if vlist[i+1] == nil {
			continue
		}
		// Get the value of the item in list.
		arg := reflect.ValueOf(vlist[i+1])
		if arg.Kind() == reflect.Ptr {
			arg = arg.Elem()
		}
		// Try to assign the item to the message field.
		if arg.Type().AssignableTo(f.Type()) {
			f.Set(arg)
			continue
		}
		// Cannot directly assign, so try to convert and assign.
		if arg.Type().ConvertibleTo(f.Type()) {
			f.Set(arg.Convert(f.Type()))
			continue
		}
		// See if the list item and message field are same kind, error if not.
		if arg.Type().Kind() != f.Type().Kind() {
			return nil, fmt.Errorf("field %d not recognized, has %s, want %s",
				i+1, arg.Type(), f.Type())
		}
		// If field and list item type is map, then assign item to msg field.
		if f.Type().Kind() == reflect.Map {
			if err := assignMap(f, arg); err != nil {
				return nil, err
			}
			continue
		}
		// If field and list item type is slice, then assign item to msg field.
		if f.Type().Kind() == reflect.Slice {
			if err := assignSlice(f, arg); err != nil {
				return nil, err
			}
			continue
		}
		// Should never happen since this means that our own message type has
		// a type that is not a map or a slice.  This is a programming error,
		// so panic.
		panic(fmt.Sprintf("internal message field %d not recognized", i+1))
	}
	return msg, nil
}

// convertType converts a value to the specified type if necessary/possible.
// No-op if not necessary, error if not possible.
func convertType(val reflect.Value, typ reflect.Type) (reflect.Value, error) {
	valType := val.Type()
	// See if value is directly assignable.
	if !valType.AssignableTo(typ) {
		// Not directly assignable, so see if it is convertible.
		if !valType.ConvertibleTo(typ) {
			return val, fmt.Errorf("type %s not convertible to %s",
				valType.Kind(), typ.Kind())
		}
		// Convertible, so convert value.
		return val.Convert(typ), nil
	}
	return val, nil
}

// assignMap takes the key-value pairs from src and copies them into dst.
// Types are converted as needed.
func assignMap(dst reflect.Value, src reflect.Value) error {
	dstKeyType := dst.Type().Key()
	dstValType := dst.Type().Elem()

	dst.Set(reflect.MakeMap(dst.Type()))
	for _, k := range src.MapKeys() {
		if k.Type().Kind() == reflect.Interface {
			k = k.Elem()
		}
		var err error
		if k, err = convertType(k, dstKeyType); err != nil {
			return fmt.Errorf("cannot convert src key '%v', invalid type: %s",
				k.Interface(), err)
		}
		v := src.MapIndex(k)
		if v, err = convertType(v, dstValType); err != nil {
			return fmt.Errorf(
				"cannot convert src value for key '%v', invalid type: %s",
				k.Interface(), err)
		}
		dst.SetMapIndex(k, v)
	}
	return nil
}

// assignSlice takes the values from src and copies them into dst.  Types are
// converted as needed.
func assignSlice(dst reflect.Value, src reflect.Value) error {
	dst.Set(reflect.MakeSlice(dst.Type(), src.Len(), src.Len()))
	dstElemType := dst.Type().Elem()
	for i := 0; i < src.Len(); i++ {
		v, err := convertType(src.Index(i), dstElemType)
		if err != nil {
			return fmt.Errorf("cannot convert value at index %d: %s", i, err)
		}
		dst.Index(i).Set(v)
	}
	return nil
}

// msgToList converts a message to a list of interface{}. Trailing empty values
// are not appended to the list.
func msgToList(msg wamp.Message) []interface{} {
	val := reflect.ValueOf(msg)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	// Skip all empty fields at the end of the message structure by iterating
	// backwards until a non-empty or non-"omitempty" field is found,
	last := val.Type().NumField() - 1
	for ; last > 0; last-- {
		tag := val.Type().Field(last).Tag.Get("wamp")
		if !strings.Contains(tag, "omitempty") || val.Field(last).Len() > 0 {
			break
		}
	}

	// Encode the remaining message elements.
	ret := make([]interface{}, last+2)
	ret[0] = int(msg.MessageType())
	for i := 0; i <= last; i++ {
		ret[i+1] = val.Field(i).Interface()
	}
	return ret
}
