package router

import (
	"errors"
	"fmt"
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

	// Session meta-procedure registration ID -> handler map.
	metaProcMap map[wamp.ID]func(*wamp.Invocation) wamp.Message
	metaDone    chan struct{}
}

// NewRealm creates a new Realm with default broker, dealer, and authorizer
// implementtions.  The Realm has no authorizers unless anonymousAuth is true.
func NewRealm(uri wamp.URI, strictURI, anonymousAuth, allowDisclose bool) *Realm {
	r := &Realm{
		uri:        uri,
		broker:     NewBroker(strictURI, allowDisclose),
		authorizer: NewAuthorizer(),
		clients:    map[wamp.ID]*Session{},
		actionChan: make(chan func()),
		metaIDGen:  wamp.NewIDGen(),
		metaDone:   make(chan struct{}),
	}
	// If allowing anonymous authentication, then install the anonymous
	// authenticator.  Install this first so that it is replaced in case a
	// custom anonymous authenticator is supplied.
	if anonymousAuth {
		r.authenticators = map[string]auth.Authenticator{
			"anonymous": auth.AnonymousAuth}
	}

	// Create a session that bridges two peers.  Meta events are published by
	// the peer returned, which is the remote side of the router uplink.
	// Sending a PUBLISH message to p will result in the router publishing the
	// event to any subscribers.
	p, _ := r.bridgeSession(nil, true)
	r.metaClient = p

	r.dealer = NewDealer(strictURI, allowDisclose, p)

	// Create map of registration ID to meta procedure handler.
	r.metaProcMap = make(map[wamp.ID]func(*wamp.Invocation) wamp.Message, 9)

	// Register to handle session meta procedures.
	regID := r.registerMetaProcedure(wamp.MetaProcSessionCount, p)
	r.metaProcMap[regID] = r.sessionCount

	regID = r.registerMetaProcedure(wamp.MetaProcSessionList, p)
	r.metaProcMap[regID] = r.sessionList

	regID = r.registerMetaProcedure(wamp.MetaProcSessionGet, p)
	r.metaProcMap[regID] = r.sessionGet

	// Register to handle registraton meta procedures.

	regID = r.registerMetaProcedure(wamp.MetaProcRegList, p)
	r.metaProcMap[regID] = r.regList

	regID = r.registerMetaProcedure(wamp.MetaProcRegLookup, p)
	r.metaProcMap[regID] = r.regLookup

	regID = r.registerMetaProcedure(wamp.MetaProcRegMatch, p)
	r.metaProcMap[regID] = r.regMatch

	regID = r.registerMetaProcedure(wamp.MetaProcRegGet, p)
	r.metaProcMap[regID] = r.regGet

	regID = r.registerMetaProcedure(wamp.MetaProcRegListCallees, p)
	r.metaProcMap[regID] = r.regListCallees

	regID = r.registerMetaProcedure(wamp.MetaProcRegCountCallees, p)
	r.metaProcMap[regID] = r.regCountCallees

	go r.metaProcedureHandler()
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
	log.Printf("Added authenticator for method %s (realm=%v)", method, r.uri)
}

// AddCRAuthenticator registers the CRAuthenticator for the specified method.
func (r *Realm) AddCRAuthenticator(method string, athr auth.CRAuthenticator) {
	r.actionChan <- func() {
		if r.crAuthenticators == nil {
			r.crAuthenticators = map[string]auth.CRAuthenticator{}
		}
		r.crAuthenticators[method] = athr
	}
	log.Printf("Added CR authenticator for method %s (realm=%v)", method, r.uri)
}

func (r *Realm) SetAuthorizer(authorizer Authorizer) {
	r.actionChan <- func() {
		r.authorizer = authorizer
	}
	log.Print("Set authorizer for realm ", r.uri)
}

// DelAuthenticator deletes the Authenticator for the specified method.
func (r *Realm) DelAuthenticator(method string) {
	r.actionChan <- func() {
		delete(r.authenticators, method)
	}
	log.Printf("Deleted authenticator for method %s (realm=%v)", method, r.uri)
}

// DelCRAuthenticator deletes the CRAuthenticator for the specified method.
func (r *Realm) DelCRAuthenticator(method string) {
	r.actionChan <- func() {
		delete(r.crAuthenticators, method)
	}
	//log.Print("Deleted CR authenticator for method: ", method)
	log.Printf("Deleted CR authenticator for method %s (realm=%v)", method, r.uri)
}

