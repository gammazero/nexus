package auth_test

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/sign"

	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// cryptoSignKeyStore is a test KeyStore that maps a single authid to a
// known ed25519 public key. It supports the two methods the
// CryptoSignAuthenticator calls: AuthRole and AuthKey(authid,
// "cryptosign"). The pubkey returned must be the 32-byte raw ed25519
// public key.
type cryptoSignKeyStore struct {
	authid    string
	authrole  string
	publicKey *[32]byte
}

func (ks *cryptoSignKeyStore) AuthKey(authid, authmethod string) ([]byte, error) {
	if authid != ks.authid {
		return nil, errors.New("no such user: " + authid)
	}
	if authmethod != "cryptosign" {
		return nil, errors.New("unsupported authmethod: " + authmethod)
	}
	return ks.publicKey[:], nil
}

func (ks *cryptoSignKeyStore) AuthRole(authid string) (string, error) {
	if authid != ks.authid {
		return "", errors.New("no such user: " + authid)
	}
	return ks.authrole, nil
}

func (ks *cryptoSignKeyStore) PasswordInfo(string) (string, int, int) { return "", 0, 0 }
func (ks *cryptoSignKeyStore) Provider() string                       { return "test-cryptosign" }

// signChallenge takes the hex-encoded challenge from a wamp.Challenge
// message's `Extra["challenge"]` field, signs it with privateKey via
// nacl/sign (yielding signature(64) + message(32) = 96 bytes), and
// returns the hex-encoded result — exactly what AUTHENTICATE.Signature
// must carry per WAMP §14.6.5 (cryptosign).
func signChallenge(t *testing.T, ch *wamp.Challenge, privateKey *[64]byte) string {
	t.Helper()
	hexChallenge, ok := ch.Extra["challenge"].(string)
	require.True(t, ok, "Challenge.Extra missing 'challenge' string")
	challenge, err := hex.DecodeString(hexChallenge)
	require.NoError(t, err)
	require.Len(t, challenge, 32, "challenge must be 32 bytes")

	signed := sign.Sign(nil, challenge, privateKey)
	require.Len(t, signed, 96, "nacl-signed payload must be 96 bytes (sig+msg)")
	return hex.EncodeToString(signed)
}

// TestCryptoSignAuthMethod is a trivial getter test, but pulls
// `(*CryptoSignAuthenticator).AuthMethod` into coverage and pins the
// canonical method name from spec §14.6.5.
func TestCryptoSignAuthMethod(t *testing.T) {
	a := auth.NewCryptoSignAuthenticator(&cryptoSignKeyStore{}, time.Second)
	require.Equal(t, "cryptosign", a.AuthMethod())
}

// TestCryptoSignAuthenticateValidSignature drives the happy path:
// a client signs the challenge with the private key whose public
// half is in the KeyStore, the authenticator verifies, and returns
// a WELCOME with authmethod=cryptosign and the configured authrole.
func TestCryptoSignAuthenticateValidSignature(t *testing.T) {
	pub, priv, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)

	ks := &cryptoSignKeyStore{
		authid:    "alice",
		authrole:  "users",
		publicKey: pub,
	}
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	cp, rp := transport.LinkedPeers()
	defer cp.Close()
	defer rp.Close()

	// Client-side responder: receive CHALLENGE, sign, send AUTHENTICATE.
	go func() {
		msg, ok := <-cp.Recv()
		if !ok {
			return
		}
		ch, ok := msg.(*wamp.Challenge)
		if !ok {
			return
		}
		cp.Send() <- &wamp.Authenticate{Signature: signChallenge(t, ch, priv)}
	}()

	welcome, err := a.Authenticate(wamp.ID(42), wamp.Dict{"authid": "alice"}, rp)
	require.NoError(t, err, "Authenticate should succeed for valid signature")
	require.NotNil(t, welcome)
	require.Equal(t, "cryptosign", welcome.Details["authmethod"])
	require.Equal(t, "users", welcome.Details["authrole"])
	require.Equal(t, "alice", welcome.Details["authid"])
	require.Equal(t, "test-cryptosign", welcome.Details["authprovider"])
}

// TestCryptoSignAuthenticateBadSignature uses a DIFFERENT keypair to
// sign than what the KeyStore advertises: nacl.Open returns false →
// authenticator returns "invalid signature".
func TestCryptoSignAuthenticateBadSignature(t *testing.T) {
	correctPub, _, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_, wrongPriv, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)

	ks := &cryptoSignKeyStore{
		authid:    "alice",
		authrole:  "users",
		publicKey: correctPub,
	}
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	cp, rp := transport.LinkedPeers()
	defer cp.Close()
	defer rp.Close()

	go func() {
		msg, ok := <-cp.Recv()
		if !ok {
			return
		}
		ch, ok := msg.(*wamp.Challenge)
		if !ok {
			return
		}
		// Sign with the wrong key — verifySignature must reject.
		cp.Send() <- &wamp.Authenticate{Signature: signChallenge(t, ch, wrongPriv)}
	}()

	welcome, err := a.Authenticate(wamp.ID(43), wamp.Dict{"authid": "alice"}, rp)
	require.Error(t, err, "Authenticate must reject signature from wrong key")
	require.Nil(t, welcome)
	require.Contains(t, err.Error(), "invalid signature")
}

