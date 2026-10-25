package router

import (
	"fmt"
	"math"
	"reflect"
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

// sameDetailValue reports whether a and b are the same session detail value.
// Maps, slices, pointers and channels are the same only if they are the same
// object, whatever they hold, so that comparing them is cheap. Functions are
// the same if they have the same code. Other values are the same if they are
// equal, and a NaN is the same as itself.
func sameDetailValue(a, b any) bool {
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	if !va.IsValid() || !vb.IsValid() {
		return va.IsValid() == vb.IsValid()
	}
	if va.Type() != vb.Type() {
		return false
	}
	switch va.Kind() {
	case reflect.Map, reflect.Pointer, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return va.UnsafePointer() == vb.UnsafePointer()
	case reflect.Slice:
		return va.UnsafePointer() == vb.UnsafePointer() && va.Len() == vb.Len()
	case reflect.Float32, reflect.Float64:
		return math.Float64bits(va.Float()) == math.Float64bits(vb.Float())
	}
	return va.Comparable() && va.Equal(vb)
}
