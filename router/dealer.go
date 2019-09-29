package router

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/wamp"
)

const (
	roleCallee = "callee"
	roleCaller = "caller"

	featureCallCanceling    = "call_canceling"
	featureCallTimeout      = "call_timeout"
	featureCallerIdent      = "caller_identification"
	featurePatternBasedReg  = "pattern_based_registration"
	featureProgCallResults  = "progressive_call_results"
	featureSessionMetaAPI   = "session_meta_api"
	featureSharedReg        = "shared_registration"
	featureRegMetaAPI       = "registration_meta_api"
	featureTestamentMetaAPI = "testament_meta_api"

	// sendResultDeadline is the amount of time until the dealer gives up
	// trying to send a RESULT to a blocked caller.  This is different that the
	// CALL timeout which spedifies how long the callee may take to answer.
	sendResultDeadline = time.Minute
	// yieldRetryDelay is the initial delay before reprocessin a blocked yield
	yieldRetryDelay = time.Millisecond
)

// Role information for this broker.
var dealerRole = wamp.Dict{
	"features": wamp.Dict{
		featureCallCanceling:    true,
		featureCallTimeout:      true,
		featureCallerIdent:      true,
		featurePatternBasedReg:  true,
		featureProgCallResults:  true,
		featureSessionMetaAPI:   true,
		featureSharedReg:        true,
		featureRegMetaAPI:       true,
		featureTestamentMetaAPI: true,
	},
}

// remoteProcedure tracks in-progress remote procedure call
type registration struct {
	id         wamp.ID  // registration ID
	procedure  wamp.URI // procedure this registration is for
	created    string   // when registration was created
	match      string   // how procedure uri is matched to registration
	policy     string   // how callee is selected if shared registration
	disclose   bool     // callee requests disclosure of caller identity
	nextCallee int      // choose callee for round-robin invocation.

	// Multiple sessions can register as callees depending on invocation policy
	// resulting in multiple procedures for the same registration ID.
	callees []*wamp.Session
}

// invocation tracks in-progress invocation
type invocation struct {
	callID     requestID
	callee     *wamp.Session
	canceled   bool
	retryCount int
}

type requestID struct {
	session wamp.ID
	request wamp.ID
}

type dealer struct {
	// procedure URI -> registration ID
	procRegMap    map[wamp.URI]*registration
	pfxProcRegMap map[wamp.URI]*registration
	wcProcRegMap  map[wamp.URI]*registration

	// registration ID -> registration
	// Used to lookup registration by ID, needed for unregister.
	registrations map[wamp.ID]*registration

	// call ID -> caller session
	calls map[requestID]*wamp.Session

	// invocation ID -> {call ID, callee, canceled}
	invocations map[wamp.ID]*invocation

	// call ID -> invocation ID (for cancel)
	invocationByCall map[requestID]wamp.ID

	// callee session -> registration ID set.
	// Used to lookup registrations when removing a callee session.
	calleeRegIDSet map[*wamp.Session]map[wamp.ID]struct{}

	actionChan chan func()

	// Generate registration IDs.
	idGen *wamp.IDGen

	// Used for round-robin call invocation.
	prng *rand.Rand

	// Dealer behavior flags.
	strictURI     bool
	allowDisclose bool

	metaPeer wamp.Peer

	// Meta-procedure registration ID -> handler func.
	metaProcMap map[wamp.ID]func(*wamp.Invocation) wamp.Message

	log   stdlog.StdLog
	debug bool
}

// newDealer creates the default Dealer implementation.
//
// Messages are routed serially by the dealer's message handling goroutine.
// This serialization is limited to the work of determining the message's
// destination, and then the message is handed off to the next goroutine,
// typically the receiving client's send handler.
func newDealer(logger stdlog.StdLog, strictURI, allowDisclose, debug bool) *dealer {
	d := &dealer{
		procRegMap:    map[wamp.URI]*registration{},
		pfxProcRegMap: map[wamp.URI]*registration{},
		wcProcRegMap:  map[wamp.URI]*registration{},

		registrations: map[wamp.ID]*registration{},

		calls:            map[requestID]*wamp.Session{},
		invocations:      map[wamp.ID]*invocation{},
		invocationByCall: map[requestID]wamp.ID{},
		calleeRegIDSet:   map[*wamp.Session]map[wamp.ID]struct{}{},

		// The action handler should be nearly always runable, since it is the
		// critical section that does the only routing.  So, and unbuffered
		// channel is appropriate.
		actionChan: make(chan func()),

		idGen: new(wamp.IDGen),
		prng:  rand.New(rand.NewSource(time.Now().Unix())),

		strictURI:     strictURI,
		allowDisclose: allowDisclose,

		log:   logger,
		debug: debug,
	}
	go d.run()
	return d
}

// setMetaPeer sets the client that the dealer uses to publish meta events.
func (d *dealer) setMetaPeer(metaPeer wamp.Peer) {
	d.actionChan <- func() {
		d.metaPeer = metaPeer
	}
}

