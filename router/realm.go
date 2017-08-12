package router

import (
	"errors"
	"fmt"
	"sync"

	"github.com/gammazero/nexus/auth"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/wamp"
)

type RealmConfig struct {
	URI            wamp.URI
	StrictURI      bool `json:"strict_uri"`
	AnonymousAuth  bool `json:"anonymous_auth"`
	AllowDisclose  bool `json:"auto_disclose"`
	Authenticators map[string]auth.Authenticator
	Authorizer     Authorizer
}

// A Realm is a WAMP routing and administrative domain, optionally protected by
// authentication and authorization.  WAMP messages are only routed within a
// Realm.
type realm struct {
	broker Broker
	dealer Dealer

	authorizer Authorizer

	// authmethod -> Authenticator
	authenticators map[string]auth.Authenticator

	// session ID -> Session
	clients map[wamp.ID]*Session

	metaClient *Session
	metaSess   *Session
	metaIDGen  *wamp.IDGen

	actionChan chan func()

	// Used by close() to wait for sessions to exit.
	waitHandlers sync.WaitGroup

	// Session meta-procedure registration ID -> handler map.
	metaProcMap map[wamp.ID]func(*wamp.Invocation) wamp.Message
	metaDone    chan struct{}

	closed    bool
	closeLock sync.Mutex
}

// NewRealm creates a new Realm with default broker, dealer, and authorizer
// implementtions.  The Realm has no authorizers unless anonymousAuth is true.
func NewRealm(config *RealmConfig) (*realm, error) {
	if !config.URI.ValidURI(config.StrictURI, "") {
		return nil, fmt.Errorf(
			"invalid realm URI %v (URI strict checking %v)", config.URI, config.StrictURI)
	}

	r := &realm{
		authorizer:     config.Authorizer,
		authenticators: config.Authenticators,
		clients:        map[wamp.ID]*Session{},
		actionChan:     make(chan func()),
		metaIDGen:      wamp.NewIDGen(),
		metaDone:       make(chan struct{}),
		metaProcMap:    make(map[wamp.ID]func(*wamp.Invocation) wamp.Message, 9),
	}

	if r.authorizer == nil {
		r.authorizer = NewAuthorizer()
	}

	if r.authenticators == nil {
		r.authenticators = map[string]auth.Authenticator{}

	}
	// If allowing anonymous authentication, then install the anonymous
	// authenticator.  Install this first so that it is replaced in case a
	// custom anonymous authenticator is supplied.
	if config.AnonymousAuth {
		if _, ok := r.authenticators["anonymous"]; !ok {
			r.authenticators["anonymous"] = auth.AnonymousAuth
		}
	}

	// Create a session that bridges two peers.  Meta events are published by
	// the metaClient returned, which is the remote side of the router uplink.
	// Sending a PUBLISH message to it will result in the router publishing the
	// event to any subscribers.
	r.metaClient, r.metaSess = r.createMetaSession()

	r.broker = NewBroker(config.StrictURI, config.AllowDisclose)
	r.dealer = NewDealer(config.StrictURI, config.AllowDisclose, r.metaClient)

	// Register to handle session meta procedures.
	r.registerMetaProcedure(wamp.MetaProcSessionCount, r.sessionCount)
	r.registerMetaProcedure(wamp.MetaProcSessionList, r.sessionList)
	r.registerMetaProcedure(wamp.MetaProcSessionGet, r.sessionGet)

	// Register to handle registration meta procedures.
	r.registerMetaProcedure(wamp.MetaProcRegList, r.dealer.RegList)
	r.registerMetaProcedure(wamp.MetaProcRegLookup, r.dealer.RegLookup)
	r.registerMetaProcedure(wamp.MetaProcRegMatch, r.dealer.RegMatch)
	r.registerMetaProcedure(wamp.MetaProcRegGet, r.dealer.RegGet)
	r.registerMetaProcedure(wamp.MetaProcRegListCallees, r.dealer.RegListCallees)
	r.registerMetaProcedure(wamp.MetaProcRegCountCallees, r.dealer.RegCountCallees)

	go r.metaProcedureHandler()
	return r, nil
}

