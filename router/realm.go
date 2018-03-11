package router

import (
	"errors"
	"fmt"
	"sync"

	"github.com/gammazero/nexus/router/auth"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport"
	"github.com/gammazero/nexus/wamp"
)

// RealmConfig configures a single realm in the router.  The router
// configuration may specify a list of realms to configure.
type RealmConfig struct {
	// URI that identifies the realm.
	URI wamp.URI
	// Enforce strict URI format validation.
	StrictURI bool `json:"strict_uri"`
	// Allow anonymous authentication.  Does not require any Authenticators.
	AnonymousAuth bool `json:"anonymous_auth"`
	// Allow publisher and caller identity disclosure when requested.
	AllowDisclose bool `json:"allow_disclose"`
	// Slice of Authenticator interfaces.
	Authenticators []auth.Authenticator
	// Authorizer called for each message.
	Authorizer Authorizer
	// Require authentication for local clients.  Normally local clients are
	// always trusted.  Setting this treats local clients same as others.
	RequireLocalAuth bool `json:"require_local_auth"`
}

// A Realm is a WAMP routing and administrative domain, optionally protected by
// authentication and authorization.  WAMP messages are only routed within a
// Realm.
type realm struct {
	broker *Broker
	dealer *Dealer

	authorizer Authorizer

	// authmethod -> Authenticator
	authenticators map[string]auth.Authenticator

	// session ID -> Session
	clients    map[wamp.ID]*wamp.Session
	clientStop chan struct{}

	metaPeer  wamp.Peer
	metaSess  *wamp.Session
	metaIDGen *wamp.IDGen

	actionChan chan func()

	// Used by close() to wait for sessions to exit.
	waitHandlers sync.WaitGroup

	// Session meta-procedure registration ID -> handler map.
	metaProcMap map[wamp.ID]func(*wamp.Invocation) wamp.Message
	metaStop    chan struct{}
	metaDone    chan struct{}

	closed    bool
	closeLock sync.Mutex

	log   stdlog.StdLog
	debug bool

	localAuth bool
}

// newRealm creates a new realm with the given RealmConfig, broker and dealer.
func newRealm(config *RealmConfig, broker *Broker, dealer *Dealer, logger stdlog.StdLog, debug bool) (*realm, error) {
	if !config.URI.ValidURI(config.StrictURI, "") {
		return nil, fmt.Errorf(
			"invalid realm URI %v (URI strict checking %v)", config.URI, config.StrictURI)
	}

	r := &realm{
		broker:      broker,
		dealer:      dealer,
		authorizer:  config.Authorizer,
		clients:     map[wamp.ID]*wamp.Session{},
		clientStop:  make(chan struct{}),
		actionChan:  make(chan func()),
		metaIDGen:   new(wamp.IDGen),
		metaStop:    make(chan struct{}),
		metaDone:    make(chan struct{}),
		metaProcMap: make(map[wamp.ID]func(*wamp.Invocation) wamp.Message, 9),
		log:         logger,
		debug:       debug,
		localAuth:   config.RequireLocalAuth,
	}

	r.authenticators = map[string]auth.Authenticator{}
	for _, auth := range config.Authenticators {
		r.authenticators[auth.AuthMethod()] = auth
	}

	// If allowing anonymous authentication, then install the anonymous
	// authenticator.  Install this first so that it is replaced in case a
	// custom anonymous authenticator is supplied.
	if config.AnonymousAuth {
		if _, ok := r.authenticators["anonymous"]; !ok {
			r.authenticators["anonymous"] = auth.AnonymousAuth
		}
	}

	return r, nil
}

// waitReady waits for the realm to be fully initialized and running.
func (r *realm) waitReady() {
	sync := make(chan struct{})
	r.actionChan <- func() {
		close(sync)
	}
	<-sync
}

