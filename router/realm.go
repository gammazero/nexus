package router

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/gammazero/nexus/auth"
	"github.com/gammazero/nexus/wamp"
)

// A Realm is a WAMP routing and administrative domain, optionally protected by
// authentication and authorization.  WAMP messages are only routed within a
// Realm.
type Realm struct {
	uri wamp.URI

	broker Broker
	dealer Dealer

	authorizer Authorizer

	// authmethod -> Authenticator
	crAuthenticators map[string]auth.CRAuthenticator
	authenticators   map[string]auth.Authenticator

	// session ID -> Session
	clients map[wamp.ID]*Session

	metaClient wamp.Peer
	actionChan chan func()
}

// NewRealm creates a new Realm with default broker, dealer, and authorizer
// implementtions.  The Realm has no authorizers unless anonymousAuth is true.
func NewRealm(uri wamp.URI, strictURI, anonymousAuth, allowDisclose bool) *Realm {
	return NewCustomRealm(uri, strictURI, anonymousAuth, allowDisclose, nil,
		nil, nil, nil, nil)
}

// NewCustomerRealm creates a new Realm with the specified components, or will use default implementations if they are nil.
func NewCustomRealm(uri wamp.URI, strictURI, anonymousAuth, allowDisclose bool, broker Broker, dealer Dealer, authorizer Authorizer, auths map[string]auth.Authenticator, crAuths map[string]auth.CRAuthenticator) *Realm {
	if broker == nil {
		broker = NewBroker(strictURI, allowDisclose)
	}
	if dealer == nil {
		dealer = NewDealer(strictURI, allowDisclose)
	}
	if authorizer == nil {
		authorizer = NewAuthorizer()
	}

	r := &Realm{
		uri:        uri,
		broker:     broker,
		dealer:     dealer,
		authorizer: authorizer,
		clients:    map[wamp.ID]*Session{},
		actionChan: make(chan func()),
	}

	// If allowing anonymous authentication, then install the anonymous
	// authenticator.  Install this first so that it is replaced in case a
	// custom anonymous authenticator is supplied.
	if anonymousAuth {
		r.authenticators = map[string]auth.Authenticator{
			"anonymous": auth.AnonymousAuth}
	}
	// Add any supplied Authenticators.
	if len(auths) != 0 {
		if r.authenticators == nil {
			r.authenticators = make(map[string]auth.Authenticator, len(auths))
		}
		for method, auth := range auths {
			r.authenticators[method] = auth
		}
	}
	// Add any supplied CRAuthenticators.
	if len(crAuths) != 0 {
		r.crAuthenticators = make(map[string]auth.CRAuthenticator, len(crAuths))
		for method, auth := range crAuths {
			r.crAuthenticators[method] = auth
		}
	}

	// Create a session that bridges two peers.  Meta events are published by
	// the peer returned, which is the remote side of the router uplink.
	// Sending a PUBLISH message to p will result in the router publishing the
	// event to any subscribers.
	p, _ := r.bridgeSession(nil)
	r.metaClient = p

	go r.run()
	return r
}

// AddAuthenticator registers the Authenticator for the specified method.
func (r *Realm) AddAuthenticator(method string, athr auth.Authenticator) {
	r.actionChan <- func() {
		if r.authenticators == nil {
			r.authenticators = map[string]auth.Authenticator{}
		}
		r.authenticators[method] = athr
	}
}

// AddCRAuthenticator registers the CRAuthenticator for the specified method.
func (r *Realm) AddCRAuthenticator(method string, athr auth.CRAuthenticator) {
	r.actionChan <- func() {
		if r.crAuthenticators == nil {
			r.crAuthenticators = map[string]auth.CRAuthenticator{}
		}
		r.crAuthenticators[method] = athr
	}
}

// DelAuthenticator deletes the Authenticator for the specified method.
func (r *Realm) DelAuthenticator(method string) {
	r.actionChan <- func() {
		delete(r.authenticators, method)
	}
}

// DelCRAuthenticator deletes the CRAuthenticator for the specified method.
func (r *Realm) DelCRAuthenticator(method string) {
	r.actionChan <- func() {
		delete(r.crAuthenticators, method)
	}
}

