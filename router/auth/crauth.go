package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gammazero/nexus/v3/wamp/crsign"
)

// CRAuthenticator is a challenge-response authenticator.
type CRAuthenticator struct {
	keyStore KeyStore
	timeout  time.Duration
}

// NewCRAuthenticator creates a new CRAuthenticator with the given key store
// and the maximum time to wait for a client to respond to a CHALLENGE message.
func NewCRAuthenticator(keyStore KeyStore, timeout time.Duration) *CRAuthenticator {
	return &CRAuthenticator{
		keyStore: keyStore,
		timeout:  timeout,
	}
}

func (cr *CRAuthenticator) AuthMethod() string { return "wampcra" }

func (cr *CRAuthenticator) Authenticate(sid wamp.ID, details wamp.Dict, client wamp.Peer) (*wamp.Welcome, error) {
	authid, _ := wamp.AsString(details["authid"])
	if authid == "" {
		return nil, errors.New("missing authid")
	}

	authrole, err := cr.keyStore.AuthRole(authid)
	if err != nil {
		// Do not error here since that leaks authid info.
		authrole = "user"
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
		// Do not error here since that leaks authid info.
		keyStr, _ := nonce()
		if keyStr == "" {
			keyStr = wamp.NowISO8601()
		}
		key = []byte(keyStr)
	}

	// Create the JSON encoded challenge string.
	chStr, err := cr.makeChallengeStr(sid, authid, authrole)
	if err != nil {
		return nil, err
	}

	extra := wamp.Dict{"challenge": chStr}
	// If key was created using PBKDF2, then salting info should be present.
	salt, keylen, iters := cr.keyStore.PasswordInfo(authid)
	if salt != "" {
		extra["salt"] = salt
		extra["keylen"] = keylen
		extra["iterations"] = iters
	}

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

	// Check signature.
	if !crsign.VerifySignature(authRsp.Signature, chStr, key) {
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

	if ks != nil {
		// Tell the keystore that the client was authenticated, and provide the
		// transport details if available.
		if err = ks.OnWelcome(authid, welcome, details); err != nil {
			return nil, err
		}
	}
	return welcome, nil
}

func (cr *CRAuthenticator) makeChallengeStr(session wamp.ID, authid, authrole string) (string, error) {
	nonce, err := nonce()
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %s", err)
	}

	return fmt.Sprintf(
		"{ \"nonce\":\"%s\", \"authprovider\":\"%s\", \"authid\":\"%s\", \"timestamp\":\"%s\", \"authrole\":\"%s\", \"authmethod\":\"%s\", \"session\":%d }",
		nonce, cr.keyStore.Provider(), authid, wamp.NowISO8601(), authrole,
		cr.AuthMethod(), int(session)), nil
}

// nonce generates 16 random bytes as a base64 encoded string.
func nonce() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