// closeRealm kills all clients, causing them to send a goodbye message.  This
// is only called from the router's single action goroutine, so will never be
// called by multiple goroutines.
func (r *Realm) closeRealm() {
	// Stop broker and dealer so they can be GC'd, and then so can this realm.
	defer r.broker.Close()
	defer r.dealer.Close()

	r.actionChan <- func() {
		for _, client := range r.clients {
			client.stop <- wamp.ErrSystemShutdown
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
	// stop signal.  When the meta client receives GOODBYE from the meta
	// session, this means the meta session is done and will not try to publish
	// anything more to the broker, and it is finally save to exit and close
	// the broker.
	r.metaSess.stop <- wamp.ErrSystemShutdown
	<-r.metaDone
	log.Println("Realm", r.uri, "completed shutdown")
}

// Single goroutine used to read and modify Reaml data.
func (r *Realm) run() {
	for action := range r.actionChan {
		action()
	}
	log.Println("Realm", r.uri, "stopped")
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
	details = wamp.SetOption(details, "authrole", "trusted")

	// This session is the local leg of the router uplink.
	sess := Session{
		Peer:    rtr,
		ID:      wamp.GlobalID(),
		Details: details,
		stop:    make(chan wamp.URI, 1),
	}
	// Run the session handler for the
	go r.handleSession(&sess, meta)
	log.Print("Created internal session: ", sess)

	// Return the session that is the remote leg of the router uplink.
	return cli, nil
}

// onJoin is called when a session joins this realm.  The session is stored in
// the realm's clients and a meta event is published.
//
// Note: onJoin() must be called from outside handleSession() so that it is not
// called for the meta client.
func (r *Realm) onJoin(sess *Session) {
	r.waitHandlers.Add(1)
	sync := make(chan struct{})
	r.actionChan <- func() {
		r.clients[sess.ID] = sess
		sync <- struct{}{}
	}
	<-sync

	// The event payload consists of a single positional argument details|dict.
	details := map[string]interface{}{
		"session":      sess.ID,
		"authid":       wamp.OptionString(sess.Details, "authid"),
		"authrole":     wamp.OptionString(sess.Details, "authrole"),
		"authmethod":   wamp.OptionString(sess.Details, "authmethod"),
		"authprovider": wamp.OptionString(sess.Details, "authprovider"),
	}

	// Session Meta Events MUST be dispatched by the Router to the same realm
	// as the WAMP session which triggered the event.
	r.metaClient.Send(&wamp.Publish{
		Request:   wamp.GlobalID(),
		Topic:     wamp.MetaEventSessionOnJoin,
		Arguments: []interface{}{details},
	})
}

// onLeave is called when a session leaves this realm.  The session is removed
// from the realm's clients and a meta event is published.
//
// Note: onLeave() must be called from outside handleSession() so that it is
// not called for the meta client.
func (r *Realm) onLeave(sess *Session) {
	sync := make(chan struct{})
	r.actionChan <- func() {
		delete(r.clients, sess.ID)
		r.dealer.RemoveSession(sess)
		r.broker.RemoveSession(sess)
		sync <- struct{}{}
	}
	<-sync

	r.metaClient.Send(&wamp.Publish{
		Request:   wamp.GlobalID(),
		Topic:     wamp.MetaEventSessionOnLeave,
		Arguments: []interface{}{sess.ID},
	})

	r.waitHandlers.Done()
}

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
	log.Println("Started", sname, sess)
	defer log.Println("Ended", sname, sess)

	recvChan := sess.Recv()
	for {
		var msg wamp.Message
		var open bool
		select {
		case msg, open = <-recvChan:
			if !open {
				log.Println("Lost", sname, sess)
				return
			}
		case reason := <-sess.stop:
			log.Printf("Stop %s %s: %v", sname, sess, reason)
			sess.Send(&wamp.Goodbye{
				Reason:  reason,
				Details: map[string]interface{}{},
			})
			return
		}

		// Debug
		if DebugEnabled {
			log.Printf("%s %s submitting %s: %+v", sname, sess,
				msg.MessageType(), msg)
		}

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
			case *wamp.Cancel:
				errMsg.Request = msg.Request
			case *wamp.Yield:
				errMsg.Request = msg.Request
			}
			if err != nil {
				// Error trying to authorize.
				errMsg.Error = wamp.ErrAuthorizationFailed
				log.Println("Client", sess, "authorization failed:", err)
			} else {
				// Session not authorized.
				errMsg.Error = wamp.ErrNotAuthorized
				log.Println("Client", sess, msg.MessageType(), "UNAUTHORIZED")
			}
			sess.Send(errMsg)
			continue
		}

		switch msg.(type) {
		case *wamp.Publish, *wamp.Subscribe, *wamp.Unsubscribe:
			// Dispatch pub/sub messages to broker.
			r.broker.Submit(sess, msg)

		case *wamp.Register, *wamp.Unregister, *wamp.Call, *wamp.Yield, *wamp.Cancel:
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
			log.Println(sname, sess, "unhandled message:", msg.MessageType())
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
		return nil, errors.New("no authentication supplied")
	}

	authr, crAuthr, method := r.getAuthenticator(authmethods)
	if authr != nil {
		// Return welcome message or error.
		welcome, err := authr.Authenticate(details)
		if err != nil {
			return nil, err
		}
		welcome.Details["authmethod"] = method
		return welcome, nil
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
	log.Print("Sending auth challenge to client %v", client)
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
	log.Println("Received", authRsp.MessageType(), "response from client %v",
		client)

	welcome, err := pendingCRAuth.Authenticate(authRsp)
	if err != nil {
		return nil, err
	}
	welcome.Details["authmethod"] = method
	return welcome, nil
}

// getAuthenticator finds the first authenticator registered for the methods.
func (r *Realm) getAuthenticator(methods []string) (auth auth.Authenticator, crAuth auth.CRAuthenticator, authMethod string) {
	sync := make(chan struct{})
	r.actionChan <- func() {
		// Iterate through the methods and see if there is an Authenticator or
		// a CRAuthenticator for the method.
		for _, method := range methods {
			if len(r.authenticators) != 0 {
				if a, ok := r.authenticators[method]; ok {
					auth = a
					authMethod = method
					break
				}
			}
			if len(r.crAuthenticators) != 0 {
				if a, ok := r.crAuthenticators[method]; ok {
					crAuth = a
					authMethod = method
					break
				}
			}
		}
		sync <- struct{}{}
	}
	<-sync
	return
}

func (r *Realm) registerMetaProcedure(procedure wamp.URI, peer wamp.Peer) wamp.ID {
	peer.Send(&wamp.Register{
		Request:   r.metaIDGen.Next(),
		Procedure: procedure,
	})
	msg := <-peer.Recv()
	reg, ok := msg.(*wamp.Registered)
	if !ok {
		err, ok := msg.(*wamp.Error)
		if !ok {
			log.Println("PANIC! Received unexpected ", msg.MessageType())
			panic("cannot register metapocedure")
		}
		errMsg := fmt.Sprintf(
			"PANIC! Failed to register session meta procedure: %v", err.Error)
		if len(err.Arguments) != 0 {
			errMsg += fmt.Sprint(": ", err.Arguments[0])
		}
		log.Print(errMsg)
		panic(errMsg)
	}
	return reg.Registration
}

func (r *Realm) metaProcedureHandler() {
	defer close(r.metaDone)
	var rsp wamp.Message
	for msg := range r.metaClient.Recv() {
		switch msg := msg.(type) {
		case *wamp.Invocation:
			metaProcHandler, ok := r.metaProcMap[msg.Registration]
			if !ok {
				r.metaClient.Send(&wamp.Error{
					Type:    msg.MessageType(),
					Request: msg.Request,
					Details: map[string]interface{}{},
					Error:   wamp.ErrNoSuchProcedure,
				})
				continue
			}
			rsp = metaProcHandler(msg)
			if rsp == nil {
				// Response is nil if it was meta procedure handled by dealer.
				continue
			}
		case *wamp.Goodbye:
			log.Println("Session meta procedure handler exiting GOODBYE")
			return
		default:
			log.Print("Meta procedure received unexpected ", msg.MessageType())
		}
		r.metaClient.Send(rsp)
	}
}

func (r *Realm) sessionCount(msg *wamp.Invocation) wamp.Message {
	var filter []string
	if len(msg.Arguments) != 0 {
		filter = msg.Arguments[0].([]string)
	}
	retChan := make(chan int)

	if len(filter) == 0 {
		r.actionChan <- func() {
			retChan <- len(r.clients)
		}
	} else {
		r.actionChan <- func() {
			var nclients int
			for _, sess := range r.clients {
				authrole := wamp.OptionString(sess.Details, "authrole")
				for j := range filter {
					if filter[j] == authrole {
						nclients++
						break
					}
				}
			}
			retChan <- nclients
		}
	}
	nclients := <-retChan
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: []interface{}{nclients},
	}
}

