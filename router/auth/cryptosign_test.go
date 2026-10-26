package auth_test

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/sign"

	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// cryptoSignKeyStore is a test KeyStore that knows a single authid, whose
// cryptosign key is publicKey.
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

// newCryptoSignKeyStore returns a keystore for the authid "alice", with a new
// key pair, and the private key of that pair.
func newCryptoSignKeyStore(t *testing.T) (*cryptoSignKeyStore, *[64]byte) {
	t.Helper()
	pub, priv, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ks := &cryptoSignKeyStore{
		authid:    "alice",
		authrole:  "users",
		publicKey: pub,
	}
	return ks, priv
}

// signChallenge signs the challenge in ch with privateKey, and returns the
// signature that AUTHENTICATE carries: the hex encoding of the 64-byte
// signature followed by the 32-byte challenge.
func signChallenge(t *testing.T, ch *wamp.Challenge, privateKey *[64]byte) string {
	t.Helper()
	hexChallenge, ok := ch.Extra["challenge"].(string)
	require.True(t, ok, "CHALLENGE missing challenge string")
	challenge, err := hex.DecodeString(hexChallenge)
	require.NoError(t, err)
	require.Len(t, challenge, 32)

	signed := sign.Sign(nil, challenge, privateKey)
	require.Len(t, signed, 96)
	return hex.EncodeToString(signed)
}

// authenticate runs a.Authenticate for the client details. If respond is not
// nil, the client waits for the CHALLENGE and answers it with the message that
// respond returns. Otherwise the client never answers.
func authenticate(t *testing.T, a *auth.CryptoSignAuthenticator, details wamp.Dict, respond func(*wamp.Challenge) wamp.Message) (*wamp.Welcome, error) { //nolint:lll
	t.Helper()
	cp, rp := transport.LinkedPeers()
	defer cp.Close()
	defer rp.Close()

	type result struct {
		welcome *wamp.Welcome
		err     error
	}
	done := make(chan result, 1)
	go func() {
		welcome, err := a.Authenticate(wamp.ID(42), details, rp)
		done <- result{welcome, err}
	}()

	if respond != nil {
		msg, err := wamp.RecvTimeout(cp, time.Second)
		require.NoError(t, err, "no CHALLENGE")
		ch, ok := msg.(*wamp.Challenge)
		require.True(t, ok, "expected CHALLENGE, got %T", msg)
		require.Equal(t, "cryptosign", ch.AuthMethod)
		cp.Send() <- respond(ch)
	}

	res := <-done
	return res.welcome, res.err
}

func TestCryptoSignAuthMethod(t *testing.T) {
	a := auth.NewCryptoSignAuthenticator(&cryptoSignKeyStore{}, time.Second)
	require.Equal(t, "cryptosign", a.AuthMethod())
}

// TestCryptoSignAuthenticateValidSignature checks that a client that signs the
// challenge with the private key, whose public key is in the keystore, is
// welcomed with the keystore's authrole and provider.
func TestCryptoSignAuthenticateValidSignature(t *testing.T) {
	ks, priv := newCryptoSignKeyStore(t)
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	welcome, err := authenticate(t, a, wamp.Dict{"authid": "alice"},
		func(ch *wamp.Challenge) wamp.Message {
			return &wamp.Authenticate{Signature: signChallenge(t, ch, priv)}
		})
	require.NoError(t, err)
	require.NotNil(t, welcome)
	require.Equal(t, "cryptosign", welcome.Details["authmethod"])
	require.Equal(t, "users", welcome.Details["authrole"])
	require.Equal(t, "alice", welcome.Details["authid"])
	require.Equal(t, "test-cryptosign", welcome.Details["authprovider"])
}

