package auth

import (
	"errors"
	"time"

	"github.com/gammazero/nexus/wamp"
)

// ticketAuthenticator implements CRAuthenticator
type ticketAuthenticator struct {
	userDB UserDB
}

// NewTicketAuthenticator creates a ticket-based CR authenticator from the
// given authID to ticket map.
//
// Caution: This scheme is extremely simple and flexible, but the resulting
// security may be limited. E.g., the ticket value will be sent over the
// wire. If the transport WAMP is running over is not encrypted, a
// man-in-the-middle can sniff and possibly hijack the ticket. If the ticket
// value is reused, that might enable replay attacks.
func NewTicketAuthenticator(userDB UserDB) CRAuthenticator {
	return &ticketAuthenticator{
		userDB: userDB,
	}
}

// pendingTicketAuth implements the PendingCRAuth interface.
type pendingTicketAuth struct {
	authID   string
	secret   string
	role     string
	provider string
}

// Return the ticket challenge message.
func (p *pendingTicketAuth) Msg() *wamp.Challenge {
	// Challenge Extra map is empty since the ticket challenge only asks for a
	// ticket (using authmethod) and provides no additional challenge info.
	return &wamp.Challenge{
		AuthMethod: "ticket",
		Extra:      map[string]interface{}{},
	}
}

// Create a PendingCRAuth for ticket CR authentication.
func (t *ticketAuthenticator) Challenge(details map[string]interface{}) (PendingCRAuth, error) {
	// The HELLO.Details.authid|string is the authentication ID (e.g. username)
	// the client wishes to authenticate as. For Ticket-based authentication,
	// this MUST be provided.
	_authID, ok := details["authid"]
	if !ok {
		return nil, errors.New("missing authid")
	}
	authID := _authID.(string)

	// Ticket authenticator should not have been used for authmethod other than
	// "ticket", but check anyway.
	if "ticket" != details["authmethod"].(string) {
		return nil, errors.New("invalid authmethod for ticket authentication")
	}

	// If the server is willing to let the client authenticate using a ticket
	// and the server recognizes the provided authid, it'll send a CHALLENGE
	// message.
	userInfo, err := t.userDB.ReadUserInfo(authID)
	if err != nil {
		return nil, err
	}

	return &pendingTicketAuth{
		authID:   authID,
		role:     userInfo["role"],
		secret:   userInfo["secret"],
		provider: t.userDB.Name(),
	}, nil
}

// Timeout returns the amount of time to wait for a client to respond to a
// CHALLENGE message.
func (p *pendingTicketAuth) Timeout() time.Duration {
	return defaultCRAuthTimeout
}

// Authenticate the client's response to a challenge.
func (p *pendingTicketAuth) Authenticate(msg *wamp.Authenticate) (*wamp.Welcome, error) {
	// The client will send an AUTHENTICATE message containing a ticket.  The
	// server will then check if the ticket provided is permissible (for the
	// authid given).
	if p.secret != msg.Signature {
		return nil, errors.New("invalid ticket")
	}

	// Create welcome details containing auth info.
	details := map[string]interface{}{
		"authid":       p.authID,
		"authmethod":   "ticket",
		"authrole":     p.role,
		"authprovider": p.provider}

	return &wamp.Welcome{Details: details}, nil
}
