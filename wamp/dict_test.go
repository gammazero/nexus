package wamp

import (
	"errors"
	"testing"
)

func hasRole(d Dict, role string) bool {
	_, err := DictValue(d, []string{"roles", role})
	return err == nil
}

func hasFeature(d Dict, role, feature string) bool {
	b, _ := DictFlag(d, []string{"roles", role, "features", feature})
	return b
}

func checkRoles(sess Session) error {
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
	dict := Dict{}
	clientRoles := map[string]Dict{
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

	if err := checkRoles(Session{Details: dict}); err != nil {
		t.Fatal(err)
	}

	sess := Session{ID: ID(193847264)}
	if sess.String() != "193847264" {
		t.Fatal("Got unexpected session ID string")
	}

	dict = NormalizeDict(dict)

	roleVals, err := DictValue(dict, []string{"roles"})
	if err != nil {
		t.Fatal(err)
	}
	for k := range roleVals.(Dict) {
		if k == "badrole" {
			if recognizeRole(k) {
				t.Fatal("badrole is recognized")
			}
		} else {
			if !recognizeRole(k) {
				t.Fatal("role", k, "not recognized")
			}
		}
	}

	// Check again after conversion.
	if err := checkRoles(Session{Details: dict}); err != nil {
		t.Fatal(err)
	}

	dict = Dict{
		"roles": Dict{
			"subscriber": Dict{
				"features": Dict{
					"publisher_identification": true,
				},
			},
			"publisher": struct{}{},
			"callee":    struct{}{},
			"caller": Dict{
				"features": Dict{
					"call_timeout": true,
				},
			},
		},
		"authmethods": []string{"anonymous", "ticket"},
	}
	if err := checkRoles(Session{Details: dict}); err != nil {
		t.Fatal(err)
	}
}

func TestOptions(t *testing.T) {
	options := Dict{
		"disclose_me":  true,
		"call_timeout": 5000,
		"mode":         "killnowait",
		"flags": Dict{
			"flag_a":   true,
			"flag_b":   false,
			"not_flag": 123,
		},
	}

	options = NormalizeDict(options)

	if !OptionFlag(options, "disclose_me") {
		t.Fatal("missing or bad option flag")
	}
	if OptionFlag(options, "not_here") {
		t.Fatal("expected false value")
	}
	if OptionFlag(options, "call_timeout") {
		t.Fatal("expected false value")
	}

	if OptionString(options, "not_here") != "" {
		t.Fatal("expected empty string")
	}
	if OptionString(options, "call_timeout") != "" {
		t.Fatal("expected empty string")
	}

	timeout := OptionInt64(options, "call_timeout")
	if timeout != 5000 {
		t.Fatal("wrong timeout value")
	}

	if OptionString(options, "mode") != "killnowait" {
		t.Fatal("did not get expected value")
	}

	boolOpts := map[string]bool{"disclose_me": true}

	if !OptionFlag(NormalizeDict(boolOpts), "disclose_me") {
		t.Fatal("missing or bad option flag")
	}
	if OptionFlag(NormalizeDict(boolOpts), "not_here") {
		t.Fatal("expected false value")
	}

	fval, err := DictFlag(options, []string{"flags", "flag_a"})
	if err != nil || !fval {
		t.Fatal("Failed to get flag")
	}
	fval, err = DictFlag(options, []string{"flags", "flag_b"})
	if err != nil || fval {
		t.Fatal("Failed to get flag")
	}
	_, err = DictFlag(options, []string{"flags", "flag_c"})
	if err == nil {
		t.Fatal("Expected error for invalid flag path")
	}
	_, err = DictFlag(options, []string{"no_flags", "flag_a"})
	if err == nil {
		t.Fatal("Expected error for invalid flag path")
	}
	_, err = DictFlag(options, []string{"flags", "not_flag"})
	if err == nil {
		t.Fatal("Expected error for non-bool flag value")
	}

	uri := URI("some.test.uri")
	SetOption(options, "uri", uri)
	if OptionURI(options, "uri") != uri {
		t.Fatal("failed to get uri")
	}

	id := ID(1234)
	newDict := SetOption(nil, "id", id)
	if OptionID(newDict, "id") != id {
		t.Fatal("failed to get id")
	}

	if OptionInt64(options, "call_timeout") != int64(5000) {
		t.Fatal("Failed to get int64 option")
	}
	if OptionInt64(options, "mode") != 0 {
		t.Fatal("Expected 0 for invalid int64 option")
	}
}

func TestConversion(t *testing.T) {
	num := 1234
	uri := URI("some.test.uri")
	str := "hello"
	bytes := []byte{41, 42, 43}
	dict := Dict{"num": num, "uri": uri, "str": str, "bytes": bytes}
	list := List{num, uri, str, bytes}
	ilist := []interface{}{interface{}(num), uri, str, bytes}

	d, ok := AsDict(interface{}(dict))
	if !ok || len(d) == 0 {
		t.Error("Failed to convert to Dict")
	}
	if d, ok = AsDict(interface{}(num)); ok || d != nil {
		t.Error("Should fail converting int to Dict")
	}

	l, ok := AsList(ilist)
	if !ok || len(l) != len(list) {
		t.Error("Failed to convert to List")
	}
	if l, ok = AsList(list); !ok || len(l) != len(list) {
		t.Error("Failed to convert to List")
	}
	if l, ok = AsList(bytes); !ok || len(l) != len(bytes) {
		t.Error("Failed to convert to List")
	}
	if l, ok = AsList(num); ok || l != nil {
		t.Error("Should fail converting int to List")
	}

	if _, ok = AsID(num); !ok {
		t.Error("ID conversion failed")
	}
	if _, ok = AsID(str); ok {
		t.Error("Invalid ID conversion")
	}

	if _, ok = AsString(str); !ok {
		t.Error("String conversion failed")
	}
	if _, ok = AsString(uri); !ok {
		t.Error("String conversion failed")
	}
	if _, ok = AsString(bytes); !ok {
		t.Error("String conversion failed")
	}
	s, ok := AsString(num)
	if ok || s != "" {
		t.Error("Should fail converting int to string")
	}

	if _, ok = AsURI(uri); !ok {
		t.Error("URI conversion failed")
	}
	if _, ok = AsURI(str); !ok {
		t.Error("String conversion failed")
	}
	if _, ok = AsURI(bytes); !ok {
		t.Error("URI conversion failed")
	}
	u, ok := AsURI(num)
	if ok || u != "" {
		t.Error("Should fail converting int to URI")
	}

	if _, ok = AsInt64(int64(num)); !ok {
		t.Error("ID conversion failed")
	}
	if _, ok = AsInt64(ID(num)); !ok {
		t.Error("ID conversion failed")
	}
	if _, ok = AsInt64(uint64(num)); !ok {
		t.Error("ID conversion failed")
	}
	if _, ok = AsInt64(int(num)); !ok {
		t.Error("ID conversion failed")
	}
	if _, ok = AsInt64(int32(num)); !ok {
		t.Error("ID conversion failed")
	}
	if _, ok = AsInt64(int64(num)); !ok {
		t.Error("ID conversion failed")
	}
	if _, ok = AsInt64(uint(num)); !ok {
		t.Error("ID conversion failed")
	}
	if _, ok = AsInt64(uint32(num)); !ok {
		t.Error("ID conversion failed")
	}
	if _, ok = AsInt64(uint64(num)); !ok {
		t.Error("ID conversion failed")
	}
	if _, ok = AsInt64(float64(num)); !ok {
		t.Error("ID conversion failed")
	}
	if _, ok = AsInt64(str); ok {
		t.Error("Invalid ID conversion")
	}

}

func BenchmarkNormalized(b *testing.B) {
	dict := Dict{}
	clientRoles := map[string]Dict{
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
	dict = NormalizeDict(dict)
	sess := Session{Details: dict}
	for i := 0; i < b.N; i++ {
		checkRoles(sess)
	}
}

func BenchmarkNotNormalized(b *testing.B) {
	dict := Dict{}
	clientRoles := map[string]Dict{
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
	sess := Session{Details: dict}
	for i := 0; i < b.N; i++ {
		checkRoles(sess)
	}
}
