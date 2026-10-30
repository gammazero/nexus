package wamp

import (
	"errors"
	"reflect"
	"strings"
)

// Dict is a dictionary that maps keys to objects in a WAMP message.
type Dict map[string]any

// NormalizeDict returns a copy of the map v as a Dict, or nil if v is not a
// map. Every map nested in v, including one that is already a Dict, is also
// copied as a Dict, and every []any or List nested in v is copied as a List,
// so that maps in lists are converted too. Entries whose keys are not strings
// are left out. Other values remain the same.
//
// This is used for initial conversion of hello details. The original dict is
// not mutated.
func NormalizeDict(v any) Dict {
	switch m := v.(type) {
	case Dict:
		return normalizeMap(m)
	case map[string]any:
		return normalizeMap(Dict(m))
	case map[any]any:
		dict := make(Dict, len(m))
		for k, val := range m {
			if key, ok := stringKey(k); ok {
				dict[key] = normalizeValue(val)
			}
		}
		return dict
	}

	// Some other map type.
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map {
		return nil
	}
	dict := make(Dict, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		if key, ok := stringKey(iter.Key().Interface()); ok {
			dict[key] = normalizeValue(iter.Value().Interface())
		}
	}
	return dict
}

// normalizeMap returns a copy of m with its values normalized.
func normalizeMap(m Dict) Dict {
	dict := make(Dict, len(m))
	for k, v := range m {
		dict[k] = normalizeValue(v)
	}
	return dict
}

// normalizeList returns a copy of list with its elements normalized.
func normalizeList(list List) List {
	if list == nil {
		return nil
	}
	out := make(List, len(list))
	for i, v := range list {
		out[i] = normalizeValue(v)
	}
	return out
}

// normalizeValue returns v as a Dict if it is a map, and as a List if it is a
// []any or other slice that converts to a List, normalizing the values in it.
// Otherwise, it returns v.
func normalizeValue(v any) any {
	switch v := v.(type) {
	case nil, string, bool, int, int64, uint64, float64, []byte, URI, ID:
		return v
	case Dict, map[string]any, map[any]any:
		return NormalizeDict(v)
	case List:
		return normalizeList(v)
	case []any:
		return normalizeList(v)
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		return NormalizeDict(v)
	case reflect.Slice:
		listType := reflect.TypeFor[List]()
		if rv.Type().ConvertibleTo(listType) {
			return normalizeList(rv.Convert(listType).Interface().(List))
		}
	}
	return v
}

// stringKey returns k as a string if it is a string, or of a type whose
// underlying type is string, such as URI.
func stringKey(k any) (string, bool) {
	if s, ok := k.(string); ok {
		return s, true
	}
	rv := reflect.ValueOf(k)
	if rv.Kind() == reflect.String {
		return rv.String(), true
	}
	return "", false
}

// DictChild returns the child dictionary for the given key, or nil if not
// present.
//
// If the child is not a Dict, an attempt is made to convert it. The dict is
// not modified since features may be looked up concurrently for the same
// session.
func DictChild(dict Dict, key string) Dict {
	iface, ok := dict[key]
	if !ok || iface == nil {
		// Map does not have the specified key or value is nil.
		return nil
	}
	child, ok := iface.(Dict)
	if !ok {
		// value is not in expected form; try to convert. Session details are
		// normalized when session is attached, so this should not be necessary
		// normally.
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
//
//	DictValue(dict, strings.Split(path, "."))
//
// For example, the path []string{"roles","callee","features","call_timeout"}
// returns the value of the call_timeout feature as any. An error is returned
// if the value is not present.
func DictValue(dict Dict, path []string) (any, error) {
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
//
//	DictFlag(dict, strings.Split(path, "."))
//
// For example: "roles.subscriber.features.publisher_identification" returns
// the value of the publisher_identification feature. An error is returned if
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

// SetOption sets a single option name-value pair in message options dict.
func SetOption(dict Dict, name string, value any) Dict {
	if dict == nil {
		dict = Dict{}
	}
	dict[name] = value
	return dict
}
