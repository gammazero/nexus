package auth

import (
	"fmt"

	"github.com/gammazero/nexus/wamp"
)

// Challenge returns a PendingCRAuth for Challenge/Response authentication.
type Challenger interface {
	Challenge(details map[string]interface{}) (PendingCRAuth, error)
}

// CRAuthenticator
type CRAuthenticator struct {
	challenger Challenger
}

func NewCRAuthenticator(challenger Challenger) (*CRAuthenticator, error) {
	return &CRAuthenticator{
		challenger: challenger,
	}, nil
}

func (cr *CRAuthenticator) Authenticate(details map[string]interface{}, client wamp.Peer) (*wamp.Welcome, error) {
	pendingCRAuth, err := cr.challenger.Challenge(details)
	if err != nil {
		return nil, err
	}

	// Challenge response needed.  Send CHALLENGE message to client.
	client.Send(pendingCRAuth.Msg())

	// Read AUTHENTICATE response from client.
	msg, err := wamp.RecvTimeout(client, pendingCRAuth.Timeout())
	if err != nil {
		return nil, err
	}
	authRsp, ok := msg.(*wamp.Authenticate)
	if !ok {
		return nil, fmt.Errorf("unexpected %v message received from client %v",
			msg.MessageType(), client)
	}

	welcome, err := pendingCRAuth.Authenticate(authRsp)
	if err != nil {
		return nil, err
	}

	return welcome, nil
}
