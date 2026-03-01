package crsign_test

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/pbkdf2"

	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gammazero/nexus/v3/wamp/crsign"
)

var chStr = "{ \"nonce\":\"LHRTC9zeOIrt_9U3\", \"authprovider\":\"userdb\", \"authid\":\"peter\", " +
	"\"timestamp\":\"2014-06-22T16:36:25.448Z\", \"authrole\":\"user\", \"authmethod\":\"wampcra\", " +
	"\"session\":3251278072152162 }"

func TestCRSign(t *testing.T) {
	sig := crsign.SignChallenge(chStr, []byte("secret"))
	require.Equal(t, "NWktSrMd4ItBSAKYEwvu1bTY7G/sSyjKbz+pNP9c04A=", sig)
}

func TestRespondChallenge(t *testing.T) {
	salt := []byte("salt123")
	secret := "password"

	// Compute derived key. Normally this would normally be precomputed and the
	// router would read it and the salting from from storage. Compute derived
	// key.
	dk := pbkdf2.Key([]byte(secret), salt, crsign.DefaultIters, crsign.DefaultKeyLen, sha256.New)
	// Get base64 bytes of derived key.
	derivedKey := []byte(base64.StdEncoding.EncodeToString(dk))

	// Server creates CHALLENGE message containing challenge string and salting
	// info that was used to create derived key.
	extra := wamp.Dict{"challenge": chStr}
	extra["salt"] = salt
	extra["keylen"] = crsign.DefaultKeyLen
	extra["iterations"] = crsign.DefaultIters
	chMsg := &wamp.Challenge{
		AuthMethod: "wampcra",
		Extra:      extra,
	}

	// Client computes derived key from password and salting info, then signes
	// challenge using derived key. Response gets sent back to router.
	sigClient := crsign.RespondChallenge(secret, chMsg, nil)

	// Router computes its own signature for the challenge and compares it with
	// the client's.Sign challenge using derived key.
	sigServer := crsign.SignChallenge(chStr, derivedKey)
	require.Equal(t, sigClient, sigServer, "Client and server signatures do not match")

	// Check that signature was what was expected.
	require.Equal(t, "hk/2riA2JqydfL5wLoicrYfAt8uNeP6nikk9kqDhsnM=", sigServer)
}