// role returns the role information for the "dealer" role.  The data returned
// is suitable for use as broker role info in a WELCOME message.
func (d *dealer) role() wamp.Dict {
	return dealerRole
}

// register registers a callee to handle calls to a procedure.
//
// If the shared_registration feature is supported, and if allowed by the
// invocation policy, multiple callees may register to handle the same
// procedure.
func (d *dealer) register(callee *wamp.Session, msg *wamp.Register) {
	if callee == nil || msg == nil {
		panic("dealer.Register with nil session or message")
	}

	// Validate procedure URI.  For REGISTER, must be valid URI (either strict
	// or loose), and all URI components must be non-empty other than for
	// wildcard or prefix matched procedures.
	match, _ := wamp.AsString(msg.Options[wamp.OptMatch])
	if !msg.Procedure.ValidURI(d.strictURI, match) {
		errMsg := fmt.Sprintf(
			"register for invalid procedure URI %v (URI strict checking %v)",
			msg.Procedure, d.strictURI)
		d.trySend(callee, &wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Error:     wamp.ErrInvalidURI,
			Arguments: wamp.List{errMsg},
		})
		return
	}

	wampURI := strings.HasPrefix(string(msg.Procedure), "wamp.")

	// Disallow registration of procedures starting with "wamp." by sessions
	// other then the meta session.
	if wampURI && callee.ID != metaID {
		errMsg := fmt.Sprintf("register for restricted procedure URI %v",
			msg.Procedure)
		d.trySend(callee, &wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Error:     wamp.ErrInvalidURI,
			Arguments: wamp.List{errMsg},
		})
		return
	}

	// If callee requests disclosure of caller identity, but dealer does not
	// allow, then send error as registration response.
	disclose, _ := msg.Options[wamp.OptDiscloseCaller].(bool)
	// allow disclose for trusted clients
	if !d.allowDisclose && disclose {
		callee.Lock()
		authrole, _ := wamp.AsString(callee.Details["authrole"])
		callee.Unlock()
		if authrole != "trusted" {
			d.trySend(callee, &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrOptionDisallowedDiscloseMe,
			})
			return
		}
	}

	invoke, _ := wamp.AsString(msg.Options[wamp.OptInvoke])
	var metaPubs []*wamp.Publish
	done := make(chan struct{})
	d.actionChan <- func() {
		metaPubs = d.syncRegister(callee, msg, match, invoke, disclose, wampURI)
		close(done)
	}
	<-done
	for _, pub := range metaPubs {
		d.metaPeer.Send(pub)
	}
}

// unregister removes a remote procedure previously registered by the callee.
func (d *dealer) unregister(callee *wamp.Session, msg *wamp.Unregister) {
	if callee == nil || msg == nil {
		panic("dealer.Unregister with nil session or message")
	}
	var metaPubs []*wamp.Publish
	done := make(chan struct{})
	d.actionChan <- func() {
		metaPubs = d.syncUnregister(callee, msg)
		close(done)
	}
	<-done
	for _, pub := range metaPubs {
		d.metaPeer.Send(pub)
	}

}

// call invokes a registered remote procedure.
func (d *dealer) call(caller *wamp.Session, msg *wamp.Call) {
	if caller == nil || msg == nil {
		panic("dealer.Call with nil session or message")
	}
	d.actionChan <- func() {
		d.syncCall(caller, msg)
	}
}

// cancel actively cancels a call that is in progress.
//
// Cancellation behaves differently depending on the mode:
//
// "skip": The pending call is canceled and ERROR is send immediately back to
// the caller.  No INTERRUPT is sent to the callee and the result is discarded
// when received.
//
// "kill": INTERRUPT is sent to the client, but ERROR is not returned to the
// caller until after the callee has responded to the canceled call.  In this
// case the caller may receive RESULT or ERROR depending whether the callee
// finishes processing the invocation or the interrupt first.
//
// "killnowait": The pending call is canceled and ERROR is send immediately
// back to the caller.  INTERRUPT is sent to the callee and any response to the
// invocation or interrupt from the callee is discarded when received.
//
// If the callee does not support call canceling, then behavior is "skip".
func (d *dealer) cancel(caller *wamp.Session, msg *wamp.Cancel) {
	if caller == nil || msg == nil {
		panic("dealer.Cancel with nil session or message")
	}
	// Cancel mode should be one of: "skip", "kill", "killnowait"
	mode, _ := wamp.AsString(msg.Options[wamp.OptMode])
	switch mode {
	case wamp.CancelModeKillNoWait, wamp.CancelModeKill, wamp.CancelModeSkip:
	case "":
		mode = wamp.CancelModeKillNoWait
	default:
		d.trySend(caller, &wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Error:     wamp.ErrInvalidArgument,
			Arguments: wamp.List{fmt.Sprint("invalid cancel mode ", mode)},
		})
		return
	}
	d.actionChan <- func() {
		d.syncCancel(caller, msg, mode, wamp.ErrCanceled)
	}
}

