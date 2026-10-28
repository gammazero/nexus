package router

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// Special ID for meta session.
const metaID = wamp.ID(1)

// replyRoomTimeout is how long a session's request waits for the session to
// read enough of its pending replies to make room for another. A session that
// reads nothing for this long is closed.
const replyRoomTimeout = time.Minute

// Testament scope.
const (
	destroyedScope = "destroyed"
	detachedScope  = "detached"
)

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
// authentication and authorization. WAMP messages are only routed within a
// Realm.
type realm struct {
	uri wamp.URI

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
	stopped    chan struct{}

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

	// strictRequestIDs enforces sequential client-allocated request IDs. See
	// RealmConfig.StrictRequestIDs.
	strictRequestIDs bool

	// eventBacklog is the send limit of each client session. See
	// RealmConfig.EventBacklog.
	eventBacklog int
}

var (
	shutdownGoodbye = &wamp.Goodbye{ //nolint:gochecknoglobals
		Reason:  wamp.ErrSystemShutdown,
		Details: wamp.Dict{},
	}
)

// checkRealmConfig returns an error if config is not valid for creating a
// realm. It is called before the realm's broker and dealer are created, so
// that an invalid config does not leave their goroutines running.
func checkRealmConfig(config *RealmConfig) error {
	if !config.URI.ValidURI(config.StrictURI, "") {
		return fmt.Errorf(
			"invalid realm URI %v (URI strict checking %v)", config.URI, config.StrictURI)
	}
	if config.EventBacklog < 0 {
		return fmt.Errorf("negative event backlog: %d", config.EventBacklog)
	}
	return nil
}

