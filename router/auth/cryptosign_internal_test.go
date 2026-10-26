package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// cryptosignTestVectors are the WAMP-Cryptosign test vectors without a channel
// ID from the WAMP spec, section 13.4.1.3.
var cryptosignTestVectors = []struct {
	privateKey, challenge, signature string
}{
	{"4d57d97a68f555696620a6d849c0ce582568518d729eb753dc7c732de2804510",
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"b32675b221f08593213737bef8240e7c15228b07028e19595294678c90d11c0cae80a357331bfc5cc9fb71081464e6e75013517c2cf067ad566a6b7b728e5d03ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
	{"d511fe78e23934b3dadb52fcd022974b80bd92bccc7c5cf404e46cc0a8a2f5cd",
		"b26c1f87c13fc1da14997f1b5a71995dff8fbe0a62fae8473c7bdbd05bfb607d",
		"d4209ad10d5aff6bfbc009d7e924795de138a63515efc7afc6b01b7fe5201372190374886a70207b042294af5bd64ce725cd8dceb344e6d11c09d1aaaf4d660fb26c1f87c13fc1da14997f1b5a71995dff8fbe0a62fae8473c7bdbd05bfb607d"},
	{"6e1fde9cf9e2359a87420b65a87dc0c66136e66945196ba2475990d8a0c3a25b",
		"b05e6b8ad4d69abf74aa3be3c0ee40ae07d66e1895b9ab09285a2f1192d562d2",
		"7beb282184baadd08f166f16dd683b39cab53816ed81e6955def951cb2ddad1ec184e206746fd82bda075af03711d3d5658fc84a76196b0fa8d1ebc92ef9f30bb05e6b8ad4d69abf74aa3be3c0ee40ae07d66e1895b9ab09285a2f1192d562d2"},
}

// TestCryptosignTestVectors checks verifySignature against the spec's test
// vectors, and that each signature is rejected for any other challenge.
func TestCryptosignTestVectors(t *testing.T) {
	var cr CryptoSignAuthenticator
	for i, tv := range cryptosignTestVectors {
		seed, err := hex.DecodeString(tv.privateKey)
		require.NoError(t, err)
		pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		challenge, err := hex.DecodeString(tv.challenge)
		require.NoError(t, err)

		ok, err := cr.verifySignature(tv.signature, pub, challenge)
		require.NoError(t, err)
		require.True(t, ok, "test vector %d rejected", i)

		other, err := hex.DecodeString(cryptosignTestVectors[(i+1)%len(cryptosignTestVectors)].challenge)
		require.NoError(t, err)
		ok, err = cr.verifySignature(tv.signature, pub, other)
		require.NoError(t, err)
		require.False(t, ok, "test vector %d accepted for another challenge", i)
	}
}