// close performs an orderly shutdown of the realm.
//
// First a lock is acquired that prevents any new clients from joining the
// realm and makes sure any clients already in the process of joining finish
// joining.
//
// Next, each client session is killed, removing it from the broker and dealer,
// triggering a GOODBYE message to the client, and causing the session's
// message handler to exit.  This ensures there are no messages remaining to be
// sent to the router.
//
// After that, the meta client session is killed.  This ensures there are no
// more meta messages to sent to the router.
//
// At this point the broker and dealer are shutdown since they cannot receive
// any more messages to route, and have no clients to route messages to.
//
// Finally, the realm's action channel is closed and its goroutine is stopped.
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

	// Make sure that realm is fully initialized, by checking that it is
	// running, before closing.
	r.waitReady()

	// Kick all clients off.  Clients will not generate meta events when the
	// ErrSystesShutdown reason is given.
	close(r.clientStop)

	// Wait until each client's handleInboundMessages() has exited.  No new
	// messages can be generated once sessions are closed.
	r.waitHandlers.Wait()

	// All normal handlers have exited, so now stop the meta session.  When
	// the meta client receives GOODBYE from the meta session, the meta
	// session is done and will not try to publish anything more to the
	// broker, and it is finally safe to exit and close the broker.
	close(r.metaStop)
	<-r.metaDone

	// handleInboundMessages() and metaProcedureHandler() are the only things
	// than can submit request to the broker and dealer, so now that these are
	// finished there can be no more messages to broker and dealer.

	// No new messages, so safe to close dealer and broker.  Stop broker and
	// dealer so they can be GC'd, and then so can this realm.
	r.dealer.Close()
	r.broker.Close()

	// Finally close realm's action channel.
	close(r.actionChan)
}

// run must be called to start the Realm.
// It blocks so should be executed in a separate goroutine
func (r *realm) run() {
	// Create a local client for publishing meta events.
	r.createMetaSession()

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

	for action := range r.actionChan {
		action()
	}
}

// createMetaSession creates and starts a session that runs in this realm, and
// bridges two peers.  One peer, the r.metaSess, is associated with the router
// and handles meta session requests.  The other, r.metaPeer, is the remote
// side of the router uplink and is used as the interface to send meta session
// messages to.  Sending a PUBLISH message to it will result in the router
// publishing the event to any subscribers.
func (r *realm) createMetaSession() {
	cli, rtr := transport.LinkedPeers()
	r.metaPeer = cli
	r.dealer.SetMetaPeer(cli)

	details := wamp.SetOption(nil, "authrole", "trusted")

	// This session is the local leg of the router uplink.
	r.metaSess = &wamp.Session{
		Peer:    rtr,
		ID:      wamp.GlobalID(),
		Details: details,
	}

	// Run the handler for messages from the meta session.
	go r.handleInboundMessages(r.metaSess)
	if r.debug {
		r.log.Println("Started meta-session", r.metaSess)
	}
}

// onJoin is called when a non-meta session joins this realm.  The session is
// stored in the realm's clients and a meta event is published.
//
// Note: onJoin() is called from handleSession, not handleInboundMessages, so
// that it is not called for the meta client.
func (r *realm) onJoin(sess *wamp.Session) {
	r.waitHandlers.Add(1)
	sync := make(chan struct{})
	r.actionChan <- func() {
		r.clients[sess.ID] = sess
		close(sync)
	}
	<-sync

	// Session Meta Events MUST be dispatched by the Router to the same realm
	// as the WAMP session which triggered the event.
	//
	// WAMP spec only specifies publishing "authid", "authrole", "authmethod",
	// "authprovider", "transport".  This implementation publishes all details.
	r.metaPeer.Send(&wamp.Publish{
		Request:   wamp.GlobalID(),
		Topic:     wamp.MetaEventSessionOnJoin,
		Arguments: wamp.List{sess.Details},
	})
}

// onLeave is called when a non-meta session leaves this realm.  The session is
// removed from the realm's clients and a meta event is published.
//
// If the session handler exited due to realm shutdown, then remove the session
// from broker, dealer, and realm without generating meta events.  If not
// shutdown, then remove the session and generate meta events as appropriate.
//
// There is no point to generating meta events at realm shutdown since those
// events would only be received by meta event subscribers that had not been
// removed yet, and clients are removed in any order.
//
// Note: onLeave() must be called from outside handleInboundMessages so that it
// is not called for the meta client.
func (r *realm) onLeave(sess *wamp.Session, shutdown bool) {
	sync := make(chan struct{})
	r.actionChan <- func() {
		delete(r.clients, sess.ID)
		// If realm is shutdown, do not bother to remove session from broker
		// and dealer.  They will be closed after sessions are closed.
		if !shutdown {
			r.dealer.RemoveSession(sess)
			r.broker.RemoveSession(sess)
		}
		close(sync)
	}
	<-sync

	if !shutdown {
		r.metaPeer.Send(&wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventSessionOnLeave,
			Arguments: wamp.List{sess.ID},
		})
	}

	r.waitHandlers.Done()
}

