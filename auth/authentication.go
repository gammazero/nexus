/*
Package auth provides interfaces for implementing authentication logic that the
WAMP router can use.

In addition in authentication and challenge-response authentication interface,
this package provides default implementations for some authentication methods.
*/
package auth

import (
	"errors"
	"time"

	"github.com/gammazero/nexus/wamp"
)

const defaultCRAuthTimeout = time.Minute

// Authenticator is implemented by a type that handles authentication using
// only the HELLO message.
type Authenticator interface {
	// Authenticate takes HELLO details and returns a WELCOME message if
	// successful, otherwise it returns an error.
	Authenticate(details map[string]interface{}, client wamp.Peer) (*wamp.Welcome, error)
}

// PendingCRAuth is a pending challenge-response.  It contains whatever data is
// needed to authenticate a client's response to a CHALLENGE.
type PendingCRAuth interface {
	// Msg returns the WAMP CHALLENGE message to send to the client.
	Msg() *wamp.Challenge

	// Timeout returns the amount of time to wait for a client to respond to a
	// CHALLENGE message.
	Timeout() time.Duration

	// Authenticate takes a WAMP AUTHENTICATE message, and authenticates the
	// message against the challenge.
	//
	// Returns a WELCOME message or error.
	Authenticate(msg *wamp.Authenticate) (*wamp.Welcome, error)
}

// UserDB is a simple interface supporting CRUD operations for user data used
// for authentication.
type UserDB interface {
	// Name returns the name of the database. Used to supply provider
	// information to the welcome message.
	Name() string

	// CreateUser a new user in the DB.  Returns error is user already exists.
	CreateUser(authID string, userInfo map[string]string) error

	// ReadsUserInfo returns the user information map for the requested user.
	// Returns error if the user does not not exist.
	ReadUserInfo(authID string) (map[string]string, error)

	// UpdateUserInfo is used to update individual user data fields.  Returns
	// error if user not found.
	UpdateUserInfo(authID, field, value string) error

	// DeleteUser removes a user from the database.
	DeleteUser(authID string) error
}

// staticUserDB implements a the simplest storage for user data.
type staticUserDB struct {
	name    string
	userMap map[string]map[string]string
}

// Create a new simple user DB for authentication.  This is intended for use as
// as static DB that is configured as part of initializing the WAMP router.
func NewStaticUserDB(name string) UserDB {
	return &staticUserDB{
		name:    name,
		userMap: map[string]map[string]string{},
	}
}

func (db *staticUserDB) Name() string { return db.name }

func (db *staticUserDB) CreateUser(authID string, userInfo map[string]string) error {
	if _, ok := db.userMap[authID]; ok {
		return errors.New("user already exists: " + authID)
	}
	info := make(map[string]string, len(userInfo))
	for k, v := range userInfo {
		info[k] = v
	}
	db.userMap[authID] = info
	return nil
}

func (db *staticUserDB) DeleteUser(authID string) error {
	_, ok := db.userMap[authID]
	if !ok {
		return errors.New("user not found: " + authID)
	}
	delete(db.userMap, authID)
	return nil
}

func (db *staticUserDB) ReadUserInfo(authID string) (map[string]string, error) {
	userInfo, ok := db.userMap[authID]
	if !ok {
		return nil, errors.New("user not found: " + authID)
	}
	return userInfo, nil
}

func (db *staticUserDB) UpdateUserInfo(authID, field, value string) error {
	userInfo, ok := db.userMap[authID]
	if !ok {
		return errors.New("user not found: " + authID)
	}
	if value == "" {
		delete(userInfo, field)
	} else {
		userInfo[field] = value
	}
	return nil
}