// Close kills all clients, causing them to send a goodbye message.
func (r Realm) Close() {
	defer r.broker.Close()
	defer r.dealer.Close()

	sync := make(chan bool)
	r.actionChan <- func() {
		for _, client := range r.clients {
			client.kill <- wamp.ErrSystemShutdown
		}
		sync <- false
	}
	<-sync

	// Wait until handleSession for all clients has exited.
	for {
		r.actionChan <- func() {
			if len(r.clients) == 0 {
				sync <- true
			} else {
				sync <- false
			}
		}
		done := <-sync
		if done {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(r.actionChan)
}

// Single goroutine used to read and modify Reaml data.
func (r *Realm) run() {
	for action := range r.actionChan {
		action()
	}
}

// bridgeSession creates and starts a session that runs in this realm, and
// returns the session's Peer for communicating with the running session.
//
// This is used for creating a local client for publishing meta events, and for
// the router to create local sessions used by an application that the router
// is embedded in.
func (r *Realm) bridgeSession(details map[string]interface{}) (wamp.Peer, error) {
	peerA, peerB := LinkedPeers()
	if details == nil {
		details = map[string]interface{}{}
	}

	// This session is the local leg of the router uplink.
	sess := Session{
		Peer:    peerA,
		ID:      wamp.GlobalID(),
		Details: details,
		kill:    make(chan wamp.URI, 1),
	}
	go r.handleSession(&sess)
	log.Println("Created internal session:", sess)

	// Return the session that is the remote leg of the router uplink.
	return peerB, nil
}

// onJoin is called when a session joins this realm.  The session is stored in
// the realm's clients and a meta event is published.
func (r *Realm) onJoin(sess *Session) {
	sync := make(chan struct{})
	r.actionChan <- func() {
		r.clients[sess.ID] = sess

		r.metaClient.Send(&wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventSessionOnJoin,
			Arguments: []interface{}{sess.Details},
		})

		sync <- struct{}{}
	}
	<-sync
}

// onLeave is called when a session leaves this realm.  The session is removed
// from the realm's clients and a meta event is published.
func (r *Realm) onLeave(sess *Session) {
	r.actionChan <- func() {
		delete(r.clients, sess.ID)
		r.dealer.RemoveSession(sess)
		r.broker.RemoveSession(sess)

		r.metaClient.Send(&wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventSessionOnLeave,
			Arguments: []interface{}{sess.ID},
		})
	}
}

/*
func (r *Realm) sessionCount(sess *Session, msg *wamp.Invocation) {
	var nclients int
	sycn := make(chan struct{})
	r.actionChan <- func() {
		nclients = len(r.clients)
		sync <- struct{}{}
	}
	<-sync
	sess.Send(&wamp.Yield{Request: msg.Request, Arguments: []int{nclients}})
}
*/