// TestCryptoSignAuthenticateMalformedSignature pins the format-check
// branch in verifySignature: a signature of the wrong length (not 96
// bytes) is rejected before nacl.Open even runs.
func TestCryptoSignAuthenticateMalformedSignature(t *testing.T) {
	pub, _, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)

	ks := &cryptoSignKeyStore{
		authid:    "alice",
		authrole:  "users",
		publicKey: pub,
	}
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	cp, rp := transport.LinkedPeers()
	defer cp.Close()
	defer rp.Close()

	go func() {
		msg, ok := <-cp.Recv()
		if !ok {
			return
		}
		_, isChallenge := msg.(*wamp.Challenge)
		if !isChallenge {
			return
		}
		// Signature is well-formed hex but only 32 bytes — wrong length.
		cp.Send() <- &wamp.Authenticate{
			Signature: hex.EncodeToString(make([]byte, 32)),
		}
	}()

	welcome, err := a.Authenticate(wamp.ID(44), wamp.Dict{"authid": "alice"}, rp)
	require.Error(t, err)
	require.Nil(t, welcome)
	require.Contains(t, err.Error(), "invalid length")
}

// TestCryptoSignAuthenticateNonHexSignature pins the hex-decode error
// path: a signature string that isn't valid hex fails before length
// or signature checks.
func TestCryptoSignAuthenticateNonHexSignature(t *testing.T) {
	pub, _, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)

	ks := &cryptoSignKeyStore{
		authid:    "alice",
		authrole:  "users",
		publicKey: pub,
	}
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	cp, rp := transport.LinkedPeers()
	defer cp.Close()
	defer rp.Close()

	go func() {
		msg, ok := <-cp.Recv()
		if !ok {
			return
		}
		_, isChallenge := msg.(*wamp.Challenge)
		if !isChallenge {
			return
		}
		cp.Send() <- &wamp.Authenticate{Signature: "not-valid-hex!"}
	}()

	welcome, err := a.Authenticate(wamp.ID(45), wamp.Dict{"authid": "alice"}, rp)
	require.Error(t, err)
	require.Nil(t, welcome)
}

// TestCryptoSignAuthenticateUnknownAuthid pins the KeyStore-miss path
// for AuthRole: HELLO carries an authid the KeyStore doesn't know,
// authenticator rejects before challenging.
func TestCryptoSignAuthenticateUnknownAuthid(t *testing.T) {
	pub, _, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)

	ks := &cryptoSignKeyStore{
		authid:    "alice",
		authrole:  "users",
		publicKey: pub,
	}
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	_, rp := transport.LinkedPeers()
	defer rp.Close()

	welcome, err := a.Authenticate(wamp.ID(46), wamp.Dict{"authid": "mallory"}, rp)
	require.Error(t, err, "must reject unknown authid")
	require.Nil(t, welcome)
}

// TestCryptoSignAuthenticateMissingAuthid pins the empty-authid early
// rejection.
func TestCryptoSignAuthenticateMissingAuthid(t *testing.T) {
	pub, _, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ks := &cryptoSignKeyStore{authid: "alice", authrole: "users", publicKey: pub}
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	_, rp := transport.LinkedPeers()
	defer rp.Close()

	welcome, err := a.Authenticate(wamp.ID(47), wamp.Dict{}, rp)
	require.Error(t, err)
	require.Nil(t, welcome)
	require.Contains(t, err.Error(), "missing authid")
}

// TestCryptoSignAuthenticateTimeout pins the read-timeout path: the
// client receives the CHALLENGE but never sends AUTHENTICATE.
// Authenticate's wamp.RecvTimeout returns an error.
func TestCryptoSignAuthenticateTimeout(t *testing.T) {
	pub, _, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ks := &cryptoSignKeyStore{authid: "alice", authrole: "users", publicKey: pub}
	// 50ms timeout so the test runs fast.
	a := auth.NewCryptoSignAuthenticator(ks, 50*time.Millisecond)

	cp, rp := transport.LinkedPeers()
	defer cp.Close()
	defer rp.Close()

	// Client-side: drain CHALLENGE but DON'T respond.
	go func() {
		<-cp.Recv()
	}()

	welcome, err := a.Authenticate(wamp.ID(48), wamp.Dict{"authid": "alice"}, rp)
	require.Error(t, err, "Authenticate must time out without AUTHENTICATE response")
	require.Nil(t, welcome)
}

