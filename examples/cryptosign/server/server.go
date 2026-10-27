/*
Example nexus WAMP router that authenticates clients with WAMP-Cryptosign.

A client authenticates by signing a random challenge from the router with its
Ed25519 private key. The router knows each client's public key in advance,
from a key store, and admits the client under the authrole that the key store
gives for it.
*/
package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/wamp"
)

const (
	address = "localhost:8080"
	realm   = "realm1"

	// demoPublicKey is the public key of the private key in the WAMP spec's
	// cryptosign test vectors, which the example client uses by default. It is
	// public knowledge, so never trust it outside this example.
	demoPublicKey = "1adfc8bfe1d35616e64dffbd900096f23b066f914c8c2ffbb66f6075b96e116d"
)

// keyStore is a cryptosign key store that knows one client, identified by its
// authid, and its Ed25519 public key. A real key store would usually look up
// clients in a database.
type keyStore struct {
	authid    string
	authrole  string
	publicKey []byte
}

// AuthKey returns the client's public key, which the cryptosign authenticator
// uses to verify the client's signature.
func (ks *keyStore) AuthKey(authid, authmethod string) ([]byte, error) {
	if authid != ks.authid {
		return nil, errors.New("no such user: " + authid)
	}
	if authmethod != "cryptosign" {
		return nil, errors.New("unsupported authmethod: " + authmethod)
	}
	return ks.publicKey, nil
}

// AuthRole returns the authrole that the client is admitted under.
func (ks *keyStore) AuthRole(authid string) (string, error) {
	if authid != ks.authid {
		return "", errors.New("no such user: " + authid)
	}
	return ks.authrole, nil
}

// PasswordInfo is only used for WAMP-CRA with salted passwords.
func (ks *keyStore) PasswordInfo(string) (string, int, int) { return "", 0, 0 }

// Provider names the key store in the WELCOME details.
func (ks *keyStore) Provider() string { return "example-keystore" }

func main() {
	authid := flag.String("authid", "alice", "authid of the client to admit")
	pubkeyHex := flag.String("pubkey", demoPublicKey,
		"hex encoded Ed25519 public key of the client")
	flag.Parse()

	publicKey, err := hex.DecodeString(*pubkeyHex)
	if err != nil || len(publicKey) != 32 {
		log.Fatal("-pubkey must be a hex encoded 32 byte Ed25519 public key")
	}
	if *pubkeyHex == demoPublicKey {
		log.Print("Using the demo public key, which is public knowledge")
	}

	ks := &keyStore{
		authid:    *authid,
		authrole:  "user",
		publicKey: publicKey,
	}

	// Create router instance. The realm accepts only cryptosign, so a client
	// that does not authenticate with it is refused. A timeout of 0 uses the
	// default time to wait for the client's signature.
	routerConfig := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			{
				URI: wamp.URI(realm),
				Authenticators: []auth.Authenticator{
					auth.NewCryptoSignAuthenticator(ks, 0),
				},
			},
		},
	}
	nxr, err := router.NewRouter(routerConfig, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer nxr.Close()

	// Create and run server. A real router should use ListenAndServeTLS, so that
	// the authentication cannot be observed or tampered with.
	closer, err := router.NewWebsocketServer(nxr).ListenAndServe(address)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Websocket server listening on ws://%s/, admitting %q", address, *authid)

	// Wait for SIGINT (CTRL-c), then close server and exit.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	<-shutdown
	_ = closer.Close()
}
