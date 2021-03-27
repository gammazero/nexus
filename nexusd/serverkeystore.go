package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
)

// `ServerKeyStore` is server-side implement of `Keystore`
type ServerKeyStore struct {
	// The name of this KeyStore instance
	Provider2 string	`json:"provider"`
	// The authentication ID the client announced (e.g. username).
	Authid string 		`json:"authid"`
	// The authrole to assign to the client if successfully authenticated
	Role string 		`json:"role"`
	// The `secret` shared with the client.
	Secret string 		`json:"secret"`
	// If `secret` was salted, the `keylen` of the derived key (a parameter of
	// the pbkdf2 algorithm used).
	Keylen int 			`json:"keylen"`
	//If `secret` was salted, the `iterations` during salting (a parameter of
	// the pbkdf2 algorithm used).
	Iterations int 		`json:"iterations"`
	// If `secret` was salted, the `salt` used (with pbkdf2)
	Salt string
}


// AuthKey returns the user's key appropriate for the specified authmethod.
func (ks *ServerKeyStore) AuthKey(authid, authmethod string) ([]byte, error) {
	if authid != ks.Authid {
		return nil, errors.New("no such user: " + authid)
	}

	switch authmethod {
	case "wampcra":
		// Lookup the user's PBKDF2-derived key.
		return []byte(ks.Secret), nil
	//case "ticket":
	//	// Lookup the user's key.
	//	return []byte(ks.Secret), nil
	}

	return nil, errors.New("unsupported authmethod")
}


// PasswordInfo returns salting info for the user's password.
// This information must be available when using keys computed with PBKDF2.
func (ks *ServerKeyStore) PasswordInfo(authid string) (string, int, int) {
	if authid != ks.Authid {
		return "", 0, 0
	}

	return ks.Salt, ks.Keylen, ks.Iterations
}


// Returns name of this KeyStore instance.
func (ks *ServerKeyStore) Provider() string { return ks.Provider2 }


// Returns the authrole for the user.
func (ks *ServerKeyStore) AuthRole(authid string) (string, error) {
	if authid !=  ks.Authid {
		return "", errors.New("no such user: " + authid)
	}
	return ks.Role, nil
}

// Load wamp-cra config from json file
func LoadWAMPCRAConfig(path string) []ServerKeyStore {
	file, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatal("Config File Missing. ", err)
	}

	var sks []ServerKeyStore
	err = json.Unmarshal(file, &sks)
	if err != nil {
		log.Fatal("Config Parse Error: ", err)
	}

	return sks
}

