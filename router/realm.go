package router

import (
	"errors"
	"fmt"
	"log"
	"sync"

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
	metaSess   *Session
	metaIDGen  *wamp.IDGen

	actionChan chan func()

	// Used by Close() to wait for sessions to exit.
	waitHandlers sync.WaitGroup
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
		metaIDGen:  wamp.NewIDGen(),
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
	p, _ := r.bridgeSession(nil, true)
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
func (r *Realm) Close() {
	defer r.broker.Close()
	defer r.dealer.Close()

	r.actionChan <- func() {
		for _, client := range r.clients {
			client.kill <- wamp.ErrSystemShutdown
		}
	}

	// Wait until handleSession for all clients has exited.
	r.waitHandlers.Wait()
	close(r.actionChan)

	if r.metaSess == nil {
		return
	}
	// All normal handlers have exited.  There may still be pending meta events
	// from the session getting booted off the router.  Send the meta session a
	// kill signal.  When the meta client receives GOODBYE from the meta
	// session, this means the meta session is done and will not try to publish
	// anything more to the broker, and it is finally save to exit and close
	// the broker.
	r.metaSess.kill <- wamp.ErrSystemShutdown
	<-r.metaClient.Recv()
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
func (r *Realm) bridgeSession(details map[string]interface{}, meta bool) (wamp.Peer, error) {
	cli, rtr := LinkedPeers()
	if details == nil {
		details = map[string]interface{}{}
	} else {
		wamp.NormalizeDict(details)
	}

	// This session is the local leg of the router uplink.
	sess := Session{
		Peer:    rtr,
		ID:      wamp.GlobalID(),
		Details: details,
		kill:    make(chan wamp.URI, 1),
	}
	// Run the session handler for the
	go r.handleSession(&sess, meta)
	log.Println("Created internal session:", sess)

	// Return the session that is the remote leg of the router uplink.
	return cli, nil
}

// onJoin is called when a session joins this realm.  The session is stored in
// the realm's clients and a meta event is published.
//
// Note: onJoin() must be called from outside handleSession() so that it is not
// called for the meta client.
func (r *Realm) onJoin(sess *Session) {
	var metaID wamp.ID
	r.waitHandlers.Add(1)
	sync := make(chan struct{})
	r.actionChan <- func() {
		r.clients[sess.ID] = sess
		metaID = r.metaIDGen.Next()
		sync <- struct{}{}
	}
	<-sync

	// Session Meta Events MUST be dispatched by the Router to the same realm
	// as the WAMP session which triggered the event.
	r.metaClient.Send(&wamp.Publish{
		Request:   metaID,
		Topic:     wamp.MetaEventSessionOnJoin,
		Arguments: []interface{}{sess.Details},
	})
}

// onLeave is called when a session leaves this realm.  The session is removed
// from the realm's clients and a meta event is published.
//
// Note: onLeave() must be called from outside handleSession() so that it is
// not called for the meta client.
func (r *Realm) onLeave(sess *Session) {
	var metaID wamp.ID
	sync := make(chan struct{})
	r.actionChan <- func() {
		delete(r.clients, sess.ID)
		r.dealer.RemoveSession(sess)
		r.broker.RemoveSession(sess)
		metaID = r.metaIDGen.Next()
		sync <- struct{}{}
	}
	<-sync

	r.metaClient.Send(&wamp.Publish{
		Request:   metaID,
		Topic:     wamp.MetaEventSessionOnLeave,
		Arguments: []interface{}{sess.ID},
	})

	r.waitHandlers.Done()
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
func (r *Realm) handleSession(sess *Session, meta bool) {
	var sname string
	if !meta {
		sname = "session"
		// Add the client session the realm and send meta event.
		r.onJoin(sess)
		// Remove client session from realm, and send meta event.
		defer r.onLeave(sess)
	} else {
		sname = "meta-session"
		r.metaSess = sess
	}
	log.Println("started", sname, sess)
	defer log.Println("ended", sname, sess)

	recvChan := sess.Recv()
	for {
		var msg wamp.Message
		var open bool
		select {
		case msg, open = <-recvChan:
			if !open {
				log.Println("lost", sname, sess)
				return
			}
		case reason := <-sess.kill:
			log.Printf("kill %s %s: %v", sname, sess, reason)
			sess.Send(&wamp.Goodbye{
				Reason:  reason,
				Details: map[string]interface{}{},
			})
			return
		}

		// Debug
		log.Printf("%s %s submitting %s: %+v", sname, sess, msg.MessageType(), msg)

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
				log.Println("client", sess, "authorization failed:", err)
			} else {
				// Session not authorized.
				errMsg.Error = wamp.ErrNotAuthorized
				log.Println("client", sess, msg.MessageType(), "UNAUTHORIZED")
			}
			sess.Send(errMsg)
			continue
		}

		switch msg.(type) {
		case *wamp.Publish, *wamp.Subscribe, *wamp.Unsubscribe:
			// Dispatch pub/sub messages to broker.
			r.broker.Submit(sess, msg)

		case *wamp.Register, *wamp.Unregister, *wamp.Call, *wamp.Yield:
			// Dispatch RPC messages and invocation errors to dealer.
			r.dealer.Submit(sess, msg)

		case *wamp.Error:
			msg := msg.(*wamp.Error)
			// An INVOCATION error is the only type of ERROR message the
			// router should receive.
			if msg.Type == wamp.INVOCATION {
				r.dealer.Submit(sess, msg)
			} else {
				log.Println(sname, sess, "invalid ERROR message received:", msg)
			}

		case *wamp.Goodbye:
			// Handle client leaving realm.
			sess.Send(&wamp.Goodbye{
				Reason:  wamp.ErrGoodbyeAndOut,
				Details: map[string]interface{}{},
			})
			msg := msg.(*wamp.Goodbye)
			log.Println(sname, sess, "goodbye:", msg.Reason)
			return

		default:
			// Received unrecognized message type.
			log.Println(sname, sess, "unhandled message:",
				msg.MessageType())
		}
	}
}

// authClient authenticates the client according to the authmethods in the
// HELLO message details and the authenticators available for this realm.
func (r *Realm) authClient(client wamp.Peer, details map[string]interface{}) (*wamp.Welcome, error) {
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