func (r *Realm) sessionList(msg *wamp.Invocation) wamp.Message {
	var filter []string
	if len(msg.Arguments) != 0 {
		filter = msg.Arguments[0].([]string)
	}
	retChan := make(chan []wamp.ID)

	if len(filter) == 0 {
		r.actionChan <- func() {
			ids := make([]wamp.ID, len(r.clients))
			count := 0
			for sessID := range r.clients {
				ids[count] = sessID
				count++
			}
			retChan <- ids
		}
	} else {
		r.actionChan <- func() {
			var ids []wamp.ID
			for sessID, sess := range r.clients {
				authrole := wamp.OptionString(sess.Details, "authrole")
				for j := range filter {
					if filter[j] == authrole {
						ids = append(ids, sessID)
						break
					}
				}
			}
			retChan <- ids
		}
	}
	list := <-retChan
	return &wamp.Yield{Request: msg.Request, Arguments: []interface{}{list}}
}

func (r *Realm) sessionGet(msg *wamp.Invocation) wamp.Message {
	makeErr := func() *wamp.Error {
		return &wamp.Error{
			Type:    wamp.INVOCATION,
			Request: msg.Request,
			Details: map[string]interface{}{},
			Error:   wamp.ErrNoSuchSession,
		}
	}

	if len(msg.Arguments) == 0 {
		return makeErr()
	}

	sessID, ok := wamp.AsInt64(msg.Arguments[0])
	if !ok {
		return makeErr()
	}

	retChan := make(chan *Session)
	r.actionChan <- func() {
		sess, _ := r.clients[wamp.ID(sessID)]
		retChan <- sess
	}
	sess := <-retChan
	if sess == nil {
		return makeErr()
	}
	dict := wamp.SetOption(nil, "session", sessID)
	for _, name := range []string{"authid", "authrole", "authmethod", "authprovider", "transport"} {
		opt := wamp.OptionString(sess.Details, name)
		if opt != "" {
			dict = wamp.SetOption(dict, name, opt)
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: []interface{}{dict},
	}
}

// regList retrieves registration IDs listed according to match policies.
func (r *Realm) regList(msg *wamp.Invocation) wamp.Message {
	// Submit INVOCATION message to run the registration meta procedure in the
	// dealer's request handler.  Replace the registration ID with the index of
	// the meta procedure to run.  The registration ID is not needed in this
	// case since the dealer will always respond to the meta client, and does
	// not need to lookup the registered caller to respond to.
	msg.Registration = RegList
	r.dealer.Submit(nil, msg)
	return nil
}

// regLookup retrieves registration IDs listed according to match policies.
func (r *Realm) regLookup(msg *wamp.Invocation) wamp.Message {
	msg.Registration = RegLookup
	r.dealer.Submit(nil, msg)
	return nil
}

// regMatch obtains the registration best matching a given procedure URI.
func (r *Realm) regMatch(msg *wamp.Invocation) wamp.Message {
	msg.Registration = RegMatch
	r.dealer.Submit(nil, msg)
	return nil
}

// regGet retrieves information on a particular registration.
func (r *Realm) regGet(msg *wamp.Invocation) wamp.Message {
	msg.Registration = RegGet
	r.dealer.Submit(nil, msg)
	return nil
}

// regregListCallees retrieves a list of session IDs for sessions currently
// attached to the registration.
func (r *Realm) regListCallees(msg *wamp.Invocation) wamp.Message {
	msg.Registration = RegListCallees
	r.dealer.Submit(nil, msg)
	return nil
}

// regCountCallees obtains the number of sessions currently attached to the
// registration.
func (r *Realm) regCountCallees(msg *wamp.Invocation) wamp.Message {
	msg.Registration = RegCountCallees
	r.dealer.Submit(nil, msg)
	return nil
}