// newRealm creates a new realm with the given RealmConfig, broker and dealer.
// The config must have been checked with checkRealmConfig.
func newRealm(config *RealmConfig, broker *broker, dealer *dealer, logger stdlog.StdLog) *realm {
	r := &realm{
		uri:         config.URI,
		broker:      broker,
		dealer:      dealer,
		authorizer:  config.Authorizer,
		clients:     map[wamp.ID]*wamp.Session{},
		testaments:  map[wamp.ID]testamentBucket{},
		actionChan:  make(chan func()),
		stopped:     make(chan struct{}),
		metaIDGen:   new(wamp.IDGen),
		metaDone:    make(chan struct{}),
		metaProcMap: make(map[wamp.ID]func(*wamp.Invocation) wamp.Message, 9),
		log:         logger,
		debug:       config.Debug,
		localAuth:   config.RequireLocalAuth,
		localAuthz:  config.RequireLocalAuthz,
		metaStrict:  config.MetaStrict,

		enableMetaKill:   config.EnableMetaKill,
		enableMetaModify: config.EnableMetaModify,

		strictRequestIDs: config.StrictRequestIDs,
		eventBacklog:     config.EventBacklog,
	}

	if r.debug {
		if r.enableMetaKill {
			r.log.Println("Session meta kill procedures enabled")
		}
		if r.enableMetaModify {
			r.log.Println("Session meta modify_details procedure enabled")
		}
	}
	if r.metaStrict && len(config.MetaIncludeSessionDetails) != 0 {
		r.metaIncDetails = slices.Clone(config.MetaIncludeSessionDetails)
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

	r.setupMetaProcedures()

	go r.metaProcedureHandler()
	go r.run()

	return r
}

func (r *realm) run() {
	for action := range r.actionChan {
		action()
	}
	close(r.stopped)
}

// close performs an orderly shutdown of the realm.
//
// First a lock is acquired that prevents any new clients from joining the
// realm and makes sure any clients already in the process of joining finish
// joining.
//
// Next, each client session is killed, triggering a GOODBYE message to the
// client, and causing the session's message handler to exit. Each handler
// removes its session from the broker and dealer before closing the session,
// since other sessions may still be routing messages to it. This ensures there
// are no messages remaining to be sent to the router.
//
// After that, the meta client session is killed. This ensures there are no
// more meta messages to send to the router.
//
// At this point the broker and dealer are shutdown since they cannot receive
// any more messages to route, and have no clients to route messages to.
//
// Finally, the realm's action channel is closed and its goroutine is stopped.
func (r *realm) close() {
	// The lock is held in mutual exclusion with the router starting any new
	// session handlers for this realm. This prevents the router from starting
	// any new session handlers, allowing the realm to safely close after
	// waiting for all existing session handlers to exit.
	r.closeLock.Lock()
	defer r.closeLock.Unlock()
	if r.closed {
		// This realm is already closed.
		return
	}
	r.closed = true

	// Kick all clients off. Sending shutdownGoodbye causes client message
	// handlers to exit without sending meta events.
	ch := make(chan struct{})
	r.actionChan <- func() {
		for _, c := range r.clients {
			c.EndRecv(shutdownGoodbye)
		}
		close(ch)
	}
	<-ch

	// Wait until each client's handleInboundMessages() has exited. No new
	// messages can be generated once sessions are closed.
	r.waitHandlers.Wait()

	// All normal handlers have exited, so now stop the meta session. When the
	// meta client receives GOODBYE from the meta session, the meta session is
	// done and will not try to publish anything more to the broker, and it is
	// finally safe to exit and close the broker.
	r.metaSess.EndRecv(shutdownGoodbye)
	<-r.metaDone

	// handleInboundMessages() and metaProcedureHandler() are the only things
	// that can submit requests to the broker and dealer, so now that these are
	// finished there can be no more messages to broker and dealer.

	// No new messages, so safe to close dealer and broker.
	r.dealer.close()
	r.broker.close()

	// Finally close realm's action channel.
	close(r.actionChan)
	<-r.stopped
}

func (r *realm) setupMetaProcedures() {
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
	// Deprecated: misspelled name this procedure previously registered under.
	// Registered as an alias so that clients that call it keep working.
	r.registerMetaProcedure(wamp.URI("wamp.subscription.count_suscribers"), r.broker.subCountSubscribers) //nolint:staticcheck // registers the deprecated alias on purpose
	r.registerMetaProcedure(wamp.MetaProcEventHistory, r.broker.subEventHistory)

	// Register to handle testament meta procedures.
	r.registerMetaProcedure(wamp.MetaProcSessionAddTestament, r.testamentAdd)
	r.registerMetaProcedure(wamp.MetaProcSessionFlushTestaments, r.testamentFlush)
}

// createMetaSession creates and starts a session that runs in this realm, and
// bridges two peers. One peer, the r.metaSess, is associated with the router
// and handles meta session requests. The other, r.metaPeer, is the remote
// (client) side of the router uplink and is used as the interface to send meta
// session messages to. Sending a PUBLISH message to it will result in the
// router publishing the event to any subscribers.
func (r *realm) createMetaSession() {
	cli, rtr := transport.LinkedPeers()
	r.metaPeer = cli

	r.dealer.setMetaPeer(cli)

	// This session is the local leg of the router uplink.
	r.metaSess = wamp.NewSession(rtr, metaID, wamp.Dict{"authrole": "trusted"}, nil)

	// Run the handler for messages from the meta session.
	go func() {
		last, _, _, err := r.handleInboundMessages(r.metaSess)
		if err != nil {
			r.log.Println("meta session handler should never return error, got:", err)
		}
		r.metaSess.StopSending()
		sendLast(r.metaSess, last)
	}()
	if r.debug {
		r.log.Println("Started meta-session", r.metaSess)
	}
}

// onJoin is called when a non-meta session joins this realm. The session is
// stored in the realm's clients and a meta event is published.
//
// welcome, if not nil, is queued to the session in the same realm action that
// stores the session, so that the client cannot get its WELCOME before the
// session is visible to the realm, and nothing that finds the session through
// the realm can queue a message to it ahead of the WELCOME. Nothing else can
// reach the session before then, so its outbound queue is empty and, being
// buffered for every router-created peer, accepts the WELCOME without blocking
// the realm goroutine.
//
// Note: onJoin() is called from handleSession, not handleInboundMessages, so
// that it is not called for the meta client.
func (r *realm) onJoin(sess *wamp.Session, welcome wamp.Message) {
	sync := make(chan struct{})
	r.actionChan <- func() {
		r.clients[sess.ID] = sess
		if welcome != nil {
			sess.Send() <- welcome
		}
		close(sync)
	}
	<-sync

	// Session Meta Events MUST be dispatched by the Router to the same realm
	// as the WAMP session which triggered the event.
	//
	// WAMP spec only specifies publishing "session", "authid", "authrole",
	// "authmethod", "authprovider", "transport". This implementation publishes
	// all details except transport.auth.
	sess.Lock()
	output := r.cleanSessionDetails(sess.Details)
	sess.Unlock()
	r.metaPeer.Send() <- &wamp.Publish{
		Request:   wamp.GlobalID(),
		Topic:     wamp.MetaEventSessionOnJoin,
		Arguments: wamp.List{output},
	}
}

// onLeave is called when a non-meta session leaves this realm. The session is
// removed from the realm's clients and a meta event is published.
//
// The session is always removed from the broker and dealer, and this is done
// before onLeave returns, so that the session can then be closed. During
// shutdown, other sessions keep routing messages until their own handlers
// exit, so the broker and dealer must not hold a session that is closed.
//
// If the session handler exited due to realm shutdown, then remove the session
// from broker, dealer, and realm without generating meta events. If not
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

		r.dealer.removeSession(sess, !shutdown)
		r.broker.removeSession(sess, !shutdown)
		close(sync)
	}
	<-sync

	if shutdown || killAll {
		return
	}
	if hasTstm {
		sendTestaments := func(testaments []testament) {
			for i := range testaments {
				r.metaPeer.Send() <- &wamp.Publish{
					Request:     wamp.GlobalID(),
					Topic:       testaments[i].topic,
					Arguments:   testaments[i].args,
					ArgumentsKw: testaments[i].kwargs,
					Options:     testaments[i].options,
				}
			}
		}
		sendTestaments(testaments.detached)
		sendTestaments(testaments.destroyed)
	}
	r.metaPeer.Send() <- &wamp.Publish{
		Request: wamp.GlobalID(),
		Topic:   wamp.MetaEventSessionOnLeave,
		Arguments: wamp.List{
			sess.ID,
			sess.Details["authid"],
			sess.Details["authrole"]},
	}
}

