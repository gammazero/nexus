package wamp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestURIPrefixMatch(t *testing.T) {
	uri := URI("this.is.a.test")
	matches := []URI{
		"this.is.a",
		"this.is.",
		"this.i",
	}
	for i := range matches {
		require.True(t, uri.PrefixMatch(matches[i]), "expected prefix to match")
	}

	nonMatches := []URI{
		"this.is.a.test.ok",
		"not.a.test"}
	for i := range nonMatches {
		require.False(t, uri.PrefixMatch(nonMatches[i]), "expected prefix to not match")
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
		require.True(t, uri.WildcardMatch(matches[i]), "expected wildcard to match")
	}

	nonMatches := []URI{
		"this.is.a",
		"this.is.a.bird",
		"this.is.test",
		".is..test.",
		"...."}
	for i := range nonMatches {
		require.False(t, uri.WildcardMatch(nonMatches[i]), "expected wildcard to not match")
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
		require.True(t, strictGood[i].ValidURI(true, ""))
	}

	strictBad := []URI{
		".is.not.good",
		"this#is_not.allowed",
		"Mixed.cAsE.URI",
		"this.one has.whitespace"}
	for i := range strictBad {
		require.False(t, strictBad[i].ValidURI(true, ""))
	}

	strictGoodPrefix := []URI{
		"this.is.a.good_test",
		"this.is.hello_123",
		"somewhere"}
	for i := range strictGoodPrefix {
		require.True(t, strictGoodPrefix[i].ValidURI(true, "prefix"))
	}

	strictBadPrefix := []URI{
		"this.is.a..",
		"this..test",
		".somewhere",
		".."}
	for i := range strictBadPrefix {
		require.False(t, strictBadPrefix[i].ValidURI(true, "prefix"))
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
		require.True(t, strictGoodWildcard[i].ValidURI(true, "wildcard"))
	}

	strictBadWildcard := []URI{
		"this#is_not.allowed",
		"Mixed.cAsE.URI",
		"this.one has.whitespace"}
	for i := range strictBadWildcard {
		require.False(t, strictBadWildcard[i].ValidURI(true, "wildcard"))
	}

	looseGood := []URI{
		"this.is.a.good_test",
		"this.is.test42",
		"test.11_22_33.v88.something",
		"somewhere"}
	for i := range looseGood {
		require.True(t, looseGood[i].ValidURI(false, ""))
	}

	looseBad := []URI{
		".is.not.good",
		"this#is_not.allowed",
		"this.one has.whitespace"}
	for i := range looseBad {
		require.False(t, looseBad[i].ValidURI(false, ""))
	}

	looseGoodPrefix := []URI{
		"this.is.a.good_test",
		"this.is.H-E-L-L-O_123",
		"somewhere"}
	for i := range looseGoodPrefix {
		require.True(t, looseGoodPrefix[i].ValidURI(false, "prefix"))
	}

	looseBadPrefix := []URI{
		"this.is.a..",
		"this..test",
		"this..test",
		".somewhere",
		".."}
	for i := range looseBadPrefix {
		require.False(t, looseBadPrefix[i].ValidURI(false, "prefix"))
	}

	looseGoodWildcard := []URI{
		"this.is.a.test",
		"This.Is.a.TEST",
		"this.is..t-e-s-t",
		"this...test",
		"this..a.",
		".is.a.test",
		"this.is.a.",
		".is.a.",
		"..."}
	for i := range looseGoodWildcard {
		require.True(t, looseGoodWildcard[i].ValidURI(false, "wildcard"))
	}

	looseBadWildcard := []URI{
		"this#is_not.allowed",
		"this.one has.whitespace"}
	for i := range looseBadWildcard {
		require.False(t, looseBadWildcard[i].ValidURI(false, "wildcard"))
	}
}
