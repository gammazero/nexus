package wamp

import "reflect"

// AsString is an extended type assertion for string.
func AsString(v interface{}) (string, bool) {
	switch v := v.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	case URI:
		return string(v), true
	}
	return "", false
}

// AsID is an extended type assertion for ID.
func AsID(v interface{}) (ID, bool) {
	if i64, ok := AsInt64(v); ok {
		return ID(i64), true
	}
	return ID(0), false
}

// AsURI is an extended type assertion for URI.
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

// AsInt64 is an extended type assertion for int64.
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
	case uint:
		return int64(v), true
	case uint32:
		return int64(v), true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	}
	return 0, false
}

// AsFloat64 is an extended type assertion for float64.
func AsFloat64(v interface{}) (float64, bool) {
	switch v := v.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int64:
		return float64(v), true
	case ID:
		return float64(v), true
	case uint64:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	}
	return 0.0, false
}

// AsBool is an extended type assertion for bool.
func AsBool(v interface{}) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

// AsDict is an extended type assertion for Dict.
func AsDict(v interface{}) (Dict, bool) {
	n := NormalizeDict(v)
	return n, n != nil
}

// AsList is an extended type assertion for List.
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

// ListToStrings converts a List to a slice of string.  Returns the string
// slice and a boolean indicating if the conversion was successful.
func ListToStrings(list List) ([]string, bool) {
	if len(list) == 0 {
		return nil, true
	}
	strs := make([]string, len(list))
	for i := range list {
		s, ok := AsString(list[i])
		if !ok {
			return nil, false
		}
		strs[i] = s
	}
	return strs, true
}

// OptionString returns named value as string; empty string if missing or not
// string type.
func OptionString(opts Dict, optionName string) string {
	opt, _ := AsString(opts[optionName])
	return opt
}

// OptionURI returns named value as URI; URI("") if missing or not URI type.
func OptionURI(opts Dict, optionName string) URI {
	opt, _ := AsURI(opts[optionName])
	return opt
}

// OptionID returns named value as ID; ID(0) if missing or not ID type.
func OptionID(opts Dict, optionName string) ID {
	opt, _ := AsID(opts[optionName])
	return opt
}

// OptionInt64 returns named value as int64; 0 if missing or not integer type.
func OptionInt64(opts Dict, optionName string) int64 {
	opt, _ := AsInt64(opts[optionName])
	return opt
}

// OptionFlag returns named value as bool; false if missing or not bool type.
func OptionFlag(opts Dict, optionName string) bool {
	opt, _ := AsBool(opts[optionName])
	return opt
}
