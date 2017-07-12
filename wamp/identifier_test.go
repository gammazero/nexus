package wamp

import "testing"

func TestURIPrefixMatch(t *testing.T) {
	uri := URI("this.is.a.test")
	matches := []URI{
		"this.is.a",
		"this.is."}
	for i := range matches {
		if !uri.PrefixMatch(matches[i]) {
			t.Error("expected prefix", matches[i], "to match", uri)
		}
	}

	nonMatches := []URI{
		"this.is.a.test.ok",
		"not.a.test"}
	for i := range nonMatches {
		if uri.PrefixMatch(nonMatches[i]) {
			t.Error("expected prefix", nonMatches[i], "to not match", uri)
		}
	}
}

func TestURIWildcardMatch(t *testing.T) {
	uri := URI("this.is.a.test")
	matches := []URI{
		"this.is.a.test",
		"this.is..test",
		"this...test",
		"this..a.",
		".is.a.test",
		"this.is.a.",
		".is.a.",
		"..."}
	for i := range matches {
		if !uri.WildcardMatch(matches[i]) {
			t.Error("expected wildcard", matches[i], "to match", uri)
		}
	}

	nonMatches := []URI{
		"this.is.a.bird",
		"this.is.test",
		".is..test.",
		"...."}
	for i := range nonMatches {
		if uri.WildcardMatch(nonMatches[i]) {
			t.Error("expected wildcard", nonMatches[i], "to not match", uri)
		}
	}
}

// URI components (the parts between two .s, the head part up to the first .,
// the tail part after the last .) MUST NOT contain a ., # or whitespace
// characters and MUST NOT be empty (zero-length strings).
func TestValidURI(t *testing.T) {
	strictGood := []URI{
		"this.is.a.good_test",
		"this.is.test42",
		"test.11_22_33.v88.something",
		"somewhere"}
	for i := range strictGood {
		if !strictGood[i].ValidURI(true, "") {
			t.Error("expected", strictGood[i], "to be valid")
		}
	}

	strictGoodPrefix := []URI{
		"this.is.a.good_test",
		"this.is.hello_123",
		"somewhere"}
	for i := range strictGoodPrefix {
		if !strictGoodPrefix[i].ValidURI(true, "") {
			t.Error("expected", strictGoodPrefix[i], "to be valid")
		}
	}

	strictBadPrefix := []URI{
		"this.is.a..",
		"this..test",
		"this.is.",
		".somewhere",
		".."}
	for i := range strictBadPrefix {
		if strictBadPrefix[i].ValidURI(true, "") {
			t.Error("expected", strictBadPrefix[i], "to be invalid")
		}
	}

	strictGoodWildcard := []URI{
		"this.is.a.test",
		"this.is..test",
		"this...test",
		"this..a.",
		".is.a.test",
		"this.is.a.",
		".is.a.",
		"..."}
	for i := range strictGoodWildcard {
		if !strictGoodWildcard[i].ValidURI(true, "wildcard") {
			t.Error("expected", strictGoodWildcard[i], "to be valid")
		}
	}

	strictBad := []URI{
		".is.not.good",
		"this#is_not.allowed",
		"Mixed.cAsE.URI",
		"this.one has.whitespace"}
	for i := range strictBad {
		if strictBad[i].ValidURI(true, "") {
			t.Error("expected", strictBad[i], "to be invalid")
		}
	}
}