// handleSession starts a session attached to this realm.
//
// Routing occurs only between WAMP Sessions that have joined the same Realm.
//
// welcome, if not nil, is queued to the session as the session becomes visible
// to the rest of the realm (onJoin), so it is always the first message the
// client receives, and the session can be found, as by wamp.session.kill_all,
// once the client has it. Anything that reacts to the join, such as a
// wamp.session.kill triggered by wamp.session.on_join or an event, queues
// behind it. If the realm is closed the welcome is not sent and an error is
// returned, so the caller can still ABORT the handshake.
func (r *realm) handleSession(sess *wamp.Session, welcome wamp.Message) error {
	// The lock is held in mutual exclusion with the closing of the realm. This
	// ensures that no new session handler can start once the realm is closing,
	// during which the realm waits for all existing session handlers to exit.
	r.closeLock.Lock()
	if r.closed {
		r.closeLock.Unlock()
		err := errors.New("realm closed")
		return err
	}

	// Set before the session is visible, so that nothing is sent to it under the
	// default limit.
	sess.SetSendLimit(r.eventBacklog)

	r.waitHandlers.Add(1)

	// Ensure session is capable of receiving exit signal before releasing
	// lock.
	r.onJoin(sess, welcome)
	r.closeLock.Unlock()

	if r.debug {
		r.log.Println("Handling messages for session", sess)
	}
	go func() {
		last, shutdown, killAll, err := r.handleInboundMessages(sess)
		if err != nil {
			last = &wamp.Abort{
				Reason:  wamp.ErrProtocolViolation,
				Details: wamp.Dict{wamp.OptMessage: err.Error()},
			}
			r.log.Println("Aborting session", sess, ":", err)
		}
		r.onLeave(sess, shutdown, killAll)
		// Send the GOODBYE or ABORT only once the session is removed from the realm,
		// so that the router sends nothing to the session after it. Removing it from
		// the dealer publishes meta events, such as wamp.registration.on_delete, that
		// the session may be subscribed to. Stop sending the session's reply backlog
		// first, so that no reply follows the GOODBYE or ABORT.
		sess.StopSending()
		sendLast(sess, last)
		sess.Close()
		r.waitHandlers.Done()
	}()

	return nil
}

