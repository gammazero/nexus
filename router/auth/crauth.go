package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/gammazero/nexus/wamp"
	"github.com/gammazero/nexus/wamp/crsign"
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
	authid := wamp.OptionString(details, "authid")
	if authid == "" {
		return nil, errors.New("missing authid")
	}

	// Get the auth key needed for signing the challenge string.
	key, err := cr.keyStore.AuthKey(authid, cr.AuthMethod())
	if err != nil {
		// Do not error here since that leaks authid info.
		keyStr, _ := getNonce()
		if keyStr == "" {
			keyStr = wamp.NowISO8601()
		}
		key = []byte(keyStr)
	}

	authrole, err := cr.keyStore.AuthRole(authid)
	if err != nil {
		// Do not error here since that leaks authid info.
		authrole = "user"
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
	signedCh := crsign.SignChallenge(chStr, key)
	if !hmac.Equal([]byte(authRsp.Signature), []byte(signedCh)) {
		return nil, errors.New("invalid signature")
	}

	// Create welcome details containing auth info.
	welcomeDetails := wamp.Dict{
		"authid":       authid,
		"authrole":     authrole,
		"authmethod":   cr.AuthMethod(),
		"authprovider": cr.keyStore.Provider(),
	}

	return &wamp.Welcome{Details: welcomeDetails}, nil
}

func (cr *CRAuthenticator) makeChallengeStr(session wamp.ID, authid, authrole string) (string, error) {
	nonce, err := getNonce()
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %s", err)
	}

	return fmt.Sprintf(
		"{ \"nonce\":\"%s\", \"authprovider\":\"%s\", \"authid\":\"%s\", \"timestamp\":\"%s\", \"authrole\":\"%s\", \"authmethod\":\"%s\", \"session\":%d }",
		nonce, cr.keyStore.Provider(), authid, wamp.NowISO8601(), authrole,
		cr.AuthMethod(), int(session)), nil
}

func getNonce() (string, error) {
	c := 16
	b := make([]byte, c)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