// TestCryptoSignAuthenticateBadSignature checks that a challenge signed with a
// key other than the one in the keystore is rejected.
func TestCryptoSignAuthenticateBadSignature(t *testing.T) {
	ks, _ := newCryptoSignKeyStore(t)
	_, wrongPriv, err := sign.GenerateKey(rand.Reader)
	require.NoError(t, err)
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	welcome, err := authenticate(t, a, wamp.Dict{"authid": "alice"},
		func(ch *wamp.Challenge) wamp.Message {
			return &wamp.Authenticate{Signature: signChallenge(t, ch, wrongPriv)}
		})
	require.ErrorContains(t, err, "invalid signature")
	require.Nil(t, welcome)
}

// TestCryptoSignAuthenticateMalformedSignature checks that a signature that is
// not 96 bytes long is rejected.
func TestCryptoSignAuthenticateMalformedSignature(t *testing.T) {
	ks, _ := newCryptoSignKeyStore(t)
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	welcome, err := authenticate(t, a, wamp.Dict{"authid": "alice"},
		func(*wamp.Challenge) wamp.Message {
			return &wamp.Authenticate{Signature: hex.EncodeToString(make([]byte, 32))}
		})
	require.ErrorContains(t, err, "invalid length")
	require.Nil(t, welcome)
}

// TestCryptoSignAuthenticateNonHexSignature checks that a signature that is
// not hex is rejected.
func TestCryptoSignAuthenticateNonHexSignature(t *testing.T) {
	ks, _ := newCryptoSignKeyStore(t)
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	welcome, err := authenticate(t, a, wamp.Dict{"authid": "alice"},
		func(*wamp.Challenge) wamp.Message {
			return &wamp.Authenticate{Signature: "not-valid-hex!"}
		})
	var hexErr hex.InvalidByteError
	require.ErrorAs(t, err, &hexErr)
	require.Nil(t, welcome)
}

// TestCryptoSignAuthenticateUnexpectedMessageType checks that a client that
// answers the CHALLENGE with something other than AUTHENTICATE is rejected.
func TestCryptoSignAuthenticateUnexpectedMessageType(t *testing.T) {
	ks, _ := newCryptoSignKeyStore(t)
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	welcome, err := authenticate(t, a, wamp.Dict{"authid": "alice"},
		func(*wamp.Challenge) wamp.Message {
			return &wamp.Goodbye{Reason: wamp.ErrCloseRealm}
		})
	require.ErrorContains(t, err, "unexpected")
	require.Nil(t, welcome)
}

// TestCryptoSignAuthenticateUnknownAuthid checks that an authid that the
// keystore does not know is rejected without a CHALLENGE.
func TestCryptoSignAuthenticateUnknownAuthid(t *testing.T) {
	ks, _ := newCryptoSignKeyStore(t)
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	welcome, err := authenticate(t, a, wamp.Dict{"authid": "mallory"}, nil)
	require.ErrorContains(t, err, "no such user")
	require.Nil(t, welcome)
}

// TestCryptoSignAuthenticateMissingAuthid checks that a client without an
// authid is rejected without a CHALLENGE.
func TestCryptoSignAuthenticateMissingAuthid(t *testing.T) {
	ks, _ := newCryptoSignKeyStore(t)
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	welcome, err := authenticate(t, a, wamp.Dict{}, nil)
	require.ErrorContains(t, err, "missing authid")
	require.Nil(t, welcome)
}

// TestCryptoSignAuthenticateTimeout checks that Authenticate gives up when the
// client does not answer the CHALLENGE within the timeout.
func TestCryptoSignAuthenticateTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ks, _ := newCryptoSignKeyStore(t)
		a := auth.NewCryptoSignAuthenticator(ks, time.Second)

		start := time.Now()
		welcome, err := authenticate(t, a, wamp.Dict{"authid": "alice"}, nil)
		require.Error(t, err)
		require.Nil(t, welcome)
		require.Equal(t, time.Second, time.Since(start))
	})
}