// isRequest reports whether msg is a request, which the router answers with a
// reply that it never drops. The other messages from a client are YIELD and
// ERROR, which answer the router's INVOCATION, and GOODBYE. The router does
// not wait for room for a reply before handling them, so any error it returns
// for them must be sent with TrySend, or a client that keeps sending them
// without reading would grow its backlog without limit.
func isRequest(msg wamp.Message) bool {
	switch msg.(type) {
	case *wamp.Yield, *wamp.Error, *wamp.Goodbye:
		return false
	}
	return true
}

// clientAllocatedRequestID returns the session scope request ID of a message
// that allocates one: PUBLISH, SUBSCRIBE, UNSUBSCRIBE, REGISTER, UNREGISTER or
// CALL. Messages that only refer to an existing request, which are CANCEL
// (CALL.Request), YIELD (INVOCATION.Request, allocated by the router) and
// ERROR, return false.
func clientAllocatedRequestID(msg wamp.Message) (wamp.ID, bool) {
	switch m := msg.(type) {
	case *wamp.Publish:
		return m.Request, true
	case *wamp.Subscribe:
		return m.Request, true
	case *wamp.Unsubscribe:
		return m.Request, true
	case *wamp.Register:
		return m.Request, true
	case *wamp.Unregister:
		return m.Request, true
	case *wamp.Call:
		return m.Request, true
	}
	return 0, false
}

// handleInboundMessages handles the messages sent from a client session to the
// router. When the session ends, it returns the GOODBYE or ABORT to send to
// the client, if any, which the caller sends once the session is removed from
// the realm. It also reports whether the session ended because the router is
// shutting down, and whether it was killed by wamp.session.kill_all.
func (r *realm) handleInboundMessages(sess *wamp.Session) (wamp.Message, bool, bool, error) {
	if r.debug {
		defer r.log.Println("Ended session", sess)
	}
	recv := sess.Recv()
	recvDone := sess.RecvDone()

	// Client-allocated request IDs are session scope IDs, which must start at 1
	// and increment by 1. Check them when StrictRequestIDs is enabled for this
	// realm. The meta session is exempt because its ID generator is
	// router-internal and trusted.
	var reqIDs *requestIDSeq
	if r.strictRequestIDs && sess != r.metaSess {
		reqIDs = &requestIDSeq{}
	}

	for {
		var msg wamp.Message
		var open bool
		select {
		case msg, open = <-recv:
			if !open {
				r.log.Println("Lost", sess)
				return nil, false, false, nil
			}
		case <-recvDone:
			if abort := sess.Abort(); abort != nil {
				// The broker or dealer aborted the session.
				r.log.Println("Aborting session", sess, ":", abort.Details[wamp.OptMessage])
				return abort, false, false, nil
			}
			goodbye := sess.Goodbye()
			switch goodbye {
			case shutdownGoodbye, wamp.NoGoodbye:
				if r.debug {
					r.log.Printf("Stop session %s: system shutdown", sess)
				}
				return goodbye, true, false, nil
			}
			if r.debug {
				r.log.Printf("Kill session %s: %s", sess, goodbye.Reason)
			}
			var killAll bool
			if _, ok := goodbye.Details["all"]; ok {
				killAll = true
			}
			return goodbye, false, killAll, nil
		}

		if r.debug {
			r.log.Printf("Session %s submitting %s: %+v", sess,
				msg.MessageType(), msg)
		}

		// Wait for room for the reply before handling a request, so that the
		// session's backlog of replies stays bounded. Replies are never
		// dropped, and messages that are not requests still flow.
		if isRequest(msg) && sess != r.metaSess && !r.waitReplyRoom(sess, recvDone) {
			return nil, false, false, nil
		}

		// Check the request ID before authorization, so that a request that is not
		// authorized still uses up its ID. Returning an error causes the caller to
		// send an ABORT with wamp.error.protocol_violation and close the session.
		var continuation bool
		if reqIDs != nil {
			if id, ok := clientAllocatedRequestID(msg); ok {
				_, isCall := msg.(*wamp.Call)
				var err error
				if continuation, err = reqIDs.check(id, isCall); err != nil {
					return nil, false, false, err
				}
			}
		}

		// Note: meta session is always authorized.
		if r.authorizer != nil && sess != r.metaSess && !r.authzMessage(sess, msg) {
			// Not authorized; error response sent; do not process message.
			continue
		}

		switch msg := msg.(type) {
		case *wamp.Publish:
			r.broker.publish(sess, msg)
		case *wamp.Yield:
			r.dealer.yield(sess, msg)
		case *wamp.Call:
			if continuation {
				r.dealer.continueCall(sess, msg)
			} else {
				r.dealer.call(sess, msg)
			}
		case *wamp.Cancel:
			r.dealer.cancel(sess, msg)
		case *wamp.Subscribe:
			r.broker.subscribe(sess, msg)
		case *wamp.Register:
			r.dealer.register(sess, msg)
		case *wamp.Unsubscribe:
			r.broker.unsubscribe(sess, msg)
		case *wamp.Unregister:
			r.dealer.unregister(sess, msg)

		case *wamp.Error:
			// An INVOCATION error is the only type of ERROR message the router
			// should receive.
			if msg.Type != wamp.INVOCATION {
				return nil, false, false, fmt.Errorf("invalid ERROR received: %v", msg)
			}
			r.dealer.error(sess, msg)

		case *wamp.Goodbye:
			// Handle client leaving realm.
			if r.debug {
				r.log.Println("GOODBYE from session", sess, "reason:", msg.Reason)
			}
			return &wamp.Goodbye{
				Reason:  wamp.ErrGoodbyeAndOut,
				Details: wamp.Dict{},
			}, false, false, nil

		default:
			// Received unrecognized message type.
			return nil, false, false, fmt.Errorf("unexpected %v", msg.MessageType())
		}
	}
}