// handleSession starts a session attached to this realm.
//
// Routing occurs only between WAMP Sessions that have joined the same Realm.
func (r *Realm) handleSession(sess *Session) {
	// Add the client session the realm and send meta event.
	r.onJoin(sess)
	// Remove client session from realm, and send meta event when handler done.
	defer r.onLeave(sess)

	recvChan := sess.Recv()
	for {
		var msg wamp.Message
		var open bool
		select {
		case msg, open = <-recvChan:
			if !open {
				log.Println("lost session:", sess)
				return
			}
		case reason := <-sess.kill:
			sess.Send(&wamp.Goodbye{
				Reason:  reason,
				Details: map[string]interface{}{},
			})
			log.Printf("kill session %s: %v", sess, reason)
			return
		}

		// Debug
		log.Printf("sssion %s %s: %+v", sess, msg.MessageType(), msg)

		if isAuthz, err := r.authorizer.Authorize(sess, msg); !isAuthz {
			errMsg := &wamp.Error{Type: msg.MessageType()}
			// Get the Request from request types of messages.
			switch msg := msg.(type) {
			case *wamp.Publish:
				errMsg.Request = msg.Request
			case *wamp.Subscribe:
				errMsg.Request = msg.Request
			case *wamp.Unsubscribe:
				errMsg.Request = msg.Request
			case *wamp.Register:
				errMsg.Request = msg.Request
			case *wamp.Unregister:
				errMsg.Request = msg.Request
			case *wamp.Call:
				errMsg.Request = msg.Request
			case *wamp.Yield:
				errMsg.Request = msg.Request
			}
			if err != nil {
				// Error trying to authorize.
				errMsg.Error = wamp.ErrAuthorizationFailed
				log.Printf("client", sess, "authorization failed:", err)
			} else {
				// Session not authorized.
				errMsg.Error = wamp.ErrNotAuthorized
				log.Printf("client", sess, msg.MessageType(), "UNAUTHORIZED")
			}
			sess.Send(errMsg)
			continue
		}

		switch msg := msg.(type) {
		// Dispatch to broker.
		case *wamp.Publish:
			r.broker.Publish(sess, msg)
		case *wamp.Subscribe:
			r.broker.Subscribe(sess, msg)
		case *wamp.Unsubscribe:
			r.broker.Unsubscribe(sess, msg)

		// Dispatch to dealer.
		case *wamp.Register:
			r.dealer.Register(sess, msg)
		case *wamp.Unregister:
			r.dealer.Unregister(sess, msg)
		case *wamp.Call:
			r.dealer.Call(sess, msg)
		case *wamp.Yield:
			r.dealer.Yield(sess, msg)

		// Error messages.
		case *wamp.Error:
			// An INVOCATION error is the only type of ERROR message the
			// router should receive.
			if msg.Type == wamp.INVOCATION {
				r.dealer.Error(sess, msg)
			} else {
				log.Println("session", sess, "invalid ERROR message received:",
					msg)
			}

		case *wamp.Goodbye:
			sess.Send(&wamp.Goodbye{
				Reason:  wamp.ErrGoodbyeAndOut,
				Details: map[string]interface{}{},
			})
			log.Println("session", sess, "goodbye:", msg.Reason)
			return

		default:
			log.Println("session", sess, "unhandled message:", msg.MessageType())
		}
	}
}

// authClient authenticates the client according to the authmethods in the
// HELLO message details and the authenticators available for this realm.
func (r *Realm) authClient(client wamp.Peer, details map[string]interface{}) (*wamp.Welcome, error) {
	// The JSON unmarshaller always gives []interface{}. Other serializers may
	// preserve more of original type.  Assume that each authmethods is a slice
	// of something that can be type-asserted to a string.
	var authmethods []string
	if _authmethods, ok := details["authmethods"]; ok {
		switch _authmethods := _authmethods.(type) {
		case []string:
			authmethods = _authmethods
		case []interface{}:
			for _, x := range _authmethods {
				authmethods = append(authmethods, x.(string))
			}
		}
	}
	if len(authmethods) == 0 {
		return nil, errors.New("No authentication supplied")
	}

	authr, crAuthr := r.getAuthenticator(authmethods)
	if authr != nil {
		// Return welcome message or error.
		return authr.Authenticate(details)
	}

	var pendingCRAuth auth.PendingCRAuth
	var err error
	if crAuthr != nil {
		pendingCRAuth, err = crAuthr.Challenge(details)
		if err != nil {
			return nil, err
		}
	}

	if pendingCRAuth == nil {
		return nil, errors.New("could not authenticate with any method")
	}

	// Challenge response needed.  Send CHALLENGE message to client.
	log.Println("sending auth challenge to client")
	client.Send(pendingCRAuth.Msg())

	// Read AUTHENTICATE response from client.
	msg, err := wamp.RecvTimeout(client, pendingCRAuth.Timeout())
	if err != nil {
		return nil, err
	}
	authRsp, ok := msg.(*wamp.Authenticate)
	if !ok {
		return nil, fmt.Errorf("unexpected %v message received ",
			msg.MessageType())
	}
	log.Println("received", authRsp.MessageType(), "response from client")

	return pendingCRAuth.Authenticate(authRsp)
}

// getAuthenticator finds the first authenticator registered for the methods.
func (r *Realm) getAuthenticator(methods []string) (auth auth.Authenticator, crAuth auth.CRAuthenticator) {
	sync := make(chan struct{})
	r.actionChan <- func() {
		// Iterate through the methods and see if there is an Authenticator or
		// a CRAuthenticator for the method.
		for _, method := range methods {
			if len(r.authenticators) != 0 {
				if a, ok := r.authenticators[method]; ok {
					auth = a
					break
				}
			}
			if len(r.crAuthenticators) != 0 {
				if a, ok := r.crAuthenticators[method]; ok {
					crAuth = a
					break
				}
			}
		}
		sync <- struct{}{}
	}
	<-sync
	return
}