// close kills all clients, causing them to send a goodbye message.  This
// is only called from the router's single action goroutine, so will never be
// called by multiple goroutines.
//
// - Realm guarantees there are never multiple calls to broker.Close() or
// dealer.Close()
//
// - handleSession() and metaProcedureHandler() are the only things than can
// submit request to the broker and dealer.
//
// - Closing realm prevents router from starting any new sessions on realm.
//
// - When new session is starting, lock is held in mutual exclusion with
// closing realm until session is capable of receiving its exit signal.
//
// - Closing Realm waits until all realm.handleSession() and
// realm.metaProcedureHandler() have exited, thereby guaranteeing that there
// will be no more requests for broker or dealer, therefore no chance of
// submitting to closed channel.
//
// - Finally realm closes broker and dealer reqChan, which is safe. Even if
// broker or dealer are in the process of publishing meta events or calling
// meta procedures, there is no metaProcedureHandler() to submit requests. Any
// client sessions that are still active likewise have no handleSession() to
// call submit requests.
func (r *realm) close() {
	// The lock is held in mutual exclusion with the router starting any new
	// session handlers for this realm.  This prevents the router from starting
	// any new session handlers, allowing the realm can safely close after
	// waiting for all existing session handlers to exit.
	r.closeLock.Lock()
	defer r.closeLock.Unlock()
	if r.closed {
		// This realm is already closed.
		return
	}
	r.closed = true
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
}

// run must be called to start the Realm.
// It blocks so should be executed in a separate goroutine
func (r *realm) run() {
	for action := range r.actionChan {
		action()
	}
}

// createMetaSession creates and starts a session that runs in this realm, and
// returns both sides of the session.
//
// This is used for creating a local client for publishing meta events.
func (r *realm) createMetaSession() (*Session, *Session) {
	cli, rtr := transport.LinkedPeers(log)

	details := wamp.SetOption(nil, "authrole", "trusted")

	// This session is the local leg of the router uplink.
	sess := &Session{
		Peer:    rtr,
		ID:      wamp.GlobalID(),
		Details: details,
		stop:    make(chan wamp.URI, 1),
	}

	// Run the session handler for the meta session
	go r.handleInternalSession(sess)
	if DebugEnabled {
		log.Println("Started meta-session", sess)
	}

	client := &Session{
		Peer: cli,
	}

	return client, sess
}

// onJoin is called when a non-meta session joins this realm.  The session is
// stored in the realm's clients and a meta event is published.
//
// Note: onJoin() is called from handleSession() so that it is not
// called for the meta client.
func (r *realm) onJoin(sess *Session) {
	r.waitHandlers.Add(1)
	sync := make(chan struct{})
	r.actionChan <- func() {
		r.clients[sess.ID] = sess
		sync <- struct{}{}
	}
	<-sync

	// The event payload consists of a single positional argument details|dict.
	details := wamp.Dict{
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
		Arguments: wamp.List{details},
	})
}

// onLeave is called when a non-meta session leaves this realm.  The session is
// removed from the realm's clients and a meta event is published.
//
// Note: onLeave() must be called from outside handleSession() so that it is
// not called for the meta client.
func (r *realm) onLeave(sess *Session) {
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
		Arguments: wamp.List{sess.ID},
	})

	r.waitHandlers.Done()
}

// HandleSession starts a session attached to this realm.
//
func (r *realm) handleSession(sess *Session) error {
	// The lock is held in mutual exclusion with the closing of the realm.
	// This ensures that no new session handler can start once the realm is
	// closing, during which the realm waits for all existing session handlers
	// to exit.
	r.closeLock.Lock()
	if r.closed {
		r.closeLock.Unlock()
		err := errors.New("realm closed")
		return err
	}

	// Ensure sesson is capable of receiving exit signal before releasing lock.
	r.onJoin(sess)
	r.closeLock.Unlock()

	if DebugEnabled {
		log.Println("Started session", sess)
	}
	go func() {
		r.handleInternalSession(sess)
		r.onLeave(sess)
		sess.Close()
	}()

	return nil
}