// HandleSession starts a session attached to this realm.
//
// Routing occurs only between WAMP Sessions that have joined the same Realm.
func (r *realm) handleSession(sess *wamp.Session) error {
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

	// Ensure session is capable of receiving exit signal before releasing lock
	r.onJoin(sess)
	r.closeLock.Unlock()

	if r.debug {
		r.log.Println("Started session", sess)
	}
	go func() {
		shutdown := r.handleInboundMessages(sess)
		r.onLeave(sess, shutdown)
		sess.Close()
	}()

	return nil
}

// handleInboundMessages handles the messages sent from a client session to
// the router.
func (r *realm) handleInboundMessages(sess *wamp.Session) bool {
	if r.debug {
		defer r.log.Println("Ended session", sess)
	}
	stopChan := r.clientStop
	if sess == r.metaSess {
		stopChan = r.metaStop
	}
	recvChan := sess.Recv()
	for {
		var msg wamp.Message
		var open bool
		select {
		case msg, open = <-recvChan:
			if !open {
				r.log.Println("Lost", sess)
				return false
			}
		case <-stopChan:
			if r.debug {
				r.log.Printf("Stop session %s: system shutdown", sess)
			}
			sess.TrySend(&wamp.Goodbye{
				Reason:  wamp.ErrSystemShutdown,
				Details: wamp.Dict{},
			})
			return true
		}

		if r.debug {
			r.log.Printf("Session %s submitting %s: %+v", sess,
				msg.MessageType(), msg)
		}

		// Note: meta session is always authorized
		if r.authorizer != nil && sess != r.metaSess && !r.authzMessage(sess, msg) {
			// Not authorized; error response sent; do not process message.
			continue
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
				r.log.Printf("Invalid ERROR received from session %v: %v",
					sess, msg)
			}

		case *wamp.Goodbye:
			// Handle client leaving realm.
			sess.TrySend(&wamp.Goodbye{
				Reason:  wamp.ErrGoodbyeAndOut,
				Details: wamp.Dict{},
			})
			if r.debug {
				r.log.Println("GOODBYE from session", sess, "reason:",
					msg.Reason)
			}
			return false

		default:
			// Received unrecognized message type.
			r.log.Println("Unhandled", msg.MessageType(), "from session", sess)
		}
	}
}

// authzMessage checks if the session is authroized to send the message.  If
// authorization fails or if the session is not authorized, then an error
// response is returned to the client, and this method returns false.
func (r *realm) authzMessage(sess *wamp.Session, msg wamp.Message) bool {
	isAuthz, err := r.authorizer.Authorize(sess, msg)
	if !isAuthz {
		errRsp := &wamp.Error{Type: msg.MessageType()}
		// Get the Request from request types of messages.
		switch msg := msg.(type) {
		case *wamp.Publish:
			errRsp.Request = msg.Request
		case *wamp.Subscribe:
			errRsp.Request = msg.Request
		case *wamp.Unsubscribe:
			errRsp.Request = msg.Request
		case *wamp.Register:
			errRsp.Request = msg.Request
		case *wamp.Unregister:
			errRsp.Request = msg.Request
		case *wamp.Call:
			errRsp.Request = msg.Request
		case *wamp.Cancel:
			errRsp.Request = msg.Request
		case *wamp.Yield:
			errRsp.Request = msg.Request
		}
		if err != nil {
			// Error trying to authorize.  Include error message.
			errRsp.Error = wamp.ErrAuthorizationFailed
			errRsp.Arguments = wamp.List{err.Error()}
			r.log.Println("Client", sess, "authorization failed:", err)
		} else {
			// Session not authorized.  The inability to return a message is
			// intentional, so as not to encourage returning information that
			// could disclose any clues about authorization to an attacker.
			errRsp.Error = wamp.ErrNotAuthorized
			r.log.Println("Client", sess, msg.MessageType(), "not authorized")
		}
		err = sess.TrySend(errRsp)
		if err != nil {
			r.log.Println("!!! client blocked, could not send authz error")
		}
		return false
	}
	return true
}

