package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/gammazero/nexus/v3/wamp"
	"golang.org/x/crypto/nacl/sign"
)

type CryptoSignAuthenticator struct {
	keyStore KeyStore
	timeout  time.Duration
}

func NewCryptoSignAuthenticator(keyStore KeyStore, timeout time.Duration) *CryptoSignAuthenticator {
	return &CryptoSignAuthenticator{
		keyStore: keyStore,
		timeout:  timeout,
	}
}

func (cr *CryptoSignAuthenticator) AuthMethod() string { return "cryptosign" }

func (cr *CryptoSignAuthenticator) Authenticate(
	sid wamp.ID,
	details wamp.Dict,
	client wamp.Peer) (*wamp.Welcome, error) {
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

	//channelBinding := cr.extractChannelBinding(details)
	challenge, err := cr.computeChallenge(nil)
	if err != nil {
		return nil, err
	}

	extra := wamp.Dict{"challenge": hex.EncodeToString(challenge)}

	// Challenge response needed.  Send CHALLENGE message to client.
	err = client.Send(&wamp.Challenge{
		AuthMethod: cr.AuthMethod(),
		Extra:      extra,
	})

	if err != nil {
		return nil, err
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

	verify, err := cr.verifySignature(authRsp.Signature, key)
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

func (cr *CryptoSignAuthenticator) verifySignature(signature string, publicKey []byte) (bool, error) {
	signatureBytes, err := hex.DecodeString(signature)
	if err != nil {
		fmt.Println(err)
		return false, err
	}

	if len(signatureBytes) != 96 {
		return false, fmt.Errorf("signed message has invalid length (was %v, but should have been 96", len(signatureBytes))
	}

	signedOut := make([]byte, 32)
	var pubkey [32]byte
	copy(pubkey[:], publicKey)
	_, verify := sign.Open(signedOut, signatureBytes, &pubkey)

	return verify, nil
}

func (cr *CryptoSignAuthenticator) extractChannelBinding(details wamp.Dict) []byte {
	if details != nil {
		authextra, ok := wamp.AsDict(details["authextra"])
		if !ok {
			return nil
		}

		_, ok = wamp.AsDict(authextra["channel_binding"])
		if !ok {
			return nil
		}

		// TODO: extract and convert channel binding
	}

	return nil
}

func (cr *CryptoSignAuthenticator) computeChallenge(channelBinding []byte) ([]byte, error) {
	challenge := make([]byte, 32)
	_, err := rand.Read(challenge)
	if err != nil {
		return nil, err
	}

	signedMessage := make([]byte, 32)
	if channelBinding != nil {
		for index, v := range challenge {
			signedMessage[index] = v ^ channelBinding[index]
		}
	} else {
		copy(signedMessage, challenge)
	}

	return signedMessage, nil
}