// yield handles the result of successfully processing and finishing the
// execution of a call, send from callee to dealer.
//
// If the RESULT could not be sent to the caller because the caller was blocked
// (send queue full), then retry sending until timeout.  If timeout while
// trying to send RESULT, then cancel call.
func (d *dealer) yield(callee *wamp.Session, msg *wamp.Yield) {
	if callee == nil || msg == nil {
		panic("dealer.Yield with nil session or message")
	}
	var again bool
	done := make(chan struct{})
	d.actionChan <- func() {
		again = d.syncYield(callee, msg, true)
		done <- struct{}{}
	}
	<-done

	// If blocked, retry
	if again {
		retry := true
		delay := yieldRetryDelay
		start := time.Now()
		// Retry processing YIELD until caller gone or deadline reached
		for {
			if d.debug {
				d.log.Println("Retry sending RESULT after", delay)
			}
			<-time.After(delay)
			// Do not retry if the elapsed time exceeds deadline
			if time.Since(start) >= sendResultDeadline {
				retry = false
			}
			d.actionChan <- func() {
				again = d.syncYield(callee, msg, retry)
				done <- struct{}{}
			}
			<-done
			if !again {
				break
			}
			delay *= 2
		}
	}
}

// error handles an invocation error returned by the callee.
func (d *dealer) error(msg *wamp.Error) {
	if msg == nil {
		panic("dealer.Error with nil message")
	}
	d.actionChan <- func() {
		d.syncError(msg)
	}
}

// removeSessiom removes a callee's registrations.  This is called when a
// client leaves the realm by sending a GOODBYE message or by disconnecting
// from the router.  If there are any registrations for this session
// wamp.registration.on_unregister and wamp.registration.on_delete meta events
// are published for each.
func (d *dealer) removeSession(sess *wamp.Session) {
	if sess == nil {
		// No session specified, no session removed.
		return
	}
	// Meta events must be returned by removeSession and must not be sent to
	// metaPeer while running inside the dealer goroutine.  Sending to metaPeer
	// from inside the dealer goroutine can deadlock since metaPeer may alredy
	// be waiting for the dealer goroutine to process a yield.
	var metaPubs []*wamp.Publish
	done := make(chan struct{})
	d.actionChan <- func() {
		metaPubs = d.syncRemoveSession(sess)
		close(done)
	}
	<-done
	for _, pub := range metaPubs {
		d.metaPeer.Send(pub)
	}
}

// close stops the dealer, letting already queued actions finish.
func (d *dealer) close() {
	close(d.actionChan)
}

func (d *dealer) run() {
	for action := range d.actionChan {
		action()
	}
	if d.debug {
		d.log.Print("Dealer stopped")
	}
}

func (d *dealer) syncRegister(callee *wamp.Session, msg *wamp.Register, match, invokePolicy string, disclose, wampURI bool) []*wamp.Publish {
	var metaPubs []*wamp.Publish
	var reg *registration
	switch match {
	default:
		reg = d.procRegMap[msg.Procedure]
	case wamp.MatchPrefix:
		reg = d.pfxProcRegMap[msg.Procedure]
	case wamp.MatchWildcard:
		reg = d.wcProcRegMap[msg.Procedure]
	}

	var created string
	var regID wamp.ID
	// If no existing registration found for the procedure, then create a new
	// registration.
	if reg == nil {
		regID = d.idGen.Next()
		created = wamp.NowISO8601()
		reg = &registration{
			id:        regID,
			procedure: msg.Procedure,
			created:   created,
			match:     match,
			policy:    invokePolicy,
			disclose:  disclose,
			callees:   []*wamp.Session{callee},
		}
		d.registrations[regID] = reg
		switch match {
		default:
			d.procRegMap[msg.Procedure] = reg
		case wamp.MatchPrefix:
			d.pfxProcRegMap[msg.Procedure] = reg
		case wamp.MatchWildcard:
			d.wcProcRegMap[msg.Procedure] = reg
		}

		if !wampURI && d.metaPeer != nil {
			// wamp.registration.on_create is fired when a registration is
			// created through a registration request for an URI which was
			// previously without a registration.
			details := wamp.Dict{
				"id":           regID,
				"created":      created,
				"uri":          msg.Procedure,
				wamp.OptMatch:  match,
				wamp.OptInvoke: invokePolicy,
			}
			metaPubs = append(metaPubs, &wamp.Publish{
				Request:   wamp.GlobalID(),
				Topic:     wamp.MetaEventRegOnCreate,
				Arguments: wamp.List{callee.ID, details},
			})
		}
	} else {
		// There is an existing registration(s) for this procedure.  See if
		// invocation policy allows another.

		// Found an existing registration that has an invocation strategy that
		// only allows a single callee on a the given registration.
		if reg.policy == "" || reg.policy == wamp.InvokeSingle {
			d.log.Println("REGISTER for already registered procedure",
				msg.Procedure, "from callee", callee)
			d.trySend(callee, &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrProcedureAlreadyExists,
			})
			return metaPubs
		}

		// Found an existing registration that has an invocation strategy
		// different from the one requested by the new callee
		if reg.policy != invokePolicy {
			d.log.Println("REGISTER for already registered procedure",
				msg.Procedure, "with conflicting invocation policy (has",
				reg.policy, "and requested", invokePolicy)
			d.trySend(callee, &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrProcedureAlreadyExists,
			})
			return metaPubs
		}

		regID = reg.id

		// Add callee for the registration.
		reg.callees = append(reg.callees, callee)
	}

	// Add the registration ID to the callees set of registrations.
	if _, ok := d.calleeRegIDSet[callee]; !ok {
		d.calleeRegIDSet[callee] = map[wamp.ID]struct{}{}
	}
	d.calleeRegIDSet[callee][regID] = struct{}{}

	if d.debug {
		d.log.Printf("Registered procedure %v (regID=%v) to callee %v",
			msg.Procedure, regID, callee)
	}
	d.trySend(callee, &wamp.Registered{
		Request:      msg.Request,
		Registration: regID,
	})

	if !wampURI && d.metaPeer != nil {
		// Publish wamp.registration.on_register meta event.  Fired when a
		// session is added to a registration.  A wamp.registration.on_register
		// event MUST be fired subsequent to a wamp.registration.on_create
		// event, since the first registration results in both the creation of
		// the registration and the addition of a session.
		metaPubs = append(metaPubs, &wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnRegister,
			Arguments: wamp.List{callee.ID, regID},
		})
	}
	return metaPubs
}

