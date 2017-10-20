package crsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// SignChallenge computes the HMAC-SHA256 over the challenge string, and
// returns the result as a base64-encoded string.
func SignChallenge(ch string, key []byte) string {
	sig := hmac.New(sha256.New, key)
	sig.Write([]byte(ch))
	return base64.StdEncoding.EncodeToString(sig.Sum(nil))
}