// TestCryptoSignNewAuthenticatorDefaultTimeout checks that a zero timeout
// selects the default timeout of one minute, so that a client that answers
// after 30 seconds is still welcomed.
func TestCryptoSignNewAuthenticatorDefaultTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ks, priv := newCryptoSignKeyStore(t)
		a := auth.NewCryptoSignAuthenticator(ks, 0)

		welcome, err := authenticate(t, a, wamp.Dict{"authid": "alice"},
			func(ch *wamp.Challenge) wamp.Message {
				time.Sleep(30 * time.Second)
				return &wamp.Authenticate{Signature: signChallenge(t, ch, priv)}
			})
		require.NoError(t, err)
		require.NotNil(t, welcome)
	})
}

// bypassCryptoSignKeyStore is a BypassKeyStore that, when alreadyAuth is set,
// reports the client as already authenticated, so that Authenticate welcomes
// it without a CHALLENGE.
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

// TestCryptoSignAuthenticateBypassSuccess checks that a client the keystore
// reports as already authenticated is welcomed without a CHALLENGE, with the
// details that OnWelcome adds.
func TestCryptoSignAuthenticateBypassSuccess(t *testing.T) {
	ks, _ := newCryptoSignKeyStore(t)
	bks := &bypassCryptoSignKeyStore{
		cryptoSignKeyStore: *ks,
		alreadyAuth:        true,
		onWelcomeOK:        true,
	}
	a := auth.NewCryptoSignAuthenticator(bks, time.Second)

	welcome, err := authenticate(t, a, wamp.Dict{"authid": "alice"}, nil)
	require.NoError(t, err)
	require.NotNil(t, welcome)
	require.Equal(t, "cryptosign", welcome.Details["authmethod"])
	require.Equal(t, true, welcome.Details["authbycookie"])
}

// TestCryptoSignAuthenticateBypassOnWelcomeError checks that an error from
// OnWelcome rejects a client the keystore reports as already authenticated.
func TestCryptoSignAuthenticateBypassOnWelcomeError(t *testing.T) {
	ks, _ := newCryptoSignKeyStore(t)
	bks := &bypassCryptoSignKeyStore{
		cryptoSignKeyStore: *ks,
		alreadyAuth:        true,
		onWelcomeOK:        false,
	}
	a := auth.NewCryptoSignAuthenticator(bks, time.Second)

	welcome, err := authenticate(t, a, wamp.Dict{"authid": "alice"}, nil)
	require.ErrorContains(t, err, "OnWelcome refused")
	require.Nil(t, welcome)
}

// TestCryptoSignRejectsReplay checks that a signature captured from one
// authentication is not accepted in another, and that a signature by the
// client's key over any message other than the challenge is rejected. The
// router used to accept any valid signature by the client's key, whatever
// message it signed.
func TestCryptoSignRejectsReplay(t *testing.T) {
	ks, priv := newCryptoSignKeyStore(t)
	a := auth.NewCryptoSignAuthenticator(ks, time.Second)

	var captured string
	_, err := authenticate(t, a, wamp.Dict{"authid": "alice"},
		func(ch *wamp.Challenge) wamp.Message {
			captured = signChallenge(t, ch, priv)
			return &wamp.Authenticate{Signature: captured}
		})
	require.NoError(t, err)

	welcome, err := authenticate(t, a, wamp.Dict{"authid": "alice"},
		func(*wamp.Challenge) wamp.Message {
			return &wamp.Authenticate{Signature: captured}
		})
	require.ErrorContains(t, err, "invalid signature", "replayed signature accepted")
	require.Nil(t, welcome)

	other := make([]byte, 32)
	_, err = rand.Read(other)
	require.NoError(t, err)
	welcome, err = authenticate(t, a, wamp.Dict{"authid": "alice"},
		func(*wamp.Challenge) wamp.Message {
			return &wamp.Authenticate{Signature: hex.EncodeToString(sign.Sign(nil, other, priv))}
		})
	require.ErrorContains(t, err, "invalid signature", "signature over another message accepted")
	require.Nil(t, welcome)
}
