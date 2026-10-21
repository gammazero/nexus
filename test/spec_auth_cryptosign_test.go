package test_test

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/sign"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/wamp"
)

// cryptosignKeyStore is a minimal KeyStore for the cryptosign-auth tests.
// It stores a single ed25519-style public key keyed by authid.
type cryptosignKeyStore struct {
	authid   string
	pubkey   [32]byte
	authrole string
}

func (k *cryptosignKeyStore) AuthKey(authid, _ string) ([]byte, error) {
	if authid != k.authid {
		return nil, errors.New("no such user: " + authid)
	}
	return k.pubkey[:], nil
}

func (k *cryptosignKeyStore) AuthRole(authid string) (string, error) {
	if authid != k.authid {
		return "", errors.New("no such user: " + authid)
	}
	return k.authrole, nil
}

func (k *cryptosignKeyStore) PasswordInfo(string) (string, int, int) { return "", 0, 0 }
func (k *cryptosignKeyStore) Provider() string                       { return "test-cryptosign" }

// newCryptosignRouter spins up a dedicated router with a cryptosign realm
// for use by these tests. Returns the router and the private key.
// Close is registered as a t.Cleanup so it runs AFTER client cleanups (LIFO).
func newCryptosignRouter(t *testing.T, authid, authrole string) (router.Router, *[64]byte) {
	t.Helper()

	pub, priv, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)

	ks := &cryptosignKeyStore{authid: authid, pubkey: *pub, authrole: authrole}
	csAuth := auth.NewCryptoSignAuthenticator(ks, time.Second)

	cfg := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			{
				URI:              "nexus.test.cryptosign",
				StrictURI:        false,
				AnonymousAuth:    false,
				Authenticators:   []auth.Authenticator{csAuth},
				RequireLocalAuth: true,
			},
		},
	}
	r, err := router.NewRouter(cfg, rtrLogger)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })
	return r, priv
}

// signCryptosignChallenge takes the hex-encoded challenge string from the
// CHALLENGE message and returns the hex-encoded signed message expected in
// the AUTHENTICATE response (per the router's verifySignature contract).
func signCryptosignChallenge(t *testing.T, c *wamp.Challenge, priv *[64]byte) string {
	t.Helper()
	chHex, _ := wamp.AsString(c.Extra["challenge"])
	chBytes, err := hex.DecodeString(chHex)
	require.NoError(t, err)
	signed := sign.Sign(nil, chBytes, priv)
	return hex.EncodeToString(signed)
}

// TestSpecAuthCryptoSignSucceeds exercises the cryptosign happy path
// (spec §14.6.5). Pin: previously untested in the integration suite.
func TestSpecAuthCryptoSignSucceeds(t *testing.T) {
	if scheme != "" {
		t.Skip("dedicated router; runs only on local in-process scheme")
	}
	checkGoLeaks(t)

	const (
		testAuthid   = "alice"
		testAuthrole = "admin"
	)
	r, priv := newCryptosignRouter(t, testAuthid, testAuthrole)

	cfg := client.Config{
		Realm: "nexus.test.cryptosign",
		HelloDetails: wamp.Dict{
			"authid":      testAuthid,
			"authmethods": wamp.List{"cryptosign"},
		},
		AuthHandlers: map[string]client.AuthFunc{
			"cryptosign": func(c *wamp.Challenge) (string, wamp.Dict) {
				return signCryptosignChallenge(t, c, priv), wamp.Dict{}
			},
		},
		ResponseTimeout: time.Second,
		Logger:          cliLogger,
	}
	cli, err := client.ConnectLocal(r, cfg)
	require.NoError(t, err)
	defer cli.Close()

	authmethod, _ := wamp.AsString(cli.RealmDetails()["authmethod"])
	require.Equal(t, "cryptosign", authmethod)
	authrole, _ := wamp.AsString(cli.RealmDetails()["authrole"])
	require.Equal(t, testAuthrole, authrole)
}

// TestSpecAuthCryptoSignBadSignature verifies an invalid signature is
// rejected with an authentication failure.
func TestSpecAuthCryptoSignBadSignature(t *testing.T) {
	if scheme != "" {
		t.Skip("dedicated router; runs only on local in-process scheme")
	}
	checkGoLeaks(t)

	const testAuthid = "alice"
	r, _ := newCryptosignRouter(t, testAuthid, "admin")

	// Generate a different keypair so signatures won't verify.
	_, otherPriv, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)

	cfg := client.Config{
		Realm: "nexus.test.cryptosign",
		HelloDetails: wamp.Dict{
			"authid":      testAuthid,
			"authmethods": wamp.List{"cryptosign"},
		},
		AuthHandlers: map[string]client.AuthFunc{
			"cryptosign": func(c *wamp.Challenge) (string, wamp.Dict) {
				return signCryptosignChallenge(t, c, otherPriv), wamp.Dict{}
			},
		},
		ResponseTimeout: time.Second,
		Logger:          cliLogger,
	}
	_, err = client.ConnectLocal(r, cfg)
	require.Error(t, err, "expected cryptosign auth to fail with mismatched key")
}

// TestSpecAuthCryptoSignUnknownUser verifies that an authid not in the
// keystore is rejected before the challenge is ever sent.
func TestSpecAuthCryptoSignUnknownUser(t *testing.T) {
	if scheme != "" {
		t.Skip("dedicated router; runs only on local in-process scheme")
	}
	checkGoLeaks(t)

	r, priv := newCryptosignRouter(t, "alice", "admin")

	cfg := client.Config{
		Realm: "nexus.test.cryptosign",
		HelloDetails: wamp.Dict{
			"authid":      "no-such-user",
			"authmethods": wamp.List{"cryptosign"},
		},
		AuthHandlers: map[string]client.AuthFunc{
			"cryptosign": func(c *wamp.Challenge) (string, wamp.Dict) {
				return signCryptosignChallenge(t, c, priv), wamp.Dict{}
			},
		},
		ResponseTimeout: time.Second,
		Logger:          cliLogger,
	}
	_, err := client.ConnectLocal(r, cfg)
	require.Error(t, err, "expected cryptosign auth to fail for unknown authid")
}
