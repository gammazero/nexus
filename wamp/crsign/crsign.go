// Package crsign provides functionality for signing challenge data in
// challenge-response authentication.
package crsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"hash"

	"github.com/gammazero/nexus/v3/wamp"
	"golang.org/x/crypto/pbkdf2"
)

// SignChallenge computes the HMAC-SHA256, using the given key, over the
// challenge string, and returns the result as a base64-encoded string.
func SignChallenge(ch string, key []byte) string {
	return base64.StdEncoding.EncodeToString(SignChallengeBytes(ch, key))
}

// SignChallenge computes the HMAC-SHA256, using the given key, over the
// challenge string, and returns the result.
func SignChallengeBytes(ch string, key []byte) []byte {
	sig := hmac.New(sha256.New, key)
	sig.Write([]byte(ch))
	return sig.Sum(nil)
}

// VerifySignature compares a signature to a signature that the computed over
// the given chalenge string using the key.  The signature is a base64-encoded
// string, generally presented by a client, and the challenge string and key
// are used to compute the expected HMAC signature.  If these are the same,
// then true is returned.
func VerifySignature(sig, chal string, key []byte) bool {
	sigBytes, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return false
	}
	return hmac.Equal(sigBytes, SignChallengeBytes(chal, key))
}

// Default parameters for PBKDF2.  These are used if not present in CHALLENGE.
const (
	defaultIters  = 1000
	defaultKeyLen = 32
)

// RespondChallenge is used by clients to sign the challenge string contained
// in the CHALLENGE message using the given password.  If the CHALLENGE message
// contains salting information, then a derived key is computed using PBKDF2,
// and that derived key is used to sign the challenge string.  If there is no
// salt, then no derived key is computed and the raw password is used to sign
// the challenge, which is identical to calling SignChallenge().
//
// Set h to nil to use default hash sha256.  This is provided in case the
// server-side PBKDF2 uses a different hash algorithm.
//
// Example Client Use:
//
//     func clientCRAuthFunc(c *wamp.Challenge) (string, wamp.Dict) {
//         // Get user password and return signature.
//         password := AskUserPassoword()
//         return RespondChallenge(password, c, nil), wamp.Dict{}
//     }
//
//     // Configure and create new client.
//     cfg := client.Config{
//         ...
//         AuthHandlers: map[string]client.AuthFunc{
//             "wampcra": clientCRAuthFunc,
//         },
//     }
//     cli, err = client.ConnectNet(routerAddr, cfg)
//
func RespondChallenge(pass string, c *wamp.Challenge, h func() hash.Hash) string {
	ch, _ := wamp.AsString(c.Extra["challenge"])
	// If the client needed to lookup a user's key, this would require decoding
	// the JSON-encoded challenge string and getting the authid.  For this
	// example assume that client only operates as one user and knows the key
	// to use.
	saltStr, _ := wamp.AsString(c.Extra["salt"])
	// If no salt given, use raw password as key.
	if saltStr == "" {
		return SignChallenge(ch, []byte(pass))
	}

	// If salting info give, then compute a derived key using PBKDF2.
	salt := []byte(saltStr)
	iters, _ := wamp.AsInt64(c.Extra["iterations"])
	keylen, _ := wamp.AsInt64(c.Extra["keylen"])

	if iters == 0 {
		iters = defaultIters
	}
	if keylen == 0 {
		keylen = defaultKeyLen
	}
	if h == nil {
		h = sha256.New
	}
	// Compute derived key.
	dk := pbkdf2.Key([]byte(pass), salt, int(iters), int(keylen), h)

	// Sign challenge using derived key.
	return SignChallenge(ch, dk)
}