func (d *dealer) syncUnregister(callee *wamp.Session, msg *wamp.Unregister) []*wamp.Publish {
	var metaPubs []*wamp.Publish
	// Delete the registration ID from the callee's set of registrations.
	if _, ok := d.calleeRegIDSet[callee]; ok {
		delete(d.calleeRegIDSet[callee], msg.Registration)
		if len(d.calleeRegIDSet[callee]) == 0 {
			delete(d.calleeRegIDSet, callee)
		}
	}

	delReg, err := d.syncDelCalleeReg(callee, msg.Registration)
	if err != nil {
		d.log.Println("Cannot unregister:", err)
		d.trySend(callee, &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchRegistration,
		})
		return metaPubs
	}

	d.trySend(callee, &wamp.Unregistered{Request: msg.Request})

	if d.metaPeer == nil {
		return metaPubs
	}

	// Publish wamp.registration.on_unregister meta event.  Fired when a
	// session is removed from a subscription.
	metaPubs = append(metaPubs, &wamp.Publish{
		Request:   wamp.GlobalID(),
		Topic:     wamp.MetaEventRegOnUnregister,
		Arguments: wamp.List{callee.ID, msg.Registration},
	})

	if delReg {
		// Publish wamp.registration.on_delete meta event.  Fired when a
		// registration is deleted after the last session attached to it has
		// been removed.  The wamp.registration.on_delete event MUST be
		// preceded by a wamp.registration.on_unregister event.
		metaPubs = append(metaPubs, &wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnDelete,
			Arguments: wamp.List{callee.ID, msg.Registration},
		})
	}
	return metaPubs
}

// syncMatchProcedure finds the best matching registration given a procedure
// URI.
//
// If there are both matching prefix and wildcard registrations, then find the
// one with the more specific match (longest matched pattern).
func (d *dealer) syncMatchProcedure(procedure wamp.URI) (*registration, bool) {
	// Find registered procedures with exact match.
	reg, ok := d.procRegMap[procedure]
	if !ok {
		// No exact match was found.  So, search for a prefix or wildcard
		// match, and prefer the most specific math (longest matched pattern).
		// If there is a tie, then prefer the first longest prefix.
		matchCount := -1 // initialize matchCount to -1 to catch an empty registration.
		for pfxProc, pfxReg := range d.pfxProcRegMap {
			if procedure.PrefixMatch(pfxProc) {
				if len(pfxProc) > matchCount {
					reg = pfxReg
					matchCount = len(pfxProc)
					ok = true
				}
			}
		}
		// According to the spec, we have to prefer prefix match over wildcard
		// match:
		// https://wamp-proto.org/static/rfc/draft-oberstet-hybi-crossbar-wamp.html#rfc.section.14.3.8.1.4.2
		if ok {
			return reg, ok
		}

		for wcProc, wcReg := range d.wcProcRegMap {
			if procedure.WildcardMatch(wcProc) {
				if len(wcProc) > matchCount {
					reg = wcReg
					matchCount = len(wcProc)
					ok = true
				}
			}
		}
	}
	return reg, ok
}

