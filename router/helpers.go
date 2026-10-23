package router

import (
	"fmt"
	"strings"

	"github.com/gammazero/nexus/v3/wamp"
)

// maxForwardedForLog is the most bytes of X-Forwarded-For headers to log.
const maxForwardedForLog = 256

// peerDescription identifies the remote end of a connection in log lines.
// forwardedFor holds the X-Forwarded-For headers of the request, if any. They
// are set by the client, or by a proxy in front of the router, so they are
// quoted and truncated in case the client wrote them.
func peerDescription(remoteAddr string, forwardedFor []string) string {
	if remoteAddr == "" {
		remoteAddr = "unknown"
	}
	if len(forwardedFor) == 0 {
		return remoteAddr
	}
	fwd := strings.Join(forwardedFor, ", ")
	if len(fwd) > maxForwardedForLog {
		fwd = fwd[:maxForwardedForLog] + "..."
	}
	return fmt.Sprintf("%s, X-Forwarded-For %q", remoteAddr, fwd)
}

func pptOptionsToDetails(options wamp.Dict, details wamp.Dict) {
	details[wamp.OptPPTScheme] = options[wamp.OptPPTScheme].(string)
	if val, ok := options[wamp.OptPPTSerializer]; ok {
		details[wamp.OptPPTSerializer] = val.(string)
	}
	if val, ok := options[wamp.OptPPTCipher]; ok {
		details[wamp.OptPPTCipher] = val.(string)
	}
	if val, ok := options[wamp.OptPPTKeyId]; ok {
		details[wamp.OptPPTKeyId] = val.(string)
	}
}

// copyDetails returns a deep copy of session details. Nested dicts and lists
// are copied too, so that changing the copy, even in place, does not change
// details. Other values, such as pointers, are shared.
func copyDetails(details wamp.Dict) wamp.Dict {
	if details == nil {
		return nil
	}
	c := make(wamp.Dict, len(details))
	for k, v := range details {
		c[k] = copyDetailValue(v)
	}
	return c
}

func copyDetailValue(v any) any {
	switch v := v.(type) {
	case wamp.Dict:
		return copyDetails(v)
	case map[string]any:
		return map[string]any(copyDetails(v))
	case wamp.List:
		return copyDetailList(v)
	case []any:
		return []any(copyDetailList(v))
	default:
		return v
	}
}

func copyDetailList(l wamp.List) wamp.List {
	if l == nil {
		return nil
	}
	c := make(wamp.List, len(l))
	for i, v := range l {
		c[i] = copyDetailValue(v)
	}
	return c
}