// authClient authenticates the client according to the authmethods in the
// HELLO message details and the authenticators available for this realm.
func (r *realm) authClient(sid wamp.ID, client wamp.Peer, details wamp.Dict) (*wamp.Welcome, error) {
	// If the client is local, then no authentication is required.
	if transport.IsLocal(client) && !r.localAuth {
		// Create welcome details for local client.
		authid, _ := wamp.AsString(details["authid"])
		if authid == "" {
			authid = string(wamp.GlobalID())
		}
		details = wamp.Dict{
			"authid":       authid,
			"authrole":     "trusted",
			"authmethod":   "local",
			"authprovider": "static",
			"roles": wamp.Dict{
				"broker": r.broker.Role(),
				"dealer": r.dealer.Role(),
			},
		}
		return &wamp.Welcome{Details: details}, nil
	}

	// The default authentication method is "WAMP-Anonymous" if client does not
	// specify otherwise.
	if _, ok := details["authmethods"]; !ok {
		if details == nil {
			details = wamp.Dict{}
		}
		details["authmethods"] = []string{"anonymous"}
	}

	var authmethods []string
	if _authmethods, ok := details["authmethods"]; ok {
		amList, _ := wamp.AsList(_authmethods)
		for _, x := range amList {
			am, ok := wamp.AsString(x)
			if !ok {
				r.log.Println("!! Could not convert authmethod:", x)
				continue
			}
			authmethods = append(authmethods, am)
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
	welcome, err := authr.Authenticate(sid, details, client)
	if err != nil {
		return nil, err
	}
	welcome.Details["authmethod"] = method
	welcome.Details["roles"] = wamp.Dict{
		"broker": r.broker.Role(),
		"dealer": r.dealer.Role(),
	}
	return welcome, nil
}

// getAuthenticator finds the first authenticator registered for the methods.
func (r *realm) getAuthenticator(methods []string) (auth auth.Authenticator, authMethod string) {
	sync := make(chan struct{})
	r.actionChan <- func() {
		// Iterate through the methods and see if there is an Authenticator for
		// the method.
		if len(r.authenticators) != 0 {
			for _, method := range methods {
				if a, ok := r.authenticators[method]; ok {
					auth = a
					authMethod = method
					break
				}
			}
		}
		close(sync)
	}
	<-sync
	return
}

func (r *realm) registerMetaProcedure(procedure wamp.URI, f func(*wamp.Invocation) wamp.Message) {
	r.metaPeer.Send(&wamp.Register{
		Request:   r.metaIDGen.Next(),
		Procedure: procedure,
	})
	msg := <-r.metaPeer.Recv()
	if msg == nil {
		// This would only happen if the meta client was closed before or
		// during meta procedure registration at realm startup.  Safety first.
		return
	}
	reg, ok := msg.(*wamp.Registered)
	if !ok {
		err, ok := msg.(*wamp.Error)
		if !ok {
			if _, ok = msg.(*wamp.Goodbye); ok {
				r.log.Println("Shutdown during meta procedure registration")
				return
			}
			r.log.Println("PANIC! Received unexpected", msg.MessageType())
			panic("cannot register meta procedure")
		}
		errMsg := fmt.Sprintf(
			"PANIC! Failed to register session meta procedure: %v", err.Error)
		if len(err.Arguments) != 0 {
			errMsg += fmt.Sprint(": ", err.Arguments[0])
		}
		r.log.Print(errMsg)
		panic(errMsg)
	}
	r.metaProcMap[reg.Registration] = f
}

func (r *realm) metaProcedureHandler() {
	defer close(r.metaDone)
	var rsp wamp.Message
	for msg := range r.metaPeer.Recv() {
		switch msg := msg.(type) {
		case *wamp.Invocation:
			metaProcHandler, ok := r.metaProcMap[msg.Registration]
			if !ok {
				r.metaPeer.Send(&wamp.Error{
					Type:    msg.MessageType(),
					Request: msg.Request,
					Details: wamp.Dict{},
					Error:   wamp.ErrNoSuchProcedure,
				})
				continue
			}
			rsp = metaProcHandler(msg)
		case *wamp.Goodbye:
			if r.debug {
				r.log.Print("Session meta procedure handler exiting GOODBYE")
			}
			return
		default:
			r.log.Println("Meta procedure received unexpected", msg.MessageType())
		}
		r.metaPeer.Send(rsp)
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

	retChan := make(chan *wamp.Session)
	r.actionChan <- func() {
		sess, _ := r.clients[wamp.ID(sessID)]
		retChan <- sess
	}
	sess := <-retChan
	if sess == nil {
		return makeErr()
	}

	// WAMP spec only specifies returning "authid", "authrole", "authmethod",
	// "authprovider", and "transport".  All details are returned in this
	// implementation.
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{sess.Details},
	}
}
