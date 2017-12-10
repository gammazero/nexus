package crsign

import (
	"crypto/sha256"
	"testing"

	"github.com/gammazero/nexus/wamp"
	"golang.org/x/crypto/pbkdf2"
)

var chStr = "{ \"nonce\":\"LHRTC9zeOIrt_9U3\", \"authprovider\":\"userdb\", \"authid\":\"peter\", \"timestamp\":\"2014-06-22T16:36:25.448Z\", \"authrole\":\"user\", \"authmethod\":\"wampcra\", \"session\":3251278072152162 }"

func TestCRSign(t *testing.T) {
	sig := SignChallenge(chStr, []byte("secret"))
	if sig != "NWktSrMd4ItBSAKYEwvu1bTY7G/sSyjKbz+pNP9c04A=" {
		t.Fatal("Wrong signature")
	}
}

func TestRespondChallenge(t *testing.T) {
	salt := []byte("salt123")
	pass := "password"

	// Compute derived key.  Normally this would normally be precomputed and
	// the router would read it and the salting from from storage.
	dk := pbkdf2.Key([]byte(pass), salt, defaultIters, defaultKeyLen, sha256.New)

	// Server creates CHALLENGE message containing challenge string and salting
	// info that was used to create derived key.
	extra := wamp.Dict{"challenge": chStr}
	extra["salt"] = salt
	extra["keylen"] = defaultKeyLen
	extra["iterations"] = defaultIters
	chMsg := &wamp.Challenge{
		AuthMethod: "wampcra",
		Extra:      extra,
	}

	// Client computes derived key from password and salting info, then signes
	// challenge using derived key.  Response gets sent back to router.
	sigClient := RespondChallenge(pass, chMsg, nil)

	// Router computes its own signature for the challenge and compares it with
	// the client's.Sign challenge using derived key.
	sigServer := SignChallenge(chStr, dk)
	if sigClient != sigServer {
		t.Fatal("Client and server signatures do not match")
	}

	// Check that signature was what was expected.
	if sigServer != "qczWyhq0mAtfNeqPCxorzlcz0t4jns97XBUHoTu2Brs=" {
		t.Fatal("Wrong signature:", sigServer)
	}
}
