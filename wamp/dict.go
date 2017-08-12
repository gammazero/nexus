package wamp

import (
	"errors"
	"reflect"
	"strings"
)

// NormalizeDict takes a dict and creates a new normalized dict where all
// map[string]xxx are converted to Dict.  Values that cannot
// be converted, or are already the correct map type, remain the same.
//
// This is used for initial conversion of hello details.  The original dict is
// not mutated.
func NormalizeDict(v interface{}) Dict {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Map {
		return nil
	}
	dict := Dict{}
	for _, key := range val.MapKeys() {
		if key.Kind() == reflect.Interface {
			key = key.Elem()
		}
		if key.Kind() != reflect.String {
			continue
		}
		cv := val.MapIndex(key)
		newVal := NormalizeDict(cv.Interface())
		if newVal == nil {
			// If the value is interface{} representing []interface{}, then
			// convert the slice to a List type.
			if cv.Kind() == reflect.Interface && cv.Elem().Kind() == reflect.Slice {
				cv = cv.Elem()
				listType := reflect.TypeOf(List{})
				if cv.Type().ConvertibleTo(listType) {
					cv = cv.Convert(listType)
				}
			}
			dict[key.String()] = cv.Interface()
			continue
		}
		dict[key.String()] = newVal
	}
	return dict
}

// Return the child dictionary for the given key, or nil if not present.
//
// If the child is not a Dict, an attempt is made to convert
// it.  The dict is not modified since features may be looked up cuncurrently
// for the same session.
func DictChild(dict Dict, key string) Dict {
	iface, ok := dict[key]
	if !ok || iface == nil {
		// Map does not have the specified key or value is nil.
		return nil
	}
	child, ok := iface.(Dict)
	if !ok {
		// value is not in expected form; try to convert
		// Session details are normalized whensession is attached, so this
		// should not be necessary normally.
		child = NormalizeDict(iface)
		if child == nil {
			// could not convert
			return nil
		}
	}
	return child
}

// DictValue returns the value specified by the slice of path elements.
//
// To specify the path using a dot-separated string, call like this:
//     DictValue(dict, strings.Split(path, "."))
//
// For example, the path []string{"roles","callee","features","call_timeout"}
// returns  the value of the call_timeout feature as interface{}.  An error
// is returned if the value is not present.
func DictValue(dict Dict, path []string) (interface{}, error) {
	for i := range path[:len(path)-1] {
		dict = DictChild(dict, path[i])
		if dict == nil {
			return nil, errors.New(
				"cannot find: " + strings.Join(path[:i+1], "."))
		}
	}
	v, ok := dict[path[len(path)-1]]
	if !ok {
		return nil, errors.New("cannot find: " + strings.Join(path, "."))
	}
	return v, nil
}

// DictFlag returns the bool specified by the dot-separated path.
//
// To specify the path using a dot-separated string, call like this:
//     DictFlag(dict, strings.Split(path, "."))
//
// For example: "roles.subscriber.features.publisher_identification" returns
// the value of the publisher_identification feature.  An error is returned if
// the value is not present or is not a boolean type.
func DictFlag(dict Dict, path []string) (bool, error) {
	v, err := DictValue(dict, path)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, errors.New(
			strings.Join(path, ".") + " is not a boolean type")
	}
	return b, nil
}

func AsID(v interface{}) (ID, bool) {
	if i64, ok := AsInt64(v); ok {
		return ID(i64), true
	}
	return ID(0), false
}

func AsURI(v interface{}) (URI, bool) {
	switch v := v.(type) {
	case URI:
		return v, true
	case string:
		return URI(v), true
	case []byte:
		return URI(string(v)), true
	}
	return URI(""), false
}

func AsInt64(v interface{}) (int64, bool) {
	switch v := v.(type) {
	case int64:
		return v, true
	case ID:
		return int64(v), true
	case uint64:
		return int64(v), true
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case float64:
		return int64(v), true
	case uint:
		return int64(v), true
	case uint32:
		return int64(v), true
	}
	return 0, false
}

func AsDict(v interface{}) (Dict, bool) {
	return NormalizeDict(v), v != nil
}

func AsList(v interface{}) (List, bool) {
	switch v := v.(type) {
	case List:
		return v, true
	case []interface{}:
		return List(v), true
	}
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Slice {
		return nil, false
	}
	list := make(List, val.Len())
	for i := 0; i < val.Len(); i++ {
		list[i] = val.Index(i).Interface()
	}
	return list, true
}

// OptionString returns the string value of the option with the specified name.
// If the option is not present or is not a string type, an empty string is
// returned.
func OptionString(opts Dict, optionName string) string {
	var opt string
	if _opt, ok := opts[optionName]; ok && _opt != nil {
		opt, _ = _opt.(string)
	}
	return opt
}

// OptionURI returns the URI value of the option with the specified name.
// If the option is not present, an empty URI is returned.
func OptionURI(opts Dict, optionName string) URI {
	var opt URI
	if _opt, ok := opts[optionName]; ok && _opt != nil {
		opt, _ = AsURI(_opt)
	}
	return opt
}

// OptionID returns the ID value of the option with the specified name.
// If the option is not present, an ID 0 is returned.
func OptionID(opts Dict, optionName string) ID {
	var opt ID
	if _opt, ok := opts[optionName]; ok && _opt != nil {
		opt, _ = AsID(_opt)
	}
	return opt
}

// OptionInt64 returns the int64 value of the option with the specified name.
// If the option is not present, a value of 0 is returned.
func OptionInt64(opts Dict, optionName string) int64 {
	if opt, ok := opts[optionName]; ok && opt != nil {
		if i64, ok := AsInt64(opt); ok {
			return i64
		}
	}
	return 0
}

// OptionString returns the boolean value of the option with the specified
// name.  If the option is not present, false is returned.
func OptionFlag(opts Dict, optionName string) bool {
	var opt bool
	if _opt, ok := opts[optionName]; ok && _opt != nil {
		opt, _ = _opt.(bool)
	}
	return opt
}

// SetOption sets a single option name-value pair in message options dict.
func SetOption(dict Dict, name string, value interface{}) Dict {
	if dict == nil {
		dict = Dict{}
	}
	dict[name] = value
	return dict
}