func (d *dealer) syncCall(caller *wamp.Session, msg *wamp.Call) {
	reg, ok := d.syncMatchProcedure(msg.Procedure)
	if !ok || len(reg.callees) == 0 {
		// If no registered procedure, send error.
		d.trySend(caller, &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchProcedure,
		})
		return
	}

	var callee *wamp.Session

	// If there are multiple callees, then select a callee based invocation
	// policy.
	if len(reg.callees) > 1 {
		switch reg.policy {
		case wamp.InvokeFirst:
			callee = reg.callees[0]
		case wamp.InvokeRoundRobin:
			if reg.nextCallee >= len(reg.callees) {
				reg.nextCallee = 0
			}
			callee = reg.callees[reg.nextCallee]
			reg.nextCallee++
		case wamp.InvokeRandom:
			callee = reg.callees[d.prng.Int63n(int64(len(reg.callees)))]
		case wamp.InvokeLast:
			callee = reg.callees[len(reg.callees)-1]
		default:
			errMsg := fmt.Sprint("multiple callees registered for ",
				msg.Procedure, " with '", wamp.InvokeSingle, "' policy")
			// This is disallowed by the dealer, and is a programming error if
			// it ever happened, so panic.
			panic(errMsg)
		}
	} else {
		callee = reg.callees[0]
	}
	details := wamp.Dict{}

	// A Caller might want to issue a call providing a timeout for the call to
	// finish.
	//
	// A timeout allows to automatically cancel a call after a specified time
	// either at the Callee or at the Dealer.
	timeout, _ := wamp.AsInt64(msg.Options[wamp.OptTimeout])
	if timeout > 0 {
		// Check that callee supports call_timeout.
		if callee.HasFeature(roleCallee, featureCallTimeout) {
			details[wamp.OptTimeout] = timeout
		}

		// TODO: Start a goroutine to cancel the pending call on timeout.
		// Should be implemented like Cancel with mode=killnowait, and error
		// message argument should say "call timeout"
	}

	// TODO: handle trust levels

	// If the callee has requested disclosure of caller identity when the
	// registration was created, and this was allowed by the dealer.
	if reg.disclose {
		if callee.ID == metaID {
			details[roleCaller] = caller.ID
		}
		discloseCaller(caller, details)
	} else {
		// A Caller MAY request the disclosure of its identity (its WAMP
		// session ID) to endpoints of a routed call.  This is indicated by the
		// "disclose_me" flag in the message options.
		if opt, _ := msg.Options[wamp.OptDiscloseMe].(bool); opt {
			// Dealer MAY deny a Caller's request to disclose its identity.
			if !d.allowDisclose {
				// Do not continue a call when discloseMe was disallowed.
				d.trySend(caller, &wamp.Error{
					Type:    msg.MessageType(),
					Request: msg.Request,
					Details: wamp.Dict{},
					Error:   wamp.ErrOptionDisallowedDiscloseMe,
				})
				return
			}
			if callee.HasFeature(roleCallee, featureCallerIdent) {
				discloseCaller(caller, details)
			}
		}
	}

	// A Caller indicates its willingness to receive progressive results by
	// setting CALL.Options.receive_progress|bool := true
	if opt, _ := msg.Options[wamp.OptReceiveProgress].(bool); opt {
		// If the Callee supports progressive calls, the Dealer will forward
		// the Caller's willingness to receive progressive results by setting.
		//
		// The Callee must support call canceling, as this is necessary to stop
		// progressive results if the caller session is closed during
		// progressive result delivery.
		if callee.HasFeature(roleCallee, featureProgCallResults) && callee.HasFeature(roleCallee, featureCallCanceling) {
			details[wamp.OptReceiveProgress] = true
		}
	}

	if reg.match != wamp.MatchExact {
		// According to the spec, a router must provide the actual procedure to
		// the client.
		details[wamp.OptProcedure] = msg.Procedure
	}

	reqID := requestID{
		session: caller.ID,
		request: msg.Request,
	}
	d.calls[reqID] = caller
	invocationID := d.idGen.Next()
	d.invocations[invocationID] = &invocation{
		callID: reqID,
		callee: callee,
	}
	d.invocationByCall[reqID] = invocationID

	// Send INVOCATION to the endpoint that has registered the requested
	// procedure.
	if !d.trySend(callee, &wamp.Invocation{
		Request:      invocationID,
		Registration: reg.id,
		Details:      details,
		Arguments:    msg.Arguments,
		ArgumentsKw:  msg.ArgumentsKw,
	}) {
		d.syncError(&wamp.Error{
			Type:      wamp.INVOCATION,
			Request:   invocationID,
			Details:   wamp.Dict{},
			Error:     wamp.ErrNetworkFailure,
			Arguments: wamp.List{"callee blocked - cannot call procedure"},
		})
	}
}