// handleInternalSession a session attached to this realm.
//
// Routing occurs only between WAMP Sessions that have joined the same Realm.
func (r *realm) handleInternalSession(sess *Session) {
	if DebugEnabled {
		defer log.Println("Ended sesion", sess)
	}
	recvChan := sess.Recv()
	for {
		var msg wamp.Message
		var open bool
		select {
		case msg, open = <-recvChan:
			if !open {
				log.Println("Lost", sess)
				return
			}
		case reason := <-sess.stop:
			if DebugEnabled {
				log.Printf("Stop session %s: %v", sess, reason)
			}
			sess.Send(&wamp.Goodbye{
				Reason:  reason,
				Details: wamp.Dict{},
			})
			return
		}

		// Debug
		if DebugEnabled {
			log.Printf("Session %s submitting %s: %+v", sess,
				msg.MessageType(), msg)
		}

		// N.B. meta session is always authorized
		if sess != r.metaSess {
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
		}

		switch msg := msg.(type) {
		case *wamp.Publish:
			r.broker.Publish(sess, msg)
		case *wamp.Subscribe:
			r.broker.Subscribe(sess, msg)
		case *wamp.Unsubscribe:
			r.broker.Unsubscribe(sess, msg)

		case *wamp.Register:
			r.dealer.Register(sess, msg)
		case *wamp.Unregister:
			r.dealer.Unregister(sess, msg)
		case *wamp.Call:
			r.dealer.Call(sess, msg)
		case *wamp.Yield:
			r.dealer.Yield(sess, msg)
		case *wamp.Cancel:
			r.dealer.Cancel(sess, msg)

		case *wamp.Error:
			// An INVOCATION error is the only type of ERROR message the
			// router should receive.
			if msg.Type == wamp.INVOCATION {
				r.dealer.Error(msg)
			} else {
				log.Printf("Invalid ERROR received from session %v: %v",
					sess, msg)
			}

		case *wamp.Goodbye:
			// Handle client leaving realm.
			sess.Send(&wamp.Goodbye{
				Reason:  wamp.ErrGoodbyeAndOut,
				Details: wamp.Dict{},
			})
			if DebugEnabled {
				log.Println("GOODBYE from session", sess, "reason:",
					msg.Reason)
			}
			return

		default:
			// Received unrecognized message type.
			log.Println("Unhandled", msg.MessageType(), "from session", sess)
		}
	}
}

// authClient authenticates the client according to the authmethods in the
// HELLO message details and the authenticators available for this realm.
func (r *realm) authClient(client wamp.Peer, details wamp.Dict) (*wamp.Welcome, error) {
	var authmethods []string
	if _authmethods, ok := details["authmethods"]; ok {
		switch _authmethods := _authmethods.(type) {
		case []string:
			authmethods = _authmethods
		case wamp.List:
			for _, x := range _authmethods {
				authmethods = append(authmethods, x.(string))
			}
		}
	}
	if len(authmethods) == 0 {
		return nil, errors.New("no authentication supplied")
	}

	authr, method := r.getAuthenticator(authmethods)
	if authr == nil {
		return nil, errors.New("could not authenticate with any method")
	}

	// Return welcome message or error.
	welcome, err := authr.Authenticate(details, client)
	if err != nil {
		return nil, err
	}
	welcome.Details["authmethod"] = method
	welcome.Details["roles"] = wamp.Dict{
		"broker": r.broker.Features(),
		"dealer": r.dealer.Features(),
	}
	return welcome, nil
}

// getAuthenticator finds the first authenticator registered for the methods.
func (r *realm) getAuthenticator(methods []string) (auth auth.Authenticator, authMethod string) {
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
		}
		sync <- struct{}{}
	}
	<-sync
	return
}

func (r *realm) registerMetaProcedure(procedure wamp.URI, f func(*wamp.Invocation) wamp.Message) {
	r.metaClient.Send(&wamp.Register{
		Request:   r.metaIDGen.Next(),
		Procedure: procedure,
	})
	msg := <-r.metaClient.Recv()
	reg, ok := msg.(*wamp.Registered)
	if !ok {
		err, ok := msg.(*wamp.Error)
		if !ok {
			log.Println("PANIC! Received unexpected", msg.MessageType())
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
	r.metaProcMap[reg.Registration] = f
}

func (r *realm) metaProcedureHandler() {
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
					Details: wamp.Dict{},
					Error:   wamp.ErrNoSuchProcedure,
				})
				continue
			}
			rsp = metaProcHandler(msg)
		case *wamp.Goodbye:
			if DebugEnabled {
				log.Print("Session meta procedure handler exiting GOODBYE")
			}
			return
		default:
			log.Println("Meta procedure received unexpected", msg.MessageType())
		}
		r.metaClient.Send(rsp)
	}
}

func (r *realm) sessionCount(msg *wamp.Invocation) wamp.Message {
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
		Arguments: wamp.List{nclients},
	}
}

func (r *realm) sessionList(msg *wamp.Invocation) wamp.Message {
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
	return &wamp.Yield{Request: msg.Request, Arguments: wamp.List{list}}
}

func (r *realm) sessionGet(msg *wamp.Invocation) wamp.Message {
	makeErr := func() *wamp.Error {
		return &wamp.Error{
			Type:    wamp.INVOCATION,
			Request: msg.Request,
			Details: wamp.Dict{},
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
		if opt, ok := sess.Details[name]; ok {
			dict = wamp.SetOption(dict, name, opt)
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{dict},
	}
}
