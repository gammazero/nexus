package wamp

import (
	"regexp"
	"strings"
)

// IDs are integers between (inclusive) 0 and 2^53 (9007199254740992)
type ID uint64

// URIs are dot-separated identifiers, where each component *should* only
// contain letters, numbers or underscores.
//
// See the documentation for specifics:
// https://github.com/wamp-proto/wamp-proto/blob/master/rfc/text/basic/bp_identifiers.md#uris-uris
type URI string

// URI check regular expressions
var (
	// loose URI check disallowing empty URI components
	looseURINonEmpty = regexp.MustCompile(`^([^\s\.#]+\.)*([^\s\.#]+)$`)
	// loose URI check disallowing empty URI components in all but the last
	looseURILastEmpty = regexp.MustCompile(`^([^\s\.#]+\.)*([^\s\.#]*)$`)
	// loose URI check allowing empty URI components
	looseURIEmpty = regexp.MustCompile(`^(([^\s\.#]+\.)|\.)*([^\s\.#]+)?$`)
	// strict URI check disallowing empty URI components
	strictURINonEmpty = regexp.MustCompile(`^([0-9a-z_]+\.)*([0-9a-z_]+)$`)
	// strict URI check disallowing empty URI components in all but the last
	strictURILastEmpty = regexp.MustCompile(`^([0-9a-z_]+\.)*([0-9a-z_]*)$`)
	// strict URI check allowing empty URI components
	strictURIEmpty = regexp.MustCompile(`^(([0-9a-z_]+\.)|\.)*([0-9a-z_]+)?$`)
)

// ValidURI returns true if the URI complies with formatting rules determined
// by the strict flag and match type.
func (u URI) ValidURI(strict bool, match string) bool {
	if strict {
		if match == MatchWildcard {
			return strictURIEmpty.MatchString(string(u))
		}
		if match == MatchPrefix {
			return strictURILastEmpty.MatchString(string(u))
		}
		return strictURINonEmpty.MatchString(string(u))
	}
	if match == MatchWildcard {
		return looseURIEmpty.MatchString(string(u))
	}
	if match == MatchPrefix {
		return looseURILastEmpty.MatchString(string(u))
	}
	return looseURINonEmpty.MatchString(string(u))
}

// PrefixMatch returns true if the receiver URI matches the specified prefix.
func (u URI) PrefixMatch(prefix URI) bool {
	return strings.HasPrefix(string(u), string(prefix))
}

// WildcardMatch returns true if the receiver URI matches the specified
// wildcard pattern.
func (u URI) WildcardMatch(wildcard URI) bool {
	wcParts := strings.Split(string(wildcard), ".")
	parts := strings.Split(string(u), ".")
	// If URI and wildcard have different number of parts, they do not match.
	if len(parts) != len(wcParts) {
		return false
	}
	// Check that all the non-empty parts are the same.
	for i := range wcParts {
		if wcParts[i] != "" && wcParts[i] != parts[i] {
			return false
		}
	}
	return true
}
