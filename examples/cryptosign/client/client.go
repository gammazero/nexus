/*
Example nexus WAMP client that authenticates with WAMP-Cryptosign.

The client announces its authid and its Ed25519 public key in HELLO, then signs
the router's challenge with its private key. Run with -genkey to make a new
key pair, and give the server the public key with its -pubkey flag.
*/
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

const (
	addr  = "ws://localhost:8080/ws"
	realm = "realm1"

	// demoPrivateKey is the private key, as a hex encoded 32 byte seed, from the
	// WAMP spec's cryptosign test vectors. It is public knowledge, so never use
	// it outside this example.
	demoPrivateKey = "4d57d97a68f555696620a6d849c0ce582568518d729eb753dc7c732de2804510"
)

func main() {
	authid := flag.String("authid", "alice", "authid to authenticate as")
	keyHex := flag.String("key", demoPrivateKey,
		"hex encoded 32 byte Ed25519 private key seed")
	genkey := flag.Bool("genkey", false, "print a new key pair and exit")
	flag.Parse()

	if *genkey {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("private key (client -key):  ", hex.EncodeToString(priv.Seed()))
		fmt.Println("public key  (server -pubkey):", hex.EncodeToString(pub))
		return
	}

	seed, err := hex.DecodeString(*keyHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		log.Fatal("-key must be a hex encoded 32 byte Ed25519 private key seed")
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)

	logger := log.New(os.Stdout, "", 0)
	cfg := client.Config{
		Realm: realm,
		// The router looks up the client by authid. The spec also has the
		// client send its public key, so that a router can find the client by
		// key instead.
		HelloDetails: wamp.Dict{
			"authid": *authid,
			"authextra": wamp.Dict{
				"pubkey": hex.EncodeToString(publicKey),
			},
		},
		// The handler for the "cryptosign" authmethod signs the router's
		// challenge. Adding it here also adds "cryptosign" to the authmethods
		// that the client announces in HELLO.
		AuthHandlers: map[string]client.AuthFunc{
			"cryptosign": func(c *wamp.Challenge) (string, wamp.Dict) {
				return signChallenge(c, privateKey), wamp.Dict{}
			},
		},
		Logger: logger,
	}

	cli, err := client.ConnectNet(context.Background(), addr, cfg)
	if err != nil {
		logger.Fatal("authentication failed: ", err)
	}
	defer cli.Close()

	details := cli.RealmDetails()
	logger.Printf("Authenticated as %v with authrole %v, using %v",
		details["authid"], details["authrole"], details["authmethod"])
}

// signChallenge signs the hex encoded challenge in the router's CHALLENGE,
// and returns what AUTHENTICATE carries: the 64 byte signature followed by
// the 32 byte challenge, hex encoded.
func signChallenge(c *wamp.Challenge, privateKey ed25519.PrivateKey) string {
	challengeHex, _ := wamp.AsString(c.Extra["challenge"])
	challenge, err := hex.DecodeString(challengeHex)
	if err != nil {
		// An empty signature makes the router reject the authentication.
		return ""
	}
	signature := ed25519.Sign(privateKey, challenge)
	return hex.EncodeToString(append(signature, challenge...))
}
