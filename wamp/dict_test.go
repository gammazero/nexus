package wamp_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

func checkRoles(sess *wamp.Session) error {
	if !sess.HasRole("caller") {
		return errors.New("session does not have caller role")
	}
	if !sess.HasRole("publisher") {
		return errors.New("session does not have publisher role")
	}
	if sess.HasRole("foo") {
		return errors.New("session has role foo")
	}
	if sess.HasFeature("caller", "bar") {
		return errors.New("caller has feature bar")
	}
	if !sess.HasFeature("caller", "call_timeout") {
		return errors.New("caller missing feature call_timeout")
	}
	if sess.HasFeature("publisher", "call_timeout") {
		return errors.New("publisher has feature call_timeout")
	}
	return nil
}

func recognizeRole(roleName string) bool {
	switch roleName {
	case "publisher", "subscriber", "caller", "callee":
	default:
		return false
	}
	return true
}

func TestHasRoleFeatureLookup(t *testing.T) {
	dict := wamp.Dict{}
	clientRoles := map[string]wamp.Dict{
		"publisher": {},
		"subscriber": {
			"junk": struct{}{}},
		"callee": {
			"Hello": "world"},
		"caller":  {},
		"badrole": {},
	}
	boolMap := map[string]bool{"call_timeout": true}
	clientRoles["caller"]["features"] = boolMap
	dict["roles"] = clientRoles

	err := checkRoles(wamp.NewSession(nil, 0, nil, dict))
	require.NoError(t, err)

	sess := &wamp.Session{ID: wamp.ID(193847264)}
	require.Equal(t, "193847264", sess.String(), "Got unexpected session ID string")

	dict = wamp.NormalizeDict(dict)

	roleVals, err := wamp.DictValue(dict, []string{"roles"})
	require.NoError(t, err)

	for k := range roleVals.(wamp.Dict) {
		if k == "badrole" {
			require.False(t, recognizeRole(k), "badrole is recognized")
		} else {
			require.True(t, recognizeRole(k), "role not recognized")
		}
	}

	// Check again after conversion.
	err = checkRoles(wamp.NewSession(nil, 0, nil, dict))
	require.NoError(t, err)

	dict = wamp.Dict{
		"roles": wamp.Dict{
			"subscriber": wamp.Dict{
				"features": wamp.Dict{
					"publisher_identification": true,
				},
			},
			"publisher": struct{}{},
			"callee":    struct{}{},
			"caller": wamp.Dict{
				"features": wamp.Dict{
					"call_timeout": true,
				},
			},
		},
		"authmethods": []string{"anonymous", "ticket"},
	}
	err = checkRoles(wamp.NewSession(nil, 0, nil, dict))
	require.NoError(t, err)
}

