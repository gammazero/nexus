package router

import (
	"strings"

	"github.com/gammazero/nexus/v3/wamp"
)

// PublishFilter is an interface to check whether a publication should be sent
// to a specific session
type PublishFilter interface {
	Allowed(sess *wamp.Session) bool
}

// FilterFactory is a function which creates a PublishFilter from a publication
type FilterFactory func(msg *wamp.Publish) PublishFilter

type simplePublishFilter struct {
	blIDs []wamp.ID
	wlIDs []wamp.ID
	blMap map[string][]string
	wlMap map[string][]string
}

// NewSimplePublishFilter gets any blacklists and whitelists included
// in a PUBLISH message.  If there are no filters defined by the
// PUBLISH message, then nil is returned.
func NewSimplePublishFilter(msg *wamp.Publish) PublishFilter {
	const (
		blacklistPrefix = "exclude_"
		whitelistPrefix = "eligible_"
	)

	if len(msg.Options) == 0 {
		return nil
	}

	var blIDs []wamp.ID
	if blacklistFilter, ok := msg.Options[wamp.BlacklistKey]; ok {
		if blacklist, ok := wamp.AsList(blacklistFilter); ok {
			for i := range blacklist {
				if blVal, ok := wamp.AsID(blacklist[i]); ok {
					blIDs = append(blIDs, blVal)
				}
			}
		}
	}

	var wlIDs []wamp.ID
	if whitelistFilter, ok := msg.Options[wamp.WhitelistKey]; ok {
		if whitelist, ok := wamp.AsList(whitelistFilter); ok {
			for i := range whitelist {
				if wlID, ok := wamp.AsID(whitelist[i]); ok {
					wlIDs = append(wlIDs, wlID)
				}
			}
		}
	}

	getAttrMap := func(prefix string) map[string][]string {
		var attrMap map[string][]string
		for k, values := range msg.Options {
			if !strings.HasPrefix(k, prefix) {
				continue
			}
			if vals, ok := wamp.AsList(values); ok {
				vallist := make([]string, 0, len(vals))
				for i := range vals {
					if val, ok := wamp.AsString(vals[i]); ok && val != "" {
						vallist = append(vallist, val)
					}
				}
				if len(vallist) != 0 {
					attrName := k[len(prefix):]
					if attrMap == nil {
						attrMap = map[string][]string{}
					}
					attrMap[attrName] = vallist
				}
			}
		}
		return attrMap
	}

	blMap := getAttrMap(blacklistPrefix)
	wlMap := getAttrMap(whitelistPrefix)

	if blIDs == nil && wlIDs == nil && blMap == nil && wlMap == nil {
		return nil
	}
	return &simplePublishFilter{blIDs, wlIDs, blMap, wlMap}
}

// Allowed determines if a message is allowed to be published to a
// subscriber, by looking at any blacklists and whitelists provided
// with the publish message.
//
// To receive a published event, the subscriber session must not have any
// values that appear in a blacklist, and must have a value from each
// whitelist.
func (f *simplePublishFilter) Allowed(sub *wamp.Session) bool {
	// Check each blacklisted ID to see if session ID is blacklisted.
	for i := range f.blIDs {
		if f.blIDs[i] == sub.ID {
			return false
		}
	}

	var eligible bool
	// If session ID whitelist given, make sure session ID is in whitelist.
	if len(f.wlIDs) != 0 {
		for i := range f.wlIDs {
			if f.wlIDs[i] == sub.ID {
				eligible = true
				break
			}
		}
		if !eligible {
			return false
		}
	}

	// Check blacklists to see if session has a value in any blacklist.
	details := sub.Details
	for attr, vals := range f.blMap {
		// Get the session attribute value to compare with blacklist.
		sessAttr, _ := wamp.AsString(details[attr])
		if sessAttr == "" {
			continue
		}
		// Check each blacklisted value to see if session attribute is one.
		for i := range vals {
			if vals[i] == sessAttr {
				// Session has blacklisted attribute value.
				return false
			}
		}
	}

	// Check whitelists to make sure session has value in each whitelist.
	for attr, vals := range f.wlMap {
		// Get the session attribute value to compare with whitelist.
		sessAttr, _ := wamp.AsString(details[attr])
		if sessAttr == "" {
			// Session does not have whitelisted value, so deny.
			return false
		}
		eligible = false
		// Check all whitelisted values to see is session attribute is one.
		for i := range vals {
			if vals[i] == sessAttr {
				// Session has whitelisted attribute value.
				eligible = true
				break
			}
		}
		// If session attribute value no found in whitelist, then deny.
		if !eligible {
			return false
		}
	}
	return true
}