func (d *dealer) syncCancel(caller *wamp.Session, msg *wamp.Cancel, mode string, reason wamp.URI) {
	reqID := requestID{
		session: caller.ID,
		request: msg.Request,
	}
	procCaller, ok := d.calls[reqID]
	if !ok {
		// There is no pending call to cancel.
		return
	}

	// Check if the caller of cancel is also the caller of the procedure.
	if caller != procCaller {
		// The caller is trying to cancel calls that it does not own.  It it
		// either confused or trying to do something bad.
		d.log.Println("CANCEL received from caller", caller,
			"for call owned by different session")
		return
	}

	// Find the pending invocation.
	invocationID, ok := d.invocationByCall[reqID]
	if !ok {
		// If there is no pending invocation, ignore cancel.
		d.log.Print("Found call with no pending invocation")
		return
	}
	invk, ok := d.invocations[invocationID]
	if !ok {
		d.log.Print("CRITICAL: missing caller for pending invocation")
		return
	}
	// For those who repeatedly press elevator buttons.
	if invk.canceled {
		return
	}
	invk.canceled = true

	// If mode is "kill" or "killnowait", then send INTERRUPT.
	if mode != wamp.CancelModeSkip {
		// Check that callee supports call canceling to see if it is alright to
		// send INTERRUPT to callee.
		if !invk.callee.HasFeature(roleCallee, featureCallCanceling) {
			// Cancel in dealer without sending INTERRUPT to callee.
			d.log.Println("Callee", invk.callee, "does not support call canceling")
		} else {
			// Send INTERRUPT message to callee.
			if d.trySend(invk.callee, &wamp.Interrupt{
				Request: invocationID,
				Options: wamp.Dict{wamp.OptReason: reason, wamp.OptMode: mode},
			}) {
				d.log.Println("Dealer sent INTERRUPT to cancel invocation",
					invocationID, "for call", msg.Request, "mode:", mode)

				// If mode is "kill" then let error from callee trigger the
				// response to the caller.  This is how the caller waits for
				// the callee to cancel the call.
				if mode == wamp.CancelModeKill {
					return
				}
			}
		}
	}
	// Treat any unrecognized mode the same as "skip".

	// Immediately delete the pending call and send ERROR back to the
	// caller.  This will cause any RESULT or ERROR arriving later from the
	// callee to be dropped.
	//
	// This also stops repeated CANCEL messages.
	delete(d.calls, reqID)
	delete(d.invocationByCall, reqID)
	delete(d.invocations, invocationID)

	// Send error to the caller.
	d.trySend(caller, &wamp.Error{
		Type:    wamp.CALL,
		Request: msg.Request,
		Error:   reason,
		Details: wamp.Dict{},
	})
}

func (d *dealer) syncYield(callee *wamp.Session, msg *wamp.Yield, canRetry bool) bool {
	progress, _ := msg.Options[wamp.OptProgress].(bool)

	// Find and delete pending invocation.
	invk, ok := d.invocations[msg.Request]
	if !ok {
		// The pending invocation is gone, which means the caller has left the
		// realm or canceled the call.
		//
		// Send INTERRUPT to cancel progressive results.
		if progress {
			// It is alright to send an INTERRUPT to the callee, since the
			// callee's progressive call results feature would have been
			// disabled at registration time if the callee did not support call
			// canceling.
			if d.trySend(callee, &wamp.Interrupt{
				Request: msg.Request,
				Options: wamp.Dict{wamp.OptMode: wamp.CancelModeKillNoWait},
			}) {
				d.log.Println("Dealer sent INTERRUPT to cancel progressive",
					"results for request", msg.Request, "to callee", callee)
			}
		} else {
			// WAMP does not allow sending INTERRUPT in response to normal or
			// final YIELD message.
			d.log.Println("YIELD received with unknown invocation request ID:",
				msg.Request)
		}
		return false
	}

	// Make sure this yield was sent by the session that handled the call
	if invk.callee != callee {
		d.log.Println("Ignoring YIELD received from session", callee, "that does not own request", msg.Request)
		return false
	}

	callID := invk.callID
	// Find caller for this result.
	caller, ok := d.calls[callID]

	details := wamp.Dict{}

	var keepInvocation bool
	if progress {
		// If this is a progressive response, then set progress=true.
		details[wamp.OptProgress] = true
	} else {
		// Clean up the invocation, unless need to retry.
		defer func() {
			if keepInvocation {
				return
			}
			delete(d.invocations, msg.Request)
			// Delete callID -> invocation.
			delete(d.invocationByCall, callID)
			// Delete pending call since it is finished.
			delete(d.calls, callID)
		}()
	}

	// Did not find caller.
	if !ok {
		// Found invocation id that does not have any call id.
		d.log.Println("!!! No matching caller for invocation from YIELD:",
			msg.Request)
		return false
	}

	// Send RESULT to the caller.  If the caller is blocked, then make the
	// callee wait and retry sending this message again.  The caller may be
	// blocked when the callee is generating progressive responses faster than
	// the caller can handle them.
	res := &wamp.Result{
		Request:     callID.request,
		Details:     details,
		Arguments:   msg.Arguments,
		ArgumentsKw: msg.ArgumentsKw,
	}
	err := caller.TrySend(res)
	if err != nil {
		if canRetry {
			keepInvocation = true
			return true
		}
		d.log.Printf("!!! Dropped %s to caller %s: %s", res.MessageType(), caller, err)
		d.syncCancel(caller, &wamp.Cancel{Request: callID.request},
			wamp.CancelModeKillNoWait, wamp.ErrCanceled)
	}
	return false
}

