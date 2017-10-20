package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/gammazero/nexus/wamp"
)

// ticketAuthenticator implements CRAuthenticator
type ticketAuthenticator struct {
	CRAuthenticator
}

// NewTicketAuthenticator creates a ticket-based CR authenticator.
//
// Caution: This scheme is extremely simple and flexible, but the resulting
// security may be limited. E.g., the ticket value will be sent over the
// wire. If the transport WAMP is running over is not encrypted, a
// man-in-the-middle can sniff and possibly hijack the ticket. If the ticket
// value is reused, that might enable replay attacks.
func NewTicketAuthenticator(keyStore KeyStore, timeout time.Duration) Authenticator {
	return &ticketAuthenticator{
		CRAuthenticator{
			keyStore: keyStore,
			timeout:  timeout,
		},
	}
}

func (t *ticketAuthenticator) AuthMethod() string { return "ticket" }

func (t *ticketAuthenticator) Authenticate(sid wamp.ID, details wamp.Dict, client wamp.Peer) (*wamp.Welcome, error) {
	// The HELLO.Details.authid|string is the authentication ID (e.g. username)
	// the client wishes to authenticate as. For Ticket-based authentication,
	// this MUST be provided.
	authID := wamp.OptionString(details, "authid")
	if authID == "" {
		return nil, errors.New("missing authid")
	}

	authrole, err := t.keyStore.AuthRole(authID)
	if err != nil {
		return nil, err
	}

	// Ticket authenticator should not have been used for authmethod other than
	// "ticket", but check anyway.
	if "ticket" != wamp.OptionString(details, "authmethod") {
		return nil, errors.New("invalid authmethod for ticket authentication")
	}

	ticket, err := t.keyStore.AuthKey(authID, t.AuthMethod())
	if err != nil {
		return nil, err
	}

	// Challenge Extra map is empty since the ticket challenge only asks for a
	// ticket (using authmethod) and provides no additional challenge info.
	client.Send(&wamp.Challenge{
		AuthMethod: t.AuthMethod(),
		Extra:      wamp.Dict{},
	})

	// Read AUTHENTICATE response from client.
	msg, err := wamp.RecvTimeout(client, t.timeout)
	if err != nil {
		return nil, err
	}
	authRsp, ok := msg.(*wamp.Authenticate)
	if !ok {
		return nil, fmt.Errorf("unexpected %v message received from client %v",
			msg.MessageType(), client)
	}

	// The client will send an AUTHENTICATE message containing a ticket.  The
	// server will then check if the ticket provided is permissible (for the
	// authid given).
	if authRsp.Signature != string(ticket) {
		return nil, errors.New("invalid ticket")
	}

	// Create welcome details containing auth info.
	welcomeDetails := wamp.Dict{
		"authid":       authID,
		"authmethod":   t.AuthMethod(),
		"authrole":     authrole,
		"authprovider": t.keyStore.Provider()}

	return &wamp.Welcome{Details: welcomeDetails}, nil
}