func TestOptions(t *testing.T) {
	options := wamp.Dict{
		"disclose_me":  true,
		"call_timeout": 5000,
		"mode":         "killnowait",
		"flags": wamp.Dict{
			"flag_a":   true,
			"flag_b":   false,
			"not_flag": 123,
		},
	}

	options = wamp.NormalizeDict(options)

	require.True(t, wamp.OptionFlag(options, "disclose_me"))
	require.False(t, wamp.OptionFlag(options, "not_here"))
	require.False(t, wamp.OptionFlag(options, "call_timeout"))
	require.Empty(t, wamp.OptionString(options, "not_here"))
	require.Empty(t, wamp.OptionString(options, "call_timeout"))
	require.Equal(t, int64(5000), wamp.OptionInt64(options, "call_timeout"))
	require.Equal(t, "killnowait", wamp.OptionString(options, "mode"))

	boolOpts := map[string]bool{"disclose_me": true}

	require.True(t, wamp.OptionFlag(wamp.NormalizeDict(boolOpts), "disclose_me"))
	require.False(t, wamp.OptionFlag(wamp.NormalizeDict(boolOpts), "not_here"))

	fval, err := wamp.DictFlag(options, []string{"flags", "flag_a"})
	require.NoError(t, err, "Failed to get flag")
	require.True(t, fval, "Failed to get flag")

	fval, err = wamp.DictFlag(options, []string{"flags", "flag_b"})
	require.NoError(t, err, "Failed to get flag")
	require.False(t, fval, "Failed to get flag")

	_, err = wamp.DictFlag(options, []string{"flags", "flag_c"})
	require.Error(t, err, "Expected error for invalid flag path")
	_, err = wamp.DictFlag(options, []string{"no_flags", "flag_a"})
	require.Error(t, err, "Expected error for invalid flag path")
	_, err = wamp.DictFlag(options, []string{"flags", "not_flag"})
	require.Error(t, err, "Expected error for non-bool flag value")

	uri := wamp.URI("some.test.uri")
	wamp.SetOption(options, "uri", uri)
	require.Equal(t, uri, wamp.OptionURI(options, "uri"), "failed to get uri")

	id := wamp.ID(1234)
	newDict := wamp.SetOption(nil, "id", id)
	require.Equal(t, id, wamp.OptionID(newDict, "id"), "failed to get id")

	require.Equal(t, int64(5000), wamp.OptionInt64(options, "call_timeout"), "Failed to get int64 option")
	require.Zero(t, wamp.OptionInt64(options, "mode"), "Expected 0 for invalid int64 option")
}

func BenchmarkNormalized(b *testing.B) {
	dict := wamp.Dict{}
	clientRoles := map[string]wamp.Dict{
		"publisher": {},
		"subscriber": {
			"junk": struct{}{}},
		"callee": {
			"Hello": "world"},
		"caller": {},
	}
	boolMap := map[string]bool{"call_timeout": true}
	clientRoles["caller"]["features"] = boolMap
	dict["roles"] = clientRoles

	b.ResetTimer()
	dict = wamp.NormalizeDict(dict)
	sess := &wamp.Session{Details: dict}
	for i := 0; i < b.N; i++ {
		checkRoles(sess)
	}
}

func BenchmarkNotNormalized(b *testing.B) {
	dict := wamp.Dict{}
	clientRoles := map[string]wamp.Dict{
		"publisher": {},
		"subscriber": {
			"junk": struct{}{}},
		"callee": {
			"Hello": "world"},
		"caller": {},
	}
	boolMap := map[string]bool{"call_timeout": true}
	clientRoles["caller"]["features"] = boolMap
	dict["roles"] = clientRoles

	b.ResetTimer()
	sess := &wamp.Session{Details: dict}
	for i := 0; i < b.N; i++ {
		checkRoles(sess)
	}
}

func TestNormalizeDict(t *testing.T) {
	// Values that are not maps are not converted.
	require.Nil(t, wamp.NormalizeDict(nil))
	require.Nil(t, wamp.NormalizeDict("hello"))
	require.Nil(t, wamp.NormalizeDict([]any{1, 2}))

	empty := wamp.NormalizeDict(map[string]int{})
	require.NotNil(t, empty)
	require.Empty(t, empty)

	require.Equal(t, wamp.Dict{"a": true, "b": false},
		wamp.NormalizeDict(map[string]bool{"a": true, "b": false}))

	// Nested maps are converted to Dict, and a []any to List. Other values remain
	// the same.
	nested := map[string]any{
		"roles": map[string]map[string]int{
			"caller": {"timeout": 5},
		},
		"list":    []any{1, "two"},
		"strings": []string{"a", "b"},
		"dict":    wamp.Dict{"x": map[string]string{"y": "z"}},
		"nil":     nil,
	}
	expect := wamp.Dict{
		"roles": wamp.Dict{
			"caller": wamp.Dict{"timeout": 5},
		},
		"list":    wamp.List{1, "two"},
		"strings": []string{"a", "b"},
		"dict":    wamp.Dict{"x": wamp.Dict{"y": "z"}},
		"nil":     nil,
	}
	require.Equal(t, expect, wamp.NormalizeDict(nested))

	// The original is not changed.
	require.IsType(t, map[string]map[string]int{}, nested["roles"])
	require.IsType(t, []any{}, nested["list"])
	require.IsType(t, map[string]string{}, nested["dict"].(wamp.Dict)["x"])

	// Keys that are not strings are left out. Keys whose underlying type is string
	// are kept.
	anyKeys := map[any]any{
		"a":             1,
		2:               "b",
		nil:             "c",
		wamp.URI("uri"): "d",
		"e":             map[any]any{"f": 4, 5: 6},
	}
	require.Equal(t,
		wamp.Dict{"a": 1, "uri": "d", "e": wamp.Dict{"f": 4}},
		wamp.NormalizeDict(anyKeys))
	require.Empty(t, wamp.NormalizeDict(map[int]string{1: "a"}))
	require.Equal(t, wamp.Dict{"x": 1},
		wamp.NormalizeDict(map[wamp.URI]int{"x": 1}))
}

