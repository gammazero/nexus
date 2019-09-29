package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/gammazero/nexus/v3/wamp"
)

// ticketAuthenticator implements CRAuthenticator
type TicketAuthenticator struct {
	CRAuthenticator
}

// NewTicketAuthenticator creates a ticket-based CR authenticator.
//
// Caution: This scheme is extremely simple and flexible, but the resulting
// security may be limited. E.g., the ticket value will be sent over the
// wire. If the transport WAMP is running over is not encrypted, a
// man-in-the-middle can sniff and possibly hijack the ticket. If the ticket
// value is reused, that might enable replay attacks.
func NewTicketAuthenticator(keyStore KeyStore, timeout time.Duration) *TicketAuthenticator {
	return &TicketAuthenticator{
		CRAuthenticator{
			keyStore: keyStore,
			timeout:  timeout,
		},
	}
}

func (t *TicketAuthenticator) AuthMethod() string { return "ticket" }

func (t *TicketAuthenticator) Authenticate(sid wamp.ID, details wamp.Dict, client wamp.Peer) (*wamp.Welcome, error) {
	// The HELLO.Details.authid|string is the authentication ID (e.g. username)
	// the client wishes to authenticate as. For Ticket-based authentication,
	// this MUST be provided.
	authID, _ := wamp.AsString(details["authid"])
	if authID == "" {
		return nil, errors.New("missing authid")
	}

	authrole, err := t.keyStore.AuthRole(authID)
	if err != nil {
		authrole = ""
	}

	ks, ok := t.keyStore.(BypassKeyStore)
	if ok {
		if ks.AlreadyAuth(authID, details) {
			// Create welcome details containing auth info.
			welcome := &wamp.Welcome{
				Details: wamp.Dict{
					"authid":       authID,
					"authrole":     authrole,
					"authmethod":   t.AuthMethod(),
					"authprovider": t.keyStore.Provider(),
				},
			}
			if err = ks.OnWelcome(authID, welcome, details); err != nil {
				return nil, err
			}
			return welcome, nil
		}
	}

	ticket, err := t.keyStore.AuthKey(authID, t.AuthMethod())
	if err != nil {
		// Do not return error here as this leaks authid.  Instead, set the
		// ticket to nil which will prevent it from authenticating.
		ticket = nil
	}

	// Challenge Extra map is empty since the ticket challenge only asks for a
	// ticket (using authmethod) and provides no additional challenge info.
	err = client.Send(&wamp.Challenge{
		AuthMethod: t.AuthMethod(),
		Extra:      wamp.Dict{},
	})
	if err != nil {
		return nil, err
	}

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
	if ticket == nil || authRsp.Signature != string(ticket) {
		return nil, errors.New("invalid ticket")
	}

	// Create welcome details containing auth info.
	welcome := &wamp.Welcome{
		Details: wamp.Dict{
			"authid":       authID,
			"authmethod":   t.AuthMethod(),
			"authrole":     authrole,
			"authprovider": t.keyStore.Provider(),
		},
	}

	if ks != nil {
		// Tell the keystore that the client was authenticated, and provide the
		// transport details if available.
		if err = ks.OnWelcome(authID, welcome, details); err != nil {
			return nil, err
		}
	}
	return welcome, nil
}
