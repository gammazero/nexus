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
