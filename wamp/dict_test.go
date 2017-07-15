package wamp

import (
	"errors"
	"testing"
)

func hasRole(d map[string]interface{}, role string) bool {
	_, err := DictValue(d, []string{"roles", role})
	return err == nil
}

func hasFeature(d map[string]interface{}, role, feature string) bool {
	b, _ := DictFlag(d, []string{"roles", role, "features", feature})
	return b
}

func checkRoles(dict map[string]interface{}) error {
	if !hasRole(dict, "caller") {
		return errors.New("session does not have caller role")
	}
	if !hasRole(dict, "publisher") {
		return errors.New("session does not have publisher role")
	}
	if hasRole(dict, "foo") {
		return errors.New("session has role foo")
	}
	if hasFeature(dict, "caller", "bar") {
		return errors.New("caller has feature bar")
	}
	if !hasFeature(dict, "caller", "call_timeout") {
		return errors.New("caller missing feature call_timeout")
	}
	if hasFeature(dict, "publisher", "call_timeout") {
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
	dict := map[string]interface{}{}
	clientRoles := map[string]map[string]interface{}{
		"publisher": map[string]interface{}{},
		"subscriber": map[string]interface{}{
			"junk": struct{}{}},
		"callee": map[string]interface{}{
			"Hello": "world"},
		"caller":  map[string]interface{}{},
		"badrole": map[string]interface{}{},
	}
	boolMap := map[string]bool{"call_timeout": true}
	clientRoles["caller"]["features"] = boolMap
	dict["roles"] = clientRoles

	if err := checkRoles(dict); err != nil {
		t.Fatal(err)
	}

	dict = NormalizeDict(dict)

	roleVals, err := DictValue(dict, []string{"roles"})
	if err != nil {
		t.Fatal(err)
	}
	for k, _ := range roleVals.(map[string]interface{}) {
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
	if err := checkRoles(dict); err != nil {
		t.Fatal(err)
	}

	dict = map[string]interface{}{
		"roles": map[string]interface{}{
			"subscriber": map[string]interface{}{
				"features": map[string]interface{}{
					"publisher_identification": true,
				},
			},
			"publisher": struct{}{},
			"callee":    struct{}{},
			"caller": map[string]interface{}{
				"features": map[string]interface{}{
					"call_timeout": true,
				},
			},
		},
		"authmethods": []string{"anonymous", "ticket"},
	}
	if err := checkRoles(dict); err != nil {
		t.Fatal(err)
	}
}

func TestOptions(t *testing.T) {
	options := map[string]interface{}{
		"disclose_me":  true,
		"call_timeout": 5000,
		"mode":         "killnowait",
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

}

func BenchmarkNormalized(b *testing.B) {
	dict := map[string]interface{}{}
	clientRoles := map[string]map[string]interface{}{
		"publisher": map[string]interface{}{},
		"subscriber": map[string]interface{}{
			"junk": struct{}{}},
		"callee": map[string]interface{}{
			"Hello": "world"},
		"caller": map[string]interface{}{},
	}
	boolMap := map[string]bool{"call_timeout": true}
	clientRoles["caller"]["features"] = boolMap
	dict["roles"] = clientRoles

	b.ResetTimer()
	dict = NormalizeDict(dict)
	for i := 0; i < b.N; i++ {
		checkRoles(dict)
	}
}

func BenchmarkNotNormalized(b *testing.B) {
	dict := map[string]interface{}{}
	clientRoles := map[string]map[string]interface{}{
		"publisher": map[string]interface{}{},
		"subscriber": map[string]interface{}{
			"junk": struct{}{}},
		"callee": map[string]interface{}{
			"Hello": "world"},
		"caller": map[string]interface{}{},
	}
	boolMap := map[string]bool{"call_timeout": true}
	clientRoles["caller"]["features"] = boolMap
	dict["roles"] = clientRoles

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checkRoles(dict)
	}
}