func (d *dealer) syncError(msg *wamp.Error) {
	// Find and delete pending invocation.
	invk, ok := d.invocations[msg.Request]
	if !ok {
		d.log.Println("Received ERROR (INVOCATION) with invalid request ID:",
			msg.Request, "(response to canceled call)")
		return
	}
	delete(d.invocations, msg.Request)
	callID := invk.callID

	// Delete invocationsByCall entry.  This will already be deleted if the
	// call canceled with mode "skip" or "killnowait".
	delete(d.invocationByCall, callID)

	// Find and delete pending call.  This will already be deleted if the
	// call canceled with mode "skip" or "killnowait".
	caller, ok := d.calls[callID]
	if !ok {
		d.log.Println("Received ERROR for call that was already canceled:",
			callID)
		return
	}
	delete(d.calls, callID)

	// Send error to the caller.
	d.trySend(caller, &wamp.Error{
		Type:        wamp.CALL,
		Request:     callID.request,
		Error:       msg.Error,
		Details:     msg.Details,
		Arguments:   msg.Arguments,
		ArgumentsKw: msg.ArgumentsKw,
	})
}

func (d *dealer) syncRemoveSession(sess *wamp.Session) []*wamp.Publish {
	var metaPubs []*wamp.Publish
	// Remove any remaining registrations for the removed session.
	for regID := range d.calleeRegIDSet[sess] {
		delReg, err := d.syncDelCalleeReg(sess, regID)
		if err != nil {
			panic("!!! Callee had ID of nonexistent registration")
		}

		if d.metaPeer == nil {
			continue
		}

		// Publish wamp.registration.on_unregister meta event.  Fired when a
		// callee session is removed from a registration.
		metaPubs = append(metaPubs, &wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnUnregister,
			Arguments: wamp.List{sess.ID, regID},
		})

		if !delReg {
			continue
		}
		// Publish wamp.registration.on_delete meta event.  Fired when a
		// registration is deleted after the last session attached to it
		// has been removed.  The wamp.registration.on_delete event MUST be
		// preceded by a wamp.registration.on_unregister event.
		metaPubs = append(metaPubs, &wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnDelete,
			Arguments: wamp.List{sess.ID, regID},
		})
	}
	delete(d.calleeRegIDSet, sess)

	// Remove any pending calls for the removed session.
	for req, caller := range d.calls {
		if caller != sess {
			continue
		}
		// Removed session has pending call.
		delete(d.calls, req)

		// If there is a pending invocation for the call, remove it.
		if invkID, ok := d.invocationByCall[req]; ok {
			delete(d.invocationByCall, req)
			delete(d.invocations, invkID)
		}
	}
	return metaPubs
}

// syncDelCalleeReg deletes the the callee from the specified registration and
// deletes the registration from the set of registrations for the callee.
//
// If there are no more callees for the registration, then the registration is
// removed and true is returned to indicate that the last registration was
// deleted.
func (d *dealer) syncDelCalleeReg(callee *wamp.Session, regID wamp.ID) (bool, error) {
	reg, ok := d.registrations[regID]
	if !ok {
		// The registration doesn't exist
		return false, fmt.Errorf("no such registration: %v", regID)
	}

	// Remove the callee from the registration.
	for i := range reg.callees {
		if reg.callees[i] == callee {
			if d.debug {
				d.log.Printf("Unregistered procedure %v (regID=%v) (callee=%v)",
					reg.procedure, regID, callee.ID)
			}
			if len(reg.callees) == 1 {
				reg.callees = nil
			} else {
				// Delete preserving order.
				reg.callees = append(reg.callees[:i], reg.callees[i+1:]...)
			}
			break
		}
	}

	// If no more callees for this registration, then delete the registration
	// according to what match type it is.
	if len(reg.callees) == 0 {
		delete(d.registrations, regID)
		switch reg.match {
		default:
			delete(d.procRegMap, reg.procedure)
		case wamp.MatchPrefix:
			delete(d.pfxProcRegMap, reg.procedure)
		case wamp.MatchWildcard:
			delete(d.wcProcRegMap, reg.procedure)
		}
		if d.debug {
			d.log.Printf("Deleted registration %v for procedure %v", regID,
				reg.procedure)
		}
		return true, nil
	}
	return false, nil
}

// ----- Meta Procedure Handlers -----