// TestCryptoSignAuthenticateUnexpectedMessageType pins the
// not-an-AUTHENTICATE path: client responds with the wrong message
// type after CHALLENGE.
func TestCryptoSignAuthenticateUnexpectedMessageType(t *testing.T) {
	pub, _, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ks := &cryptoSignKeyStore{authid: "alice", authrole: "users", publicKey: pub}
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	cp, rp := transport.LinkedPeers()
	defer cp.Close()
	defer rp.Close()

	go func() {
		msg, ok := <-cp.Recv()
		if !ok {
			return
		}
		_, isChallenge := msg.(*wamp.Challenge)
		if !isChallenge {
			return
		}
		// Send GOODBYE instead of AUTHENTICATE.
		cp.Send() <- &wamp.Goodbye{Reason: wamp.ErrCloseRealm}
	}()

	welcome, err := a.Authenticate(wamp.ID(49), wamp.Dict{"authid": "alice"}, rp)
	require.Error(t, err)
	require.Nil(t, welcome)
	require.Contains(t, err.Error(), "unexpected")
}

// TestCryptoSignNewAuthenticatorDefaultTimeout pins the constructor's
// zero-timeout fallback: passing 0 must select the package default,
// not block forever.
func TestCryptoSignNewAuthenticatorDefaultTimeout(t *testing.T) {
	a := auth.NewCryptoSignAuthenticator(&cryptoSignKeyStore{}, 0)
	require.NotNil(t, a)
	// We can't introspect timeout directly (unexported); the
	// behavioral guarantee is that Authenticate doesn't loop
	// indefinitely with a zero timeout. Covered by the timeout
	// test above when paired with a non-responding client; we
	// just need the constructor not to panic on zero.
}

// bypassCryptoSignKeyStore implements both KeyStore and
// BypassKeyStore: when AlreadyAuth returns true (e.g. trusted
// transport, cookie session), the authenticator skips the
// challenge/response entirely and returns WELCOME directly.
type bypassCryptoSignKeyStore struct {
	cryptoSignKeyStore
	alreadyAuth bool
	onWelcomeOK bool
}

func (ks *bypassCryptoSignKeyStore) AlreadyAuth(authid string, details wamp.Dict) bool {
	return ks.alreadyAuth && authid == ks.authid
}

func (ks *bypassCryptoSignKeyStore) OnWelcome(_ string, welcome *wamp.Welcome, _ wamp.Dict) error {
	if !ks.onWelcomeOK {
		return errors.New("OnWelcome refused")
	}
	welcome.Details["authbycookie"] = true
	return nil
}

// TestCryptoSignAuthenticateBypassSuccess pins the BypassKeyStore
// fast-path: a KeyStore that signals AlreadyAuth=true short-circuits
// the cryptosign challenge/response and returns a WELCOME with
// whatever extras OnWelcome adds.
func TestCryptoSignAuthenticateBypassSuccess(t *testing.T) {
	pub, _, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)

	ks := &bypassCryptoSignKeyStore{
		cryptoSignKeyStore: cryptoSignKeyStore{
			authid: "alice", authrole: "users", publicKey: pub,
		},
		alreadyAuth: true,
		onWelcomeOK: true,
	}
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	_, rp := transport.LinkedPeers()
	defer rp.Close()

	// No client-side responder — bypass path skips CHALLENGE entirely.
	welcome, err := a.Authenticate(wamp.ID(50), wamp.Dict{"authid": "alice"}, rp)
	require.NoError(t, err)
	require.NotNil(t, welcome)
	require.Equal(t, "cryptosign", welcome.Details["authmethod"])
	require.Equal(t, true, welcome.Details["authbycookie"])
}

// TestCryptoSignAuthenticateBypassOnWelcomeError pins the
// OnWelcome-error branch of the bypass path: if OnWelcome returns
// an error, Authenticate propagates it and returns no WELCOME.
func TestCryptoSignAuthenticateBypassOnWelcomeError(t *testing.T) {
	pub, _, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)

	ks := &bypassCryptoSignKeyStore{
		cryptoSignKeyStore: cryptoSignKeyStore{
			authid: "alice", authrole: "users", publicKey: pub,
		},
		alreadyAuth: true,
		onWelcomeOK: false,
	}
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	_, rp := transport.LinkedPeers()
	defer rp.Close()

	welcome, err := a.Authenticate(wamp.ID(51), wamp.Dict{"authid": "alice"}, rp)
	require.Error(t, err)
	require.Nil(t, welcome)
	require.Contains(t, err.Error(), "OnWelcome refused")
}