func TestNormalizeDictLists(t *testing.T) {
	type namedList []any

	// Maps in lists, and in lists in lists, are converted.
	listMaps := map[string]any{
		"list": []any{
			map[string]any{"a": 1},
			[]any{map[any]any{"b": 2}},
			"c",
		},
		"wamplist": wamp.List{map[string]int{"d": 3}},
		"named":    namedList{map[string]any{"e": 4}},
	}
	expect := wamp.Dict{
		"list": wamp.List{
			wamp.Dict{"a": 1},
			wamp.List{wamp.Dict{"b": 2}},
			"c",
		},
		"wamplist": wamp.List{wamp.Dict{"d": 3}},
		"named":    wamp.List{wamp.Dict{"e": 4}},
	}
	require.Equal(t, expect, wamp.NormalizeDict(listMaps))

	// The original lists are not changed.
	orig := listMaps["list"].([]any)
	require.IsType(t, map[string]any{}, orig[0])
	require.IsType(t, map[any]any{}, orig[1].([]any)[0])
	require.IsType(t, map[string]int{}, listMaps["wamplist"].(wamp.List)[0])

	// A []any is converted whatever the type of the map's values.
	require.Equal(t, wamp.Dict{"a": wamp.List{1, 2}},
		wamp.NormalizeDict(map[string][]any{"a": {1, 2}}))
	require.Equal(t, wamp.Dict{"a": wamp.List{wamp.Dict{"b": 1}}},
		wamp.NormalizeDict(wamp.Dict{"a": []any{wamp.Dict{"b": 1}}}))

	// A nil []any stays nil, as a List.
	got := wamp.NormalizeDict(map[string]any{"a": []any(nil)})
	require.IsType(t, wamp.List(nil), got["a"])
	require.Nil(t, got["a"])
}

func TestDictChild(t *testing.T) {
	child := wamp.Dict{"auth": "ticket"}
	dict := wamp.Dict{
		"dict":   child,
		"map":    map[string]any{"cookie": "abc"},
		"string": "hello",
		"nil":    nil,
	}

	// A Dict child is returned as is, not copied.
	got := wamp.DictChild(dict, "dict")
	require.Equal(t, child, got)
	got["added"] = true
	require.Equal(t, true, child["added"])

	// A child that is another map type is converted, and the parent is not
	// changed.
	require.Equal(t, wamp.Dict{"cookie": "abc"}, wamp.DictChild(dict, "map"))
	require.IsType(t, map[string]any{}, dict["map"])

	// Missing, nil, and non-map children give nil.
	require.Nil(t, wamp.DictChild(dict, "missing"))
	require.Nil(t, wamp.DictChild(dict, "nil"))
	require.Nil(t, wamp.DictChild(dict, "string"))
	require.Nil(t, wamp.DictChild(nil, "dict"))
}