// regList retrieves registration IDs listed according to match policies.
func (d *dealer) regList(msg *wamp.Invocation) wamp.Message {
	var exactRegs, pfxRegs, wcRegs []wamp.ID
	sync := make(chan struct{})
	d.actionChan <- func() {
		for _, reg := range d.procRegMap {
			exactRegs = append(exactRegs, reg.id)
		}
		for _, reg := range d.pfxProcRegMap {
			pfxRegs = append(pfxRegs, reg.id)
		}
		for _, reg := range d.wcProcRegMap {
			wcRegs = append(wcRegs, reg.id)
		}
		close(sync)
	}
	<-sync
	dict := wamp.Dict{
		wamp.MatchExact:    exactRegs,
		wamp.MatchPrefix:   pfxRegs,
		wamp.MatchWildcard: wcRegs,
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{dict},
	}
}

// regLookup obtains the registration (if any) managing a procedure, according
// to some match policy.
func (d *dealer) regLookup(msg *wamp.Invocation) wamp.Message {
	var regID wamp.ID
	if len(msg.Arguments) != 0 {
		if procedure, ok := wamp.AsURI(msg.Arguments[0]); ok {
			var match string
			if len(msg.Arguments) > 1 {
				if opts, ok := wamp.AsDict(msg.Arguments[1]); ok {
					match, _ = wamp.AsString(opts[wamp.OptMatch])
				}
			}
			sync := make(chan wamp.ID)
			d.actionChan <- func() {
				var r wamp.ID
				var reg *registration
				var ok bool
				switch match {
				default:
					reg, ok = d.procRegMap[procedure]
				case wamp.MatchPrefix:
					reg, ok = d.pfxProcRegMap[procedure]
				case wamp.MatchWildcard:
					reg, ok = d.wcProcRegMap[procedure]
				}
				if ok {
					r = reg.id
				}
				sync <- r
			}
			regID = <-sync
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{regID},
	}
}

// regMatch obtains the registration best matching a given procedure URI.
func (d *dealer) regMatch(msg *wamp.Invocation) wamp.Message {
	var regID wamp.ID
	if len(msg.Arguments) != 0 {
		if procedure, ok := wamp.AsURI(msg.Arguments[0]); ok {
			sync := make(chan wamp.ID)
			d.actionChan <- func() {
				var r wamp.ID
				if reg, ok := d.syncMatchProcedure(procedure); ok {
					r = reg.id
				}
				sync <- r
			}
			regID = <-sync
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{regID},
	}
}

// regGet retrieves information on a particular registration.
func (d *dealer) regGet(msg *wamp.Invocation) wamp.Message {
	var dict wamp.Dict
	if len(msg.Arguments) != 0 {
		if regID, ok := wamp.AsID(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			d.actionChan <- func() {
				if reg, ok := d.registrations[regID]; ok {
					dict = wamp.Dict{
						"id":           regID,
						"created":      reg.created,
						"uri":          reg.procedure,
						wamp.OptMatch:  reg.match,
						wamp.OptInvoke: reg.policy,
					}
				}
				close(sync)
			}
			<-sync
		}
	}
	if dict == nil {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchRegistration,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{dict},
	}
}

// regListCallees retrieves a list of session IDs for sessions currently
// attached to the registration.
func (d *dealer) regListCallees(msg *wamp.Invocation) wamp.Message {
	var calleeIDs []wamp.ID
	if len(msg.Arguments) != 0 {
		if regID, ok := wamp.AsID(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			d.actionChan <- func() {
				if reg, ok := d.registrations[regID]; ok {
					calleeIDs = make([]wamp.ID, len(reg.callees))
					for i := range reg.callees {
						calleeIDs[i] = reg.callees[i].ID
					}
				}
				close(sync)
			}
			<-sync
		}
	}
	if calleeIDs == nil {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchRegistration,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{calleeIDs},
	}
}

// regCountCallees obtains the number of sessions currently attached to the
// registration.
func (d *dealer) regCountCallees(msg *wamp.Invocation) wamp.Message {
	var count int
	var ok bool
	if len(msg.Arguments) != 0 {
		var regID wamp.ID
		if regID, ok = wamp.AsID(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			d.actionChan <- func() {
				if reg, found := d.registrations[regID]; found {
					count = len(reg.callees)
				} else {
					ok = false
				}
				close(sync)
			}
			<-sync
		}
	}
	if !ok {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchRegistration,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{count},
	}
}

func (d *dealer) trySend(sess *wamp.Session, msg wamp.Message) bool {
	if err := sess.TrySend(msg); err != nil {
		d.log.Printf("!!! Dropped %s to session %s: %s", msg.MessageType(), sess, err)
		return false
	}
	return true
}

// discloseCaller adds caller identity information to INVOCATION.Details.
func discloseCaller(caller *wamp.Session, details wamp.Dict) {
	details[roleCaller] = caller.ID
	// These values are not required by the specification, but are here for
	// compatibility with Crossbar.
	caller.Lock()
	for _, f := range []string{"authid", "authrole"} {
		if val, ok := caller.Details[f]; ok {
			details[fmt.Sprintf("%s_%s", roleCaller, f)] = val
		}
	}
	caller.Unlock()
}
