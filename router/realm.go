package router

import (
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// Special ID for meta session.
const metaID = wamp.ID(1)

type testament struct {
	topic   wamp.URI
	args    wamp.List
	kwargs  wamp.Dict
	options wamp.Dict
}
type testamentBucket struct {
	detached  []testament
	destroyed []testament
}

// A Realm is a WAMP routing and administrative domain, optionally protected by
// authentication and authorization.  WAMP messages are only routed within a
// Realm.
type realm struct {
	broker *broker
	dealer *dealer

	authorizer Authorizer

	// authmethod -> Authenticator
	authenticators map[string]auth.Authenticator

	// session ID -> Session
	clients map[wamp.ID]*wamp.Session
	// session ID -> testament
	testaments map[wamp.ID]testamentBucket

	metaPeer  wamp.Peer
	metaSess  *wamp.Session
	metaIDGen *wamp.IDGen

	actionChan chan func()

	// Used by close() to wait for sessions to exit.
	waitHandlers sync.WaitGroup

	// Session meta-procedure registration ID -> handler map.
	metaProcMap map[wamp.ID]func(*wamp.Invocation) wamp.Message
	metaDone    chan struct{}

	closed    bool
	closeLock sync.Mutex

	log   stdlog.StdLog
	debug bool

	localAuth  bool
	localAuthz bool

	metaStrict     bool
	metaIncDetails []string

	enableMetaKill   bool
	enableMetaModify bool
}

var (
	shutdownGoodbye = &wamp.Goodbye{
		Reason:  wamp.ErrSystemShutdown,
		Details: wamp.Dict{},
	}
)

// newRealm creates a new realm with the given RealmConfig, broker and dealer.
func newRealm(config *RealmConfig, broker *broker, dealer *dealer, logger stdlog.StdLog, debug bool) (*realm, error) {
	if !config.URI.ValidURI(config.StrictURI, "") {
		return nil, fmt.Errorf(
			"invalid realm URI %v (URI strict checking %v)", config.URI, config.StrictURI)
	}

	r := &realm{
		broker:      broker,
		dealer:      dealer,
		authorizer:  config.Authorizer,
		clients:     map[wamp.ID]*wamp.Session{},
		testaments:  map[wamp.ID]testamentBucket{},
		actionChan:  make(chan func()),
		metaIDGen:   new(wamp.IDGen),
		metaDone:    make(chan struct{}),
		metaProcMap: make(map[wamp.ID]func(*wamp.Invocation) wamp.Message, 9),
		log:         logger,
		debug:       debug,
		localAuth:   config.RequireLocalAuth,
		localAuthz:  config.RequireLocalAuthz,
		metaStrict:  config.MetaStrict,

		enableMetaKill:   config.EnableMetaKill,
		enableMetaModify: config.EnableMetaModify,
	}

	if debug {
		if r.enableMetaKill {
			r.log.Println("Session meta kill procedures enabled")
		}
		if r.enableMetaKill {
			r.log.Println("Session meta modify_details procedure enabled")
		}
	}
	if r.metaStrict && len(config.MetaIncludeSessionDetails) != 0 {
		r.metaIncDetails = make([]string, len(config.MetaIncludeSessionDetails))
		copy(r.metaIncDetails, config.MetaIncludeSessionDetails)
	}

	r.authenticators = map[string]auth.Authenticator{}
	for _, auth := range config.Authenticators {
		r.authenticators[auth.AuthMethod()] = auth
	}

	// If allowing anonymous authentication, then install an anonymous
	// authenticator if one has not already been provided in the config.
	if config.AnonymousAuth {
		if _, ok := r.authenticators["anonymous"]; !ok {
			r.authenticators["anonymous"] = &auth.AnonymousAuth{
				AuthRole: "anonymous",
			}
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

	// Kick all clients off.  Sending shutdownGoodbye causes client message
	// handlers to exit without sending meta events.
	sync := make(chan struct{})
	r.actionChan <- func() {
		for _, c := range r.clients {
			c.EndRecv(shutdownGoodbye)
		}
		close(sync)
	}
	<-sync

	// Wait until each client's handleInboundMessages() has exited.  No new
	// messages can be generated once sessions are closed.
	r.waitHandlers.Wait()

	// All normal handlers have exited, so now stop the meta session.  When
	// the meta client receives GOODBYE from the meta session, the meta
	// session is done and will not try to publish anything more to the
	// broker, and it is finally safe to exit and close the broker.
	r.metaSess.EndRecv(shutdownGoodbye)
	<-r.metaDone

	// handleInboundMessages() and metaProcedureHandler() are the only things
	// than can submit request to the broker and dealer, so now that these are
	// finished there can be no more messages to broker and dealer.

	// No new messages, so safe to close dealer and broker.  Stop broker and
	// dealer so they can be GC'd, and then so can this realm.
	r.dealer.close()
	r.broker.close()

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
	if r.enableMetaKill {
		r.registerMetaProcedure(wamp.MetaProcSessionKill, r.sessionKill)
		r.registerMetaProcedure(wamp.MetaProcSessionKillByAuthid, r.sessionKillByAuthid)
		r.registerMetaProcedure(wamp.MetaProcSessionKillByAuthrole, r.sessionKillByAuthrole)
		r.registerMetaProcedure(wamp.MetaProcSessionKillAll, r.sessionKillAll)
	}
	if r.enableMetaModify {
		r.registerMetaProcedure(wamp.MetaProcSessionModifyDetails, r.sessionModifyDetails)
	}
	// Register to handle registration meta procedures.
	r.registerMetaProcedure(wamp.MetaProcRegList, r.dealer.regList)
	r.registerMetaProcedure(wamp.MetaProcRegLookup, r.dealer.regLookup)
	r.registerMetaProcedure(wamp.MetaProcRegMatch, r.dealer.regMatch)
	r.registerMetaProcedure(wamp.MetaProcRegGet, r.dealer.regGet)
	r.registerMetaProcedure(wamp.MetaProcRegListCallees, r.dealer.regListCallees)
	r.registerMetaProcedure(wamp.MetaProcRegCountCallees, r.dealer.regCountCallees)

	// Register to handle subscription meta procedures.
	r.registerMetaProcedure(wamp.MetaProcSubList, r.broker.subList)
	r.registerMetaProcedure(wamp.MetaProcSubLookup, r.broker.subLookup)
	r.registerMetaProcedure(wamp.MetaProcSubMatch, r.broker.subMatch)
	r.registerMetaProcedure(wamp.MetaProcSubGet, r.broker.subGet)
	r.registerMetaProcedure(wamp.MetaProcSubListSubscribers, r.broker.subListSubscribers)
	r.registerMetaProcedure(wamp.MetaProcSubCountSubscribers, r.broker.subCountSubscribers)

	// Register to handle testament meta procedures.
	r.registerMetaProcedure(wamp.MetaProcSessionAddTestament, r.testamentAdd)
	r.registerMetaProcedure(wamp.MetaProcSessionFlushTestaments, r.testamentFlush)

	go r.metaProcedureHandler()

	for action := range r.actionChan {
		action()
	}
}

// createMetaSession creates and starts a session that runs in this realm, and
// bridges two peers.  One peer, the r.metaSess, is associated with the router
// and handles meta session requests.  The other, r.metaPeer, is the remote
// (client) side of the router uplink and is used as the interface to send meta
// session messages to.  Sending a PUBLISH message to it will result in the
// router publishing the event to any subscribers.
func (r *realm) createMetaSession() {
	cli, rtr := transport.LinkedPeers()
	r.metaPeer = cli

	r.dealer.setMetaPeer(cli)

	// This session is the local leg of the router uplink.
	r.metaSess = wamp.NewSession(rtr, metaID, wamp.Dict{"authrole": "trusted"}, nil)

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
	// WAMP spec only specifies publishing "session", "authid", "authrole",
	// "authmethod", "authprovider", "transport".  This implementation
	// publishes all details except transport.auth.
	sess.Lock()
	output := r.cleanSessionDetails(sess.Details)
	sess.Unlock()
	r.metaPeer.Send(&wamp.Publish{
		Request:   wamp.GlobalID(),
		Topic:     wamp.MetaEventSessionOnJoin,
		Arguments: wamp.List{output},
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
func (r *realm) onLeave(sess *wamp.Session, shutdown, killAll bool) {
	var testaments testamentBucket
	var hasTstm bool
	sync := make(chan struct{})
	r.actionChan <- func() {
		delete(r.clients, sess.ID)
		testaments, hasTstm = r.testaments[sess.ID]
		if hasTstm {
			delete(r.testaments, sess.ID)
		}

		// If realm is shutdown, do not bother to remove session from broker
		// and dealer.  They will be closed after sessions are closed.
		if !shutdown {
			r.dealer.removeSession(sess)
			r.broker.removeSession(sess)
		}
		close(sync)
	}
	<-sync

	defer r.waitHandlers.Done()

	if shutdown || killAll {
		return
	}
	if hasTstm {
		sendTestaments := func(testaments []testament) {
			for i := range testaments {
				r.metaPeer.Send(&wamp.Publish{
					Request:     wamp.GlobalID(),
					Topic:       testaments[i].topic,
					Arguments:   testaments[i].args,
					ArgumentsKw: testaments[i].kwargs,
					Options:     testaments[i].options,
				})
			}
		}
		sendTestaments(testaments.detached)
		sendTestaments(testaments.destroyed)
	}
	r.metaPeer.Send(&wamp.Publish{
		Request: wamp.GlobalID(),
		Topic:   wamp.MetaEventSessionOnLeave,
		Arguments: wamp.List{
			sess.ID,
			sess.Details["authid"],
			sess.Details["authrole"]},
	})
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
		shutdown, killAll, err := r.handleInboundMessages(sess)
		if err != nil {
			abortMsg := wamp.Abort{
				Reason:  wamp.ErrProtocolViolation,
				Details: wamp.Dict{"error": err.Error()},
			}
			r.log.Println("Aborting session", sess, ":", err)
			sess.TrySend(&abortMsg)
		}
		r.onLeave(sess, shutdown, killAll)
		sess.Close()
	}()

	return nil
}

// handleInboundMessages handles the messages sent from a client session to
// the router.
func (r *realm) handleInboundMessages(sess *wamp.Session) (bool, bool, error) {
	if r.debug {
		defer r.log.Println("Ended session", sess)
	}
	recv := sess.Recv()
	recvDone := sess.RecvDone()
	for {
		var msg wamp.Message
		var open bool
		select {
		case msg, open = <-recv:
			if !open {
				r.log.Println("Lost", sess)
				return false, false, nil
			}
		case <-recvDone:
			goodbye := sess.Goodbye()
			switch goodbye {
			case shutdownGoodbye, wamp.NoGoodbye:
				if r.debug {
					r.log.Printf("Stop session %s: system shutdown", sess)
				}
				sess.TrySend(goodbye)
				return true, false, nil
			}
			if r.debug {
				r.log.Printf("Kill session %s: %s", sess, goodbye.Reason)
			}
			var killAll bool
			if _, ok := goodbye.Details["all"]; ok {
				killAll = true
			}
			sess.TrySend(goodbye)
			return false, killAll, nil
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
			r.broker.publish(sess, msg)
		case *wamp.Subscribe:
			r.broker.subscribe(sess, msg)
		case *wamp.Unsubscribe:
			r.broker.unsubscribe(sess, msg)

		case *wamp.Register:
			r.dealer.register(sess, msg)
		case *wamp.Unregister:
			r.dealer.unregister(sess, msg)
		case *wamp.Call:
			r.dealer.call(sess, msg)
		case *wamp.Yield:
			r.dealer.yield(sess, msg)
		case *wamp.Cancel:
			r.dealer.cancel(sess, msg)

		case *wamp.Error:
			// An INVOCATION error is the only type of ERROR message the
			// router should receive.
			if msg.Type != wamp.INVOCATION {
				return false, false, fmt.Errorf("invalid ERROR received: %v", msg)
			}
			r.dealer.error(msg)

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
			return false, false, nil

		default:
			// Received unrecognized message type.
			return false, false, fmt.Errorf("unexpected %v", msg.MessageType())
		}
	}
}

// authzMessage checks if the session is authorized to send the message.  If
// authorization fails or if the session is not authorized, then an error
// response is returned to the client, and this method returns false.
func (r *realm) authzMessage(sess *wamp.Session, msg wamp.Message) bool {
	// If the client is local, then do not check authorization, unless
	// requested in config.
	if transport.IsLocal(sess.Peer) && !r.localAuthz {
		return true
	}

	// Create a safe session to prevent access to the session.Peer.
	safeSession := &wamp.Session{
		ID:      sess.ID,
		Details: sess.Details,
	}

	// Write-lock the session, becuase there is no telling what the Authorizer
	// will do to the session details.
	sess.Lock()
	isAuthz, err := r.authorizer.Authorize(safeSession, msg)
	sess.Unlock()

	if !isAuthz {
		skipResponse := false
		errRsp := &wamp.Error{Type: msg.MessageType()}
		// Get the Request from request types of messages.
		switch msg := msg.(type) {
		case *wamp.Publish:
			// a publish error should only be sent when OptAcknowledge is set.
			if pubAck, _ := msg.Options[wamp.OptAcknowledge].(bool); !pubAck {
				skipResponse = true
			}
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
		if !skipResponse {
			err = sess.TrySend(errRsp)
			if err != nil {
				r.log.Println("!!! client blocked, could not send authz error")
			}
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
			authid = strconv.FormatInt(int64(wamp.GlobalID()), 16)
		}
		details = wamp.Dict{
			"authid":       authid,
			"authrole":     "trusted",
			"authmethod":   "local",
			"authprovider": "static",
			"roles": wamp.Dict{
				"broker": r.broker.role(),
				"dealer": r.dealer.role(),
			},
		}
		return &wamp.Welcome{Details: details}, nil
	}

	// The default authentication method is "WAMP-Anonymous" if client does not
	// specify otherwise.
	_authmethods, _ := wamp.AsList(details["authmethods"])
	if len(_authmethods) == 0 {
		_authmethods = append(_authmethods, "anonymous")
	}

	var authmethods []string
	for _, val := range _authmethods {
		am, ok := wamp.AsString(val)
		if !ok {
			r.log.Println("!! Could not convert authmethod:", val)
			continue
		}
		if am == "" {
			continue
		}
		authmethods = append(authmethods, am)
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
		"broker": r.broker.role(),
		"dealer": r.dealer.role(),
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
	// Register the meta procedure.  The "disclose_caller" option must be
	// enabled for the testament API and the meta session API.
	r.metaPeer.Send(&wamp.Register{
		Request: r.metaIDGen.Next(),
		Options: wamp.Dict{
			"disclose_caller": true,
		},
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

// sessionCount is a session meta procedure that obtains the number of sessions
// currently attached to the realm.
func (r *realm) sessionCount(msg *wamp.Invocation) wamp.Message {
	var filter []string
	if len(msg.Arguments) != 0 {
		filterList, ok := wamp.AsList(msg.Arguments[0])
		if !ok {
			return &wamp.Error{
				Type:    wamp.INVOCATION,
				Error:   wamp.ErrInvalidArgument,
				Request: msg.Request,
			}
		}
		filter, ok = wamp.ListToStrings(filterList)
		if !ok {
			return &wamp.Error{
				Type:    wamp.INVOCATION,
				Error:   wamp.ErrInvalidArgument,
				Request: msg.Request,
			}
		}
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
				sess.Lock()
				authrole, _ := wamp.AsString(sess.Details["authrole"])
				sess.Unlock()
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

// sessionList is a session meta procedure that retrieves a list of the session
// IDs for all sessions currently attached to the realm.
func (r *realm) sessionList(msg *wamp.Invocation) wamp.Message {
	var filter []string
	if len(msg.Arguments) != 0 {
		filterList, ok := wamp.AsList(msg.Arguments[0])
		if !ok {
			return &wamp.Error{
				Type:    wamp.INVOCATION,
				Error:   wamp.ErrInvalidArgument,
				Request: msg.Request,
			}
		}
		filter, ok = wamp.ListToStrings(filterList)
		if !ok {
			return &wamp.Error{
				Type:    wamp.INVOCATION,
				Error:   wamp.ErrInvalidArgument,
				Request: msg.Request,
			}
		}
	}
	retChan := make(chan []wamp.ID)

	if len(filter) == 0 {
		r.actionChan <- func() {
			ids := make([]wamp.ID, len(r.clients))
			count := 0
			for sid := range r.clients {
				ids[count] = sid
				count++
			}
			retChan <- ids
		}
	} else {
		r.actionChan <- func() {
			var ids []wamp.ID
			for sid, sess := range r.clients {
				sess.Lock()
				authrole, _ := wamp.AsString(sess.Details["authrole"])
				sess.Unlock()
				for j := range filter {
					if filter[j] == authrole {
						ids = append(ids, sid)
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

// sessionGet is the session meta procedure that retrieves information on a
// specific session.
func (r *realm) sessionGet(msg *wamp.Invocation) wamp.Message {
	if len(msg.Arguments) == 0 {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	sid, ok := wamp.AsID(msg.Arguments[0])
	if !ok {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	retChan := make(chan *wamp.Session)
	r.actionChan <- func() {
		sess, _ := r.clients[sid]
		retChan <- sess
	}
	sess := <-retChan
	if sess == nil {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	// WAMP spec only specifies returning "session", "authid", "authrole",
	// "authmethod", "authprovider", and "transport".  All details are returned
	// in this implementation, except transport.auth, unless Config.MetaStrict
	// is set to true.
	sess.Lock()
	output := r.cleanSessionDetails(sess.Details)
	sess.Unlock()

	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{output},
	}
}

// sessionKill is a session meta procedure that closes a single session
// identified by session ID.
//
// The caller of this meta procedure may only specify session IDs other than
// its own session.  Specifying the caller's own session will result in a
// wamp.error.no_such_session since no other session with that ID exists.
func (r *realm) sessionKill(msg *wamp.Invocation) wamp.Message {
	if len(msg.Arguments) == 0 {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	sid, ok := wamp.AsID(msg.Arguments[0])
	if !ok {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}
	caller, _ := wamp.AsID(msg.Details["caller"])
	if caller == sid {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	reason, _ := wamp.AsURI(msg.ArgumentsKw["reason"])
	if reason != "" && !reason.ValidURI(false, "") {
		return makeError(msg.Request, wamp.ErrInvalidURI)
	}
	message, _ := wamp.AsString(msg.ArgumentsKw["message"])

	err := r.killSession(sid, reason, message)
	if err != nil {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	return &wamp.Yield{
		Request: msg.Request,
	}
}

// sessionKillByAuthid is a session meta procedure that closes all currently
// connected sessions that have the specified authid.  If the caller's own
// session has the specified authid, the caller's session is excluded from the
// closed sessions.
func (r *realm) sessionKillByAuthid(msg *wamp.Invocation) wamp.Message {
	if len(msg.Arguments) == 0 {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	authid, ok := wamp.AsString(msg.Arguments[0])
	if !ok {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	reason, _ := wamp.AsURI(msg.ArgumentsKw["reason"])
	if reason != "" && !reason.ValidURI(false, "") {
		return makeError(msg.Request, wamp.ErrInvalidURI)
	}
	message, _ := wamp.AsString(msg.ArgumentsKw["message"])

	caller, _ := wamp.AsID(msg.Details["caller"])
	count := r.killSessionsByDetail("authid", authid, reason, message, caller)
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{count},
	}
}

// sessionKillByAuthrole is a session meta procedure that closes all currently
// connected sessions that have the specified authrole.  If the caller's own
// session has the specified authrole, the caller's session is excluded from
// the closed sessions.
func (r *realm) sessionKillByAuthrole(msg *wamp.Invocation) wamp.Message {
	if len(msg.Arguments) == 0 {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	authrole, ok := wamp.AsString(msg.Arguments[0])
	if !ok {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	reason, _ := wamp.AsURI(msg.ArgumentsKw["reason"])
	if reason != "" && !reason.ValidURI(false, "") {
		return makeError(msg.Request, wamp.ErrInvalidURI)
	}
	message, _ := wamp.AsString(msg.ArgumentsKw["message"])

	caller, _ := wamp.AsID(msg.Details["caller"])
	count := r.killSessionsByDetail("authrole", authrole, reason, message, caller)
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{count},
	}
}

// sessionKillAll is a session meta procedure that closes all currently
// connected sessions in the caller's realm.  The caller's own session is
// excluded from the closed sessions.
func (r *realm) sessionKillAll(msg *wamp.Invocation) wamp.Message {
	reason, _ := wamp.AsURI(msg.ArgumentsKw["reason"])
	if reason != "" && !reason.ValidURI(false, "") {
		return makeError(msg.Request, wamp.ErrInvalidURI)
	}
	message, _ := wamp.AsString(msg.ArgumentsKw["message"])

	caller, _ := wamp.AsID(msg.Details["caller"])
	count := r.killAllSessions(reason, message, caller)
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{count},
	}
}

// sessionModifyDetails is a non-standard session meta procedure that modifies
// the details of a session.
//
// Positional arguments
//
// 1. `session|id` - The ID of the session to modify.
// 2. `details|dict` - Details delta.
func (r *realm) sessionModifyDetails(msg *wamp.Invocation) wamp.Message {
	if len(msg.Arguments) < 2 {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	sid, ok := wamp.AsID(msg.Arguments[0])
	if !ok {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	if sid == r.metaSess.ID {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}
	delta, ok := wamp.AsDict(msg.Arguments[1])
	if !ok {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	// Not allowed to modify session ID.
	if _, ok = delta["session"]; ok {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}

	done := make(chan struct{})
	r.actionChan <- func() {
		var sess *wamp.Session
		if sess, ok = r.clients[sid]; ok {
			r.modifySessionDetails(sess, delta)
		}
		close(done)
	}
	<-done

	if !ok {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	return &wamp.Yield{Request: msg.Request}
}

// testamentAdd adds a new publication which is executed when the client is
// detached (when session resumption is implemented) or destroyed (when the
// transport is lost).
func (r *realm) testamentAdd(msg *wamp.Invocation) wamp.Message {
	caller, ok := wamp.AsID(msg.Details["caller"])
	if !ok || len(msg.Arguments) < 3 {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	topic, ok := wamp.AsURI(msg.Arguments[0])
	if !ok {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	args, ok := wamp.AsList(msg.Arguments[1])
	if !ok {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	kwargs, ok := wamp.AsDict(msg.Arguments[2])
	if !ok {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	options, ok := wamp.AsDict(msg.ArgumentsKw["publish_options"])
	if !ok {
		options = wamp.Dict{}
	}
	scope, ok := wamp.AsString(msg.ArgumentsKw["scope"])
	if !ok || scope == "" {
		scope = "destroyed"
	}
	if scope != "destroyed" && scope != "detached" {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	if r.debug {
		r.log.Println("Adding", scope, "testament for session", caller)
	}

	r.actionChan <- func() {
		// A map returns the "zero value" if a key doesn't exist, so there are
		// nils for the arrays which are equal to empty arrays
		testaments := r.testaments[caller]
		t := testament{
			args:    args,
			kwargs:  kwargs,
			options: options,
			topic:   topic,
		}
		if scope == "destroyed" {
			testaments.destroyed = append(testaments.destroyed, t)
		} else {
			testaments.detached = append(testaments.detached, t)
		}
		r.testaments[caller] = testaments
	}
	return &wamp.Yield{Request: msg.Request}
}

// testamentFlush removes all testaments for the invoking client.  It takes an
// optional keyword argument "scope" that has the value "detached" or
// "destroyed"
func (r *realm) testamentFlush(msg *wamp.Invocation) wamp.Message {
	caller, ok := wamp.AsID(msg.Details["caller"])
	if !ok {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	scope, ok := wamp.AsString(msg.ArgumentsKw["scope"])
	if !ok || scope == "" {
		scope = "destroyed"
	}
	if scope != "destroyed" && scope != "detached" {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	if r.debug {
		r.log.Println("Flushing", scope, "testaments for session", caller)
	}

	r.actionChan <- func() {
		testaments, ok := r.testaments[caller]
		if !ok {
			return
		}
		if scope == "destroyed" {
			testaments.destroyed = nil
		} else {
			testaments.detached = nil
		}
		if testaments.destroyed == nil && testaments.detached == nil {
			delete(r.testaments, caller)
			return
		}
		r.testaments[caller] = testaments
	}
	return &wamp.Yield{Request: msg.Request}
}

// cleanSessionDetails returns a dictionary that only contains allowed session
// details. transport.auth is never allowed, because the data in transport.auth
// may not be serializable and may expose auth information to session meta.
func (r *realm) cleanSessionDetails(details wamp.Dict) wamp.Dict {
	var clean wamp.Dict
	// If in strict mode, only include allowed values.
	if r.metaStrict {
		stdItems := []string{"session", "authid", "authrole", "authmethod",
			"authprovider", "transport"}

		clean = make(wamp.Dict, len(stdItems)+len(r.metaIncDetails))
		// Copy standard details.
		for _, k := range stdItems {
			if v, ok := details[k]; ok {
				clean[k] = v
			}
		}
		// Copy additional includes.
		for _, k := range r.metaIncDetails {
			if v, ok := details[k]; ok {
				clean[k] = v
			}
		}
	} else {
		clean = details
	}

	// If there is no transport detail, all done.
	transDict := wamp.DictChild(details, "transport")
	if transDict == nil {
		return clean
	}

	// If transport detail does not have auth, then use transport as-is.
	authDict := wamp.DictChild(transDict, "auth")
	if authDict == nil {
		return clean
	}

	// If a copy was not previously needed, it is now.
	if !r.metaStrict {
		clean = make(wamp.Dict, len(details))
		for k, v := range details {
			clean[k] = v
		}
	}

	// If details.transport.auth exists, then provide version of transport
	// detail without auth.
	var altTrans wamp.Dict
	for n, v := range transDict {
		if n == "auth" {
			continue
		}
		if altTrans == nil {
			altTrans = wamp.Dict{}
		}
		altTrans[n] = v
	}
	clean["transport"] = altTrans

	return clean
}

// makeError returns a wamp.Error message with the given URI.
func makeError(req wamp.ID, uri wamp.URI) *wamp.Error {
	return &wamp.Error{
		Type:    wamp.INVOCATION,
		Request: req,
		Details: wamp.Dict{},
		Error:   uri,
	}
}

// makeGoodbye returns a wamp.Goodbye message with the reason and message.
func makeGoodbye(reason wamp.URI, message string) *wamp.Goodbye {
	if reason == wamp.URI("") {
		reason = wamp.CloseNormal
	}
	details := wamp.Dict{}
	if message != "" {
		details["message"] = message
	}
	return &wamp.Goodbye{
		Reason:  reason,
		Details: details,
	}
}

// killSession closes the session identified by session ID.  The meta session
// cannot be closed.
func (r *realm) killSession(sid wamp.ID, reason wamp.URI, message string) error {
	goodbye := makeGoodbye(reason, message)
	errChan := make(chan error)
	r.actionChan <- func() {
		sess, ok := r.clients[sid]
		if !ok {
			errChan <- errors.New("no such session")
			return
		}
		sess.EndRecv(goodbye)
		close(errChan)
	}
	return <-errChan
}

// killSessionsByDetail closes all sessions that have a session detail that
// matches the key and value parameters specified.  The meta session and any
// session specified in the exclude parameter are not closed.
func (r *realm) killSessionsByDetail(key, value string, reason wamp.URI, message string, exclude wamp.ID) int {
	goodbye := makeGoodbye(reason, message)
	retChan := make(chan int)
	r.actionChan <- func() {
		var kills int
		for sid, sess := range r.clients {
			if sid == exclude || sess == r.metaSess {
				continue
			}

			sess.Lock()
			val, ok := wamp.AsString(sess.Details[key])
			sess.Unlock()

			if !ok || val != value {
				continue
			}
			if sess.EndRecv(goodbye) {
				kills++
			}
		}
		retChan <- kills
	}
	return <-retChan
}

// killAllSessions closes all currently connected sessions in the caller's
// realm, except for the meta session and the session specified by the exclude
// parameter.
func (r *realm) killAllSessions(reason wamp.URI, message string, exclude wamp.ID) int {
	goodbye := makeGoodbye(reason, message)
	retChan := make(chan int)
	goodbye.Details["all"] = nil
	r.actionChan <- func() {
		var kills int
		for sid, sess := range r.clients {
			// Skip excluded session.  MetaSession does not have a kill channel
			// so not need to explicitly exclude.
			if sid == exclude {
				continue
			}
			if sess.EndRecv(goodbye) {
				kills++
			}
		}
		retChan <- kills
	}
	return <-retChan
}

// modifySessionDetails takes a session and a wamp.Dict that specifies the
// changes to make to the session details.
//
// An item with a non-nil value in the delta wamp.Dict specifies adding or
// updating that item in the session details.  An item with a nil value in the
// delta wamp.Dict specifies deleting that item from the session details.
func (r *realm) modifySessionDetails(sess *wamp.Session, delta wamp.Dict) {
	sess.Lock()
	defer sess.Unlock()

	for k, v := range delta {
		if v == nil {
			if r.debug {
				r.log.Println("Deleted", k, "from session details")
			}
			delete(sess.Details, k)
			continue
		}
		if r.debug {
			r.log.Println("Updated", k, "in session details")
		}
		sess.Details[k] = v
	}
}
