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
