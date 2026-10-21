package runner

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// matchList performs a placeholder-aware deep match between an expected
// list (loaded from YAML) and an actual list (from a deserialized WAMP
// message).
//
// Placeholders in expected strings:
//
//	"{{$name}}"          — match any value, capture under `name`
//	"{{$name:int}}"      — match an int (allows JSON-number floats), capture
//	"{{$name:string}}"   — match a string
//	"{{$name:uri}}"      — match a string shaped like a WAMP URI
//	"{{$name:dict}}"     — match a JSON object
//
// If `name` is reused across steps, subsequent appearances must equal the
// captured value (allows e.g. session ID to be checked across messages).
//
// Special expected value `"_"` matches anything without capturing.
func matchList(expected, actual []any, captures map[string]any) error {
	return matchValue("$", expected, actual, captures)
}

func matchValue(path string, expected, actual any, captures map[string]any) error {
	// Normalize JSON-numeric floats to int64 when expected is an integer
	// (yaml.v3 uses int, json.Unmarshal uses float64).
	expected = normalizeNumeric(expected)
	actual = normalizeNumeric(actual)

	if s, ok := expected.(string); ok {
		if s == "_" {
			return nil
		}
		if isPlaceholder(s) {
			return matchPlaceholder(path, s, actual, captures)
		}
	}

	switch e := expected.(type) {
	case []any:
		a, ok := actual.([]any)
		if !ok {
			return fmt.Errorf("at %s: expected list, got %T (%v)", path, actual, actual)
		}
		if len(e) != len(a) {
			return fmt.Errorf("at %s: expected list of len %d, got len %d (expected=%v actual=%v)",
				path, len(e), len(a), e, a)
		}
		for i := range e {
			if err := matchValue(fmt.Sprintf("%s[%d]", path, i), e[i], a[i], captures); err != nil {
				return err
			}
		}
		return nil

	case map[string]any:
		a, ok := actual.(map[string]any)
		if !ok {
			return fmt.Errorf("at %s: expected dict, got %T (%v)", path, actual, actual)
		}
		// Subset match: every key in expected must exist with a matching
		// value in actual. Extra keys in actual are ignored — this lets a
		// plan assert "the broker advertises pattern_based_subscription"
		// without having to enumerate every advertised feature.
		for k, ev := range e {
			av, present := a[k]
			if !present {
				return fmt.Errorf("at %s.%s: missing key (actual=%v)", path, k, a)
			}
			if err := matchValue(path+"."+k, ev, av, captures); err != nil {
				return err
			}
		}
		return nil
	}

	if !reflect.DeepEqual(expected, actual) {
		return fmt.Errorf("at %s: expected %v (%T), got %v (%T)",
			path, expected, expected, actual, actual)
	}
	return nil
}

func isPlaceholder(s string) bool {
	return strings.HasPrefix(s, "{{$") && strings.HasSuffix(s, "}}")
}

func matchPlaceholder(path, ph string, actual any, captures map[string]any) error {
	body := strings.TrimSuffix(strings.TrimPrefix(ph, "{{$"), "}}")
	name, kind, _ := strings.Cut(body, ":")

	if kind != "" {
		switch kind {
		case "int":
			if _, err := asInt(actual); err != nil {
				return fmt.Errorf("at %s: placeholder %q expects int, got %T (%v)", path, name, actual, actual)
			}
		case "string":
			if _, ok := actual.(string); !ok {
				return fmt.Errorf("at %s: placeholder %q expects string, got %T", path, name, actual)
			}
		case "uri":
			s, ok := actual.(string)
			if !ok {
				return fmt.Errorf("at %s: placeholder %q expects URI string, got %T", path, name, actual)
			}
			if s == "" || strings.Contains(s, "..") {
				return fmt.Errorf("at %s: placeholder %q got malformed URI %q", path, name, s)
			}
		case "dict":
			if _, ok := actual.(map[string]any); !ok {
				return fmt.Errorf("at %s: placeholder %q expects dict, got %T", path, name, actual)
			}
		default:
			return fmt.Errorf("at %s: placeholder %q has unknown type %q", path, name, kind)
		}
	}

	if name == "" {
		return nil
	}
	if prev, seen := captures[name]; seen {
		if !reflect.DeepEqual(prev, actual) {
			return fmt.Errorf("at %s: placeholder %q was %v previously, now %v",
				path, name, prev, actual)
		}
		return nil
	}
	captures[name] = actual
	return nil
}

// substituteCaptures walks a YAML-loaded list and replaces any string
// shaped like "{{$name}}" or "{{$name:type}}" with the captured value
// from a previous `expect` step. Used on the send side so a plan can
// echo back ids the router assigned (e.g. yield to an INVOCATION id
// captured from a prior INVOCATION expect).
//
// Returns an error if a placeholder references a name that hasn't been
// captured yet.
func substituteCaptures(v any, captures map[string]any) (any, error) {
	switch x := v.(type) {
	case string:
		if !isPlaceholder(x) {
			return x, nil
		}
		body := strings.TrimSuffix(strings.TrimPrefix(x, "{{$"), "}}")
		name, _, _ := strings.Cut(body, ":")
		if name == "" {
			return nil, fmt.Errorf("send placeholder %q has no name to substitute", x)
		}
		val, ok := captures[name]
		if !ok {
			return nil, fmt.Errorf("send placeholder %q references uncaptured name", x)
		}
		return val, nil
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			r, err := substituteCaptures(item, captures)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			r, err := substituteCaptures(val, captures)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			r, err := substituteCaptures(val, captures)
			if err != nil {
				return nil, err
			}
			out[fmt.Sprint(k)] = r
		}
		return out, nil
	default:
		return v, nil
	}
}

// asInt accepts int, int64, float64 (YAML/JSON numeric forms) and returns
// the int64 value.
func asInt(v any) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int64:
		return n, nil
	case float64:
		if n != float64(int64(n)) {
			return 0, fmt.Errorf("non-integer float %v", n)
		}
		return int64(n), nil
	default:
		return 0, fmt.Errorf("not a number: %T", v)
	}
}

// normalizeNumeric coerces float64 values that are whole numbers to int64,
// so YAML's int defaults match JSON's float64 default after deserialization.
func normalizeNumeric(v any) any {
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return int64(f)
	}
	if n, ok := v.(int); ok {
		return int64(n)
	}
	return v
}

// serializeListAsBytes marshals a YAML-loaded list as JSON bytes — the
// canonical wire form of a WAMP message under the JSON serializer.
func serializeListAsBytes(list []any) ([]byte, error) {
	return json.Marshal(yamlToJSONCompat(list))
}

// jsonBytesToList unmarshals JSON bytes into a []any list.
func jsonBytesToList(b []byte) ([]any, error) {
	var out []any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// yamlToJSONCompat walks a YAML-loaded value and converts the
// map[interface{}]interface{} keys yaml.v3 sometimes emits into string
// keys so json.Marshal accepts them.
func yamlToJSONCompat(v any) any {
	switch x := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = yamlToJSONCompat(val)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = yamlToJSONCompat(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = yamlToJSONCompat(val)
		}
		return out
	default:
		return x
	}
}