// waitReplyRoom waits until the session has room for another reply. It
// returns true when there is room, or when the session is ending, since any
// reply is then discarded. It returns false if the session reads none of its
// replies for replyRoomTimeout, or if its peer can no longer exchange
// messages, and the session must be closed. A session that keeps reading,
// however slowly, is not closed.
//
// Waiting stops reading the session's messages, which pushes back on the
// client instead of letting it queue unlimited replies in the router. Waiting
// is bounded because a client may stop reading while it waits to send, and
// would otherwise deadlock with the router.
func (r *realm) waitReplyRoom(sess *wamp.Session, recvDone <-chan struct{}) bool {
	room := sess.ReplyRoom()
	if room == nil {
		return true
	}
	sent := sess.BacklogSent()
	timer := time.NewTimer(replyRoomTimeout)
	defer timer.Stop()
	for {
		select {
		case <-room:
			return true
		case <-recvDone:
			return true
		case <-sess.PeerDone():
			// The client disconnected. The router is not reading the session, so it would
			// not see that the session is lost until the timeout.
			r.log.Println("Lost", sess, "while waiting for it to read replies")
			return false
		case <-timer.C:
			if n := sess.BacklogSent(); n != sent {
				// The session read some replies, so keep waiting.
				sent = n
				timer.Reset(replyRoomTimeout)
				continue
			}
			r.log.Printf("!!! Closing session %s: not reading replies", sess)
			return false
		}
	}
}

