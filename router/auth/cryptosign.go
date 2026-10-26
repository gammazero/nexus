package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/nacl/sign"

	"github.com/gammazero/nexus/v3/wamp"
)

type CryptoSignAuthenticator struct {
	keyStore KeyStore
	timeout  time.Duration
}

func NewCryptoSignAuthenticator(keyStore KeyStore, timeout time.Duration) *CryptoSignAuthenticator {
	if timeout == 0 {
		timeout = defaultCRAuthTimeout
	}
	return &CryptoSignAuthenticator{
		keyStore: keyStore,
		timeout:  timeout,
	}
}

func (cr *CryptoSignAuthenticator) AuthMethod() string { return "cryptosign" }

func (cr *CryptoSignAuthenticator) Authenticate(sid wamp.ID, details wamp.Dict, client wamp.Peer) (*wamp.Welcome, error) { //nolint:lll
	authid, _ := wamp.AsString(details["authid"])
	if authid == "" {
		return nil, errors.New("missing authid")
	}

	authrole, err := cr.keyStore.AuthRole(authid)
	if err != nil {
		return nil, err
	}

	ks, ok := cr.keyStore.(BypassKeyStore)
	if ok {
		if ks.AlreadyAuth(authid, details) {
			// Create welcome details containing auth info.
			welcome := &wamp.Welcome{
				Details: wamp.Dict{
					"authid":       authid,
					"authrole":     authrole,
					"authmethod":   cr.AuthMethod(),
					"authprovider": cr.keyStore.Provider(),
				},
			}
			if err = ks.OnWelcome(authid, welcome, details); err != nil {
				return nil, err
			}
			return welcome, nil
		}
	}

	// Get the key and authrole needed for signing the challenge string.
	key, err := cr.keyStore.AuthKey(authid, cr.AuthMethod())
	if err != nil {
		return nil, errors.New("failed to retrieve key")
	}

	// TODO: pass channelBinding to computeChallenge when extractChannelBinding is available.
	//channelBinding = cr.extractChannelBinding(details)
	challenge, err := cr.computeChallenge(nil)
	if err != nil {
		return nil, err
	}

	extra := wamp.Dict{"challenge": hex.EncodeToString(challenge)}

	chalMsg := &wamp.Challenge{
		AuthMethod: cr.AuthMethod(),
		Extra:      extra,
	}
	// Challenge response needed. Send CHALLENGE message to client.
	select {
	case client.Send() <- chalMsg:
	default:
		return nil, errors.New("cannot send challenge to client: blocked")
	}

	// Read AUTHENTICATE response from client.
	msg, err := wamp.RecvTimeout(client, cr.timeout)
	if err != nil {
		return nil, err
	}

	authRsp, ok := msg.(*wamp.Authenticate)
	if !ok {
		return nil, fmt.Errorf("unexpected %v message received from client %v",
			msg.MessageType(), client)
	}

	verify, err := cr.verifySignature(authRsp.Signature, key, challenge)
	if err != nil {
		return nil, err
	}

	if !verify {
		return nil, errors.New("invalid signature")
	}

	// Create welcome message containing auth info.
	welcome := &wamp.Welcome{
		Details: wamp.Dict{
			"authid":       authid,
			"authrole":     authrole,
			"authmethod":   cr.AuthMethod(),
			"authprovider": cr.keyStore.Provider(),
		},
	}

	return welcome, nil
}

// verifySignature reports whether signature, which is hex encoded, is a
// signature by publicKey followed by the message it signed, and that message
// is the challenge sent to the client. Without checking the message, any
// signature by the client's key, such as one captured from an earlier
// authentication, would be accepted.
func (cr *CryptoSignAuthenticator) verifySignature(signature string, publicKey, challenge []byte) (bool, error) { //nolint:lll
	signatureBytes, err := hex.DecodeString(signature)
	if err != nil {
		return false, err
	}

	if len(signatureBytes) != 96 {
		return false, fmt.Errorf("signed message has invalid length (was %v, but should have been 96", len(signatureBytes))
	}

	var pubkey [32]byte
	copy(pubkey[:], publicKey)
	message, verify := sign.Open(nil, signatureBytes, &pubkey)
	if !verify {
		return false, nil
	}
	return subtle.ConstantTimeCompare(message, challenge) == 1, nil
}

// TODO: Finish implementing extractChannelBinding.
/*
func (cr *CryptoSignAuthenticator) extractChannelBinding(details wamp.Dict) []byte {
	if details == nil {
		return nil
	}
	authextra, ok := wamp.AsDict(details["authextra"])
	if !ok {
		return nil
	}
	_, ok = wamp.AsDict(authextra["channel_binding"])
	if !ok {
		return nil
	}
	// TODO: extract and convert channel binding
	return nil
}
*/

func (cr *CryptoSignAuthenticator) computeChallenge(channelBinding []byte) ([]byte, error) {
	challenge := make([]byte, 32)
	_, err := rand.Read(challenge)
	if err != nil {
		return nil, err
	}

	if channelBinding != nil {
		for i := range challenge {
			challenge[i] ^= channelBinding[i]
		}
	}

	return challenge, nil
}