// authzMessage checks if the session is authorized to send the message. If
// authorization fails or if the session is not authorized, then an error
// response is returned to the client, and this method returns false.
func (r *realm) authzMessage(sess *wamp.Session, msg wamp.Message) bool {
	// If the client is local, then do not check authorization, unless
	// requested in config.
	if sess.IsLocal() && !r.localAuthz {
		return true
	}

	// Authorize a copy of the session details, without holding the session lock.
	// The broker locks each subscriber's session while filtering a publish, so an
	// Authorizer that blocks with the lock held, for example on a remote service,
	// would stall event delivery for the whole realm. The Authorizer may change
	// the session details, so its changes to the copy are applied to the session
	// afterward. Authorize calls for a session run one at a time, on that
	// session's goroutine.
	//
	// The copy is shallow, so nested values are shared with the session. The
	// Authorizer must replace a nested value to change it, not change it in
	// place, as documented on Authorizer.
	sess.Lock()
	before := maps.Clone(sess.Details)
	sess.Unlock()
	details := maps.Clone(before)

	// Create a safe session to prevent access to the session.Peer.
	safeSession := &wamp.Session{
		ID:      sess.ID,
		Details: details,
	}
	isAuthz, err := r.authorizer.Authorize(safeSession, msg)
	applyDetailChanges(sess, before, details)

	if !isAuthz {
		skipResponse := false
		errRsp := &wamp.Error{Type: msg.MessageType(), Details: wamp.Dict{}}
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
			// Error trying to authorize. Include error message.
			errRsp.Error = wamp.ErrAuthorizationFailed
			errRsp.Arguments = wamp.List{err.Error()}
			r.log.Println("Client", sess, "authorization failed:", err)
		} else {
			// Session not authorized. The inability to return a message is
			// intentional, so as not to encourage returning information that
			// could disclose any clues about authorization to an attacker.
			errRsp.Error = wamp.ErrNotAuthorized
			r.log.Println("Client", sess, msg.MessageType(), "not authorized")
		}
		if !skipResponse {
			if isRequest(msg) {
				sess.SendReply(errRsp)
			} else {
				sess.TrySend(errRsp)
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
	if client.IsLocal() && !r.localAuth {
		// Create welcome details for local client.
		authid, _ := wamp.AsString(details["authid"])
		if authid == "" {
			authid = strconv.FormatUint(uint64(wamp.GlobalID()), 16)
		}
		details = wamp.Dict{
			"authid":       authid,
			"authrole":     "trusted",
			"authmethod":   "local",
			"authprovider": "static",
			"roles": wamp.Dict{
				wamp.RoleBroker: r.broker.role(),
				wamp.RoleDealer: r.dealer.role(),
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

	// Expose the target realm URI to the authenticator. The HELLO carries the
	// realm as a separate message field from Details, so without this, dynamic
	// authenticators that share one instance across multiple realms cannot tell
	// which realm the client is requesting. It is added to a copy of details,
	// since details become the session details that other clients see, and the
	// client already knows the realm it joined.
	authDetails := make(wamp.Dict, len(details)+1)
	maps.Copy(authDetails, details)
	authDetails[auth.RealmDetail] = string(r.uri)

	// Return welcome message or error.
	welcome, err := authr.Authenticate(sid, authDetails, client)
	if err != nil {
		return nil, err
	}
	welcome.Details["authmethod"] = method
	welcome.Details["roles"] = wamp.Dict{
		wamp.RoleBroker: r.broker.role(),
		wamp.RoleDealer: r.dealer.role(),
	}
	return welcome, nil
}

// applyDetailChanges applies to the details of sess the changes from before to
// after: the top-level keys that were added, removed, or set to a different
// value. A value counts as different only if it is not the same value, for
// example a new map rather than the same map changed in place. The other keys
// keep their current values, which may have been changed meanwhile, for
// example by wamp.session.modify_details.
func applyDetailChanges(sess *wamp.Session, before, after wamp.Dict) {
	var set wamp.Dict
	for k, v := range after {
		if old, ok := before[k]; ok && sameDetailValue(old, v) {
			continue
		}
		if set == nil {
			set = wamp.Dict{}
		}
		set[k] = v
	}
	var removed []string
	for k := range before {
		if _, ok := after[k]; !ok {
			removed = append(removed, k)
		}
	}
	if set == nil && removed == nil {
		return
	}

	sess.Lock()
	defer sess.Unlock()
	if sess.Details == nil {
		sess.Details = wamp.Dict{}
	}
	maps.Copy(sess.Details, set)
	for _, k := range removed {
		delete(sess.Details, k)
	}
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
	// Register the meta procedure. The "disclose_caller" option must be
	// enabled for the testament API and the meta session API.
	r.metaPeer.Send() <- &wamp.Register{
		Request: r.metaIDGen.Next(),
		Options: wamp.Dict{
			"disclose_caller": true,
		},
		Procedure: procedure,
	}
	msg := <-r.metaPeer.Recv()
	if msg == nil {
		// This would only happen if the meta client was closed before or
		// during meta procedure registration at realm startup. Safety first.
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
				r.metaPeer.Send() <- &wamp.Error{
					Type:    msg.MessageType(),
					Request: msg.Request,
					Details: wamp.Dict{},
					Error:   wamp.ErrNoSuchProcedure,
				}
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
		r.metaPeer.Send() <- rsp
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
				Details: wamp.Dict{},
			}
		}
		filter, ok = wamp.ListToStrings(filterList)
		if !ok {
			return &wamp.Error{
				Type:    wamp.INVOCATION,
				Error:   wamp.ErrInvalidArgument,
				Request: msg.Request,
				Details: wamp.Dict{},
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
				if slices.Contains(filter, authrole) {
					nclients++
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
				Details: wamp.Dict{},
			}
		}
		filter, ok = wamp.ListToStrings(filterList)
		if !ok {
			return &wamp.Error{
				Type:    wamp.INVOCATION,
				Error:   wamp.ErrInvalidArgument,
				Request: msg.Request,
				Details: wamp.Dict{},
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
				if slices.Contains(filter, authrole) {
					ids = append(ids, sid)
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
		sess := r.clients[sid]
		retChan <- sess
	}
	sess := <-retChan
	if sess == nil {
		return makeError(msg.Request, wamp.ErrNoSuchSession)
	}

	// WAMP spec only specifies returning "session", "authid", "authrole",
	// "authmethod", "authprovider", and "transport". All details are returned
	// in this implementation, except transport.auth, unless
	// RealmConfig.MetaStrict is set to true.
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
// its own session. Specifying the caller's own session will result in a
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
// connected sessions that have the specified authid. If the caller's own
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
// connected sessions that have the specified authrole. If the caller's own
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
// connected sessions in the caller's realm. The caller's own session is
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
// # Positional arguments
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
		scope = destroyedScope
	}
	if scope != destroyedScope && scope != detachedScope {
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
		if scope == destroyedScope {
			testaments.destroyed = append(testaments.destroyed, t)
		} else {
			testaments.detached = append(testaments.detached, t)
		}
		r.testaments[caller] = testaments
	}
	return &wamp.Yield{Request: msg.Request}
}

// testamentFlush removes all testaments for the invoking client. It takes an
// optional keyword argument "scope" that has the value "detached" or
// "destroyed".
func (r *realm) testamentFlush(msg *wamp.Invocation) wamp.Message {
	caller, ok := wamp.AsID(msg.Details["caller"])
	if !ok {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	scope, ok := wamp.AsString(msg.ArgumentsKw["scope"])
	if !ok || scope == "" {
		scope = destroyedScope
	}
	if scope != destroyedScope && scope != detachedScope {
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
		if scope == destroyedScope {
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
//
// The returned dictionary is always a copy, since it is sent to other sessions
// and the session details may change after it is returned. Call this with the
// session locked.
func (r *realm) cleanSessionDetails(details wamp.Dict) wamp.Dict {
	var clean wamp.Dict
	// If in strict mode, only include allowed values.
	if r.metaStrict {
		stdItems := []string{"session", "authid", "authrole", "authmethod", "authprovider", "transport"}

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
		clean = maps.Clone(details)
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

// sendLast sends msg, the GOODBYE or ABORT that ends the session, to sess,
// unless msg is nil or the outbound queue of sess is full.
func sendLast(sess *wamp.Session, msg wamp.Message) {
	if msg == nil {
		return
	}
	select {
	case sess.Send() <- msg:
	default:
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

// killSession closes the session identified by session ID. The meta session
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
// matches the key and value parameters specified. The meta session and any
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

// killSessionsByDetailIfOpen is killSessionsByDetail for callers outside the
// realm, which may call it while the realm is being closed. It holds closeLock
// so that the realm is not closed meanwhile, which would close actionChan,
// and returns an error if the realm is already closed. The meta session is not
// closed.
func (r *realm) killSessionsByDetailIfOpen(key, value string, reason wamp.URI, message string) (int, error) { //nolint:lll
	r.closeLock.Lock()
	defer r.closeLock.Unlock()
	if r.closed {
		return 0, fmt.Errorf("realm %s is closed", r.uri)
	}
	return r.killSessionsByDetail(key, value, reason, message, 0), nil
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
			// Skip excluded session. The meta session is not in clients, so it
			// does not need to be excluded.
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
// updating that item in the session details. An item with a nil value in the
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
