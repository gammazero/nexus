package router

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/gammazero/nexus/wamp"
)

// TODO: Implement the following:
// - call timeout (need timeout goroutine)
// - call trust levels

// Features supported by this dealer.
var dealerFeatures = map[string]interface{}{
	"features": map[string]interface{}{
		"call_canceling":             true,
		"call_timeout":               true,
		"caller_identification":      true,
		"pattern_based_registration": true,
		"progressive_call_results":   true,
		"shared_registration":        true,
		"registration_meta_api":      true,
	},
}

// Dealers route calls incoming from Callers to Callees implementing the
// procedure called, and route call results back from Callees to Callers.
type Dealer interface {
	// Register registers a callee to handle calls to a procedure.
	//
	// If the shared_registration feature is supported, and if allowed by the
	// invocation policy, multiple callees may register to handle the same
	// procedure.
	Register(callee *Session, msg *wamp.Register)

	// Unregister removes a remote procedure previously registered by the
	// callee.
	Unregister(callee *Session, msg *wamp.Unregister)

	// Call invokes a registered remote procedure.
	Call(caller *Session, msg *wamp.Call)

	// Cancel actively cancels a call that is in progress.
	//
	// Cancellation behaves differently depending on the mode:
	//
	// - "skip": The pending call is canceled and ERROR is send immediately
	// back to the caller.  No INTERRUPT is sent to the callee and the result
	// is discarded when received.
	//
	// - "kill": INTERRUPT is sent to the client, but ERROR is not returned to
	// the caller until after the callee has responded to the canceled call.
	// In this case the caller may receive RESULT or ERROR depending whether
	// the callee finishes processing the invocation or the interrupt first.
	//
	// - "killnowait": The pending call is canceled and ERROR is send
	// immediately back to the caller.  INTERRUPT is sent to the callee and any
	// response to the invocation or interrupt from the callee is discarded
	// when received.
	//
	// If the callee does not support call canceling, then behavior is "skip".
	Cancel(caller *Session, msg *wamp.Cancel)

	// Yield handles the result of successfully processing and finishing the
	// execution of a call, send from callee to dealer.
	Yield(callee *Session, msg *wamp.Yield)

	// Error handles an invocation error returned by the callee.
	Error(msg *wamp.Error)

	// Remove a callee's registrations.
	RemoveSession(*Session)

	// Close shuts down the dealer.
	Close()

	// Features returns the features supported by this dealer.
	//
	// The data returned is suitable for use as the "features" section of the
	// dealer role in a WELCOME message.
	Features() map[string]interface{}

	// RegList retrieves registration IDs listed according to match policies.
	RegList(*wamp.Invocation) wamp.Message

	// RegLookup retrieves registration IDs listed according to match policies.
	RegLookup(*wamp.Invocation) wamp.Message

	// RegMatch obtains the registration best matching a given procedure URI.
	RegMatch(*wamp.Invocation) wamp.Message

	// RegGet retrieves information on a particular registration.
	RegGet(*wamp.Invocation) wamp.Message

	// RegListCallees retrieves a list of session IDs for sessions currently
	// attached to the registration.
	RegListCallees(*wamp.Invocation) wamp.Message

	// RegCountCallees obtains the number of sessions currently attached to the
	// registration.
	RegCountCallees(*wamp.Invocation) wamp.Message
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
	callees []*Session
}

// invocation tracks in-progress invocation
type invocation struct {
	callID   wamp.ID
	callee   *Session
	canceled bool
}

// Features returns the features supported by this dealer.
func (d *dealer) Features() map[string]interface{} {
	return dealerFeatures
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
	calls map[wamp.ID]*Session

	// invocation ID -> {call ID, callee, canceled}
	invocations map[wamp.ID]*invocation

	// call ID -> invocation ID (for cancel)
	invocationByCall map[wamp.ID]wamp.ID

	// callee session -> registration ID set.
	// Used to lookup registrations when removing a callee session.
	calleeRegIDSet map[*Session]map[wamp.ID]struct{}

	actionChan chan func()

	// Generate registration IDs.
	idGen *wamp.IDGen

	// Used for round-robin call invocation.
	prng *rand.Rand

	// Dealer behavior flags.
	strictURI     bool
	allowDisclose bool

	metaClient wamp.Peer

	// Meta-procedure registration ID -> handler func.
	metaProcMap map[wamp.ID]func(*wamp.Invocation) wamp.Message
}

// NewDealer creates the default Dealer implementation.
//
// Messages are routed serially by the dealer's message handling goroutine.
// This serialization is limited to the work of determining the message's
// destination, and then the message is handed off to the next goroutine,
// typically the receiving client's send handler.
func NewDealer(strictURI, allowDisclose bool, metaClient wamp.Peer) Dealer {
	d := &dealer{
		procRegMap:    map[wamp.URI]*registration{},
		pfxProcRegMap: map[wamp.URI]*registration{},
		wcProcRegMap:  map[wamp.URI]*registration{},

		registrations: map[wamp.ID]*registration{},

		calls:            map[wamp.ID]*Session{},
		invocations:      map[wamp.ID]*invocation{},
		invocationByCall: map[wamp.ID]wamp.ID{},
		calleeRegIDSet:   map[*Session]map[wamp.ID]struct{}{},

		// The action handler should be nearly always runable, since it is the
		// critical section that does the only routing.  So, and unbuffered
		// channel is appropriate.
		actionChan: make(chan func()),

		idGen: wamp.NewIDGen(),
		prng:  rand.New(rand.NewSource(time.Now().Unix())),

		strictURI:     strictURI,
		allowDisclose: allowDisclose,

		metaClient: metaClient,
	}
	go d.run()
	return d
}

// Register registers a callee to handle calls to a procedure.
func (d *dealer) Register(callee *Session, msg *wamp.Register) {
	if callee == nil || msg == nil {
		panic("dealer.Register with nil session or message")
	}

	// Validate procedure URI.  For REGISTER, must be valid URI (either strict
	// or loose), and all URI components must be non-empty other than for
	// wildcard or prefix matched procedures.
	match := wamp.OptionString(msg.Options, "match")
	if !msg.Procedure.ValidURI(d.strictURI, match) {
		errMsg := fmt.Sprintf(
			"register for invalid procedure URI %v (URI strict checking %v)",
			msg.Procedure, d.strictURI)
		callee.Send(&wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Error:     wamp.ErrInvalidURI,
			Arguments: []interface{}{errMsg},
		})
		return
	}

	wampURI := strings.HasPrefix(string(msg.Procedure), "wamp.")

	// Disallow registration of procedures starting with "wamp.", except for
	// trusted sessions that are built into router.
	authrole := wamp.OptionString(callee.Details, "authrole")
	if authrole != "" && authrole != "trusted" {
		if wampURI {
			errMsg := fmt.Sprintf("register for restricted procedure URI %v",
				msg.Procedure)
			callee.Send(&wamp.Error{
				Type:      msg.MessageType(),
				Request:   msg.Request,
				Error:     wamp.ErrInvalidURI,
				Arguments: []interface{}{errMsg},
			})
			return
		}
	}

	// If callee requests disclosure of caller identity, but dealer does not
	// allow, then send error as registration response.
	discloseCaller := wamp.OptionFlag(msg.Options, "disclose_caller")
	if !d.allowDisclose && discloseCaller {
		callee.Send(&wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: map[string]interface{}{},
			Error:   wamp.ErrOptionDisallowedDiscloseMe,
		})
		return
	}

	invoke := wamp.OptionString(msg.Options, "invoke")
	d.actionChan <- func() {
		d.register(callee, msg, match, invoke, discloseCaller, wampURI)
	}
}

// Unregister removes a remote procedure previously registered by the callee.
func (d *dealer) Unregister(callee *Session, msg *wamp.Unregister) {
	if callee == nil || msg == nil {
		panic("dealer.Unregister with nil session or message")
	}
	d.actionChan <- func() {
		d.unregister(callee, msg)
	}
}

// Call invokes a registered remote procedure.
func (d *dealer) Call(caller *Session, msg *wamp.Call) {
	if caller == nil || msg == nil {
		panic("dealer.Call with nil session or message")
	}
	d.actionChan <- func() {
		d.call(caller, msg)
	}
}

// Cancel actively cancels a call that is in progress.
func (d *dealer) Cancel(caller *Session, msg *wamp.Cancel) {
	if caller == nil || msg == nil {
		panic("dealer.Cancel with nil session or message")
	}
	d.actionChan <- func() {
		d.cancel(caller, msg)
	}
}

// Yield handles the result of successfully processing and finishing the
// execution of a call, send from callee to dealer.
func (d *dealer) Yield(callee *Session, msg *wamp.Yield) {
	if callee == nil || msg == nil {
		panic("dealer.Yield with nil session or message")
	}
	d.actionChan <- func() {
		d.yield(callee, msg)
	}
}

func (d *dealer) Error(msg *wamp.Error) {
	if msg == nil {
		panic("dealer.Error with nil message")
	}
	d.actionChan <- func() {
		d.error(msg)
	}
}

func (d *dealer) RemoveSession(sess *Session) {
	if sess == nil {
		// No session specified, no session removed.
		return
	}
	d.actionChan <- func() {
		d.removeSession(sess)
	}
}

// Close stops the dealer and waits message processing to stop.
func (d *dealer) Close() {
	close(d.actionChan)
}

func (d *dealer) run() {
	for action := range d.actionChan {
		action()
	}
	log.Print("Dealer stopped")
}

func (d *dealer) register(callee *Session, msg *wamp.Register, match, invokePolicy string, discloseCaller, wampURI bool) {
	var reg *registration
	switch match {
	default:
		reg = d.procRegMap[msg.Procedure]
	case "prefix":
		reg = d.pfxProcRegMap[msg.Procedure]
	case "wildcard":
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
			disclose:  discloseCaller,
			callees:   []*Session{callee},
		}
		d.registrations[regID] = reg
		switch match {
		default:
			d.procRegMap[msg.Procedure] = reg
		case "prefix":
			d.pfxProcRegMap[msg.Procedure] = reg
		case "wildcard":
			d.wcProcRegMap[msg.Procedure] = reg
		}

		if !wampURI {
			// wamp.registration.on_create is fired when a registration is
			// created through a registration request for an URI which was
			// previously without a registration.
			details := map[string]interface{}{
				"id":      regID,
				"created": created,
				"uri":     msg.Procedure,
				"match":   match,
				"invoke":  invokePolicy,
			}
			d.metaClient.Send(&wamp.Publish{
				Request:   wamp.GlobalID(),
				Topic:     wamp.MetaEventRegOnCreate,
				Arguments: []interface{}{callee.ID, details},
			})
		}
	} else {
		// There is an existing registration(s) for this procedure.  See if
		// invocation policy allows another.

		// Found an existing registration that has an invocation strategy that
		// only allows a single callee on a the given registration.
		if reg.policy == "" || reg.policy == "single" {
			log.Println("REGISTER for already registered procedure",
				msg.Procedure, "from callee", callee)
			callee.Send(&wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: map[string]interface{}{},
				Error:   wamp.ErrProcedureAlreadyExists,
			})
			return
		}

		// Found an existing registration that has an invocation strategy
		// different from the one requested by the new callee
		if reg.policy != invokePolicy {
			log.Println("REGISTER for already registered procedure",
				msg.Procedure, "with conflicting invocation policy (has",
				reg.policy, "and", invokePolicy, "was requested")
			callee.Send(&wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: map[string]interface{}{},
				Error:   wamp.ErrProcedureAlreadyExists,
			})
			return
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

	log.Printf("Dealer registered procedure %v (regID=%v) to callee %v",
		msg.Procedure, regID, callee)
	callee.Send(&wamp.Registered{
		Request:      msg.Request,
		Registration: regID,
	})

	if !wampURI {
		// Publish wamp.registration.on_register meta event.  Fired when a
		// session is added to a registration.  A wamp.registration.on_register
		// event MUST be fired subsequent to a wamp.registration.on_create
		// event, since the first registration results in both the creation of
		// the registration and the addition of a session.
		d.metaClient.Send(&wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnRegister,
			Arguments: []interface{}{callee.ID, regID},
		})
	}
}

func (d *dealer) unregister(callee *Session, msg *wamp.Unregister) {
	// Delete the registration ID from the callee's set of registrations.
	if _, ok := d.calleeRegIDSet[callee]; ok {
		delete(d.calleeRegIDSet[callee], msg.Registration)
		if len(d.calleeRegIDSet[callee]) == 0 {
			delete(d.calleeRegIDSet, callee)
		}
	}

	delReg, err := d.delCalleeReg(callee, msg.Registration)
	if err != nil {
		log.Println("Cannot unregister:", err)
		callee.Send(&wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: map[string]interface{}{},
			Error:   wamp.ErrNoSuchRegistration,
		})
		return
	}

	callee.Send(&wamp.Unregistered{Request: msg.Request})

	// Publish wamp.registration.on_unregister meta event.  Fired when a
	// session is removed from a subscription.
	d.metaClient.Send(&wamp.Publish{
		Request:   wamp.GlobalID(),
		Topic:     wamp.MetaEventRegOnUnregister,
		Arguments: []interface{}{callee.ID, msg.Registration},
	})

	if delReg {
		// Publish wamp.registration.on_delete meta event.  Fired when a
		// registration is deleted after the last session attached to it has
		// been removed.  The wamp.registration.on_delete event MUST be
		// preceded by a wamp.registration.on_unregister event.
		d.metaClient.Send(&wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnDelete,
			Arguments: []interface{}{callee.ID, msg.Registration},
		})
	}
}

// matchProcedure finds the best matching registration given a procedure URI.
//
// If there are both matching prefix and wildcard registrations, then find the
// one with the more specific match (longest matched pattern).
func (d *dealer) matchProcedure(procedure wamp.URI) (*registration, bool) {
	// Find registered procedures with exact match.
	reg, ok := d.procRegMap[procedure]
	if !ok {
		// No exact match was found.  So, search for a prefix or wildcard
		// match, and prefer the most specific math (longest matched pattern).
		// If there is a tie, then prefer the first longest prefix.
		var matchCount int
		for pfxProc, pfxReg := range d.pfxProcRegMap {
			if procedure.PrefixMatch(pfxProc) {
				if len(pfxProc) > matchCount {
					reg = pfxReg
					matchCount = len(pfxProc)
					ok = true
				}
			}
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

func (d *dealer) call(caller *Session, msg *wamp.Call) {
	reg, ok := d.matchProcedure(msg.Procedure)
	if !ok || len(reg.callees) == 0 {
		// If no registered procedure, send error.
		caller.Send(&wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: map[string]interface{}{},
			Error:   wamp.ErrNoSuchProcedure,
		})
		return
	}

	var callee *Session

	// If there are multiple callees, then select a callee based invocation
	// policy.
	if len(reg.callees) > 1 {
		switch reg.policy {
		case "first":
			callee = reg.callees[0]
		case "roundrobin":
			if reg.nextCallee >= len(reg.callees) {
				reg.nextCallee = 0
			}
			callee = reg.callees[reg.nextCallee]
			reg.nextCallee++
		case "random":
			callee = reg.callees[d.prng.Int63n(int64(len(reg.callees)))]
		case "last":
			callee = reg.callees[len(reg.callees)-1]
		default:
			errMsg := fmt.Sprint("multiple callees registered for",
				msg.Procedure, "with 'single' policy")
			// This is disallowed by the dealer, and is a programming error if
			// it ever happened, so panic.
			panic(errMsg)
		}
	} else {
		callee = reg.callees[0]
	}
	details := map[string]interface{}{}

	// A Caller might want to issue a call providing a timeout for the call to
	// finish.
	//
	// A timeout allows to automatically cancel a call after a specified time
	// either at the Callee or at the Dealer.
	timeout := wamp.OptionInt64(msg.Options, "timeout")
	if timeout > 0 {
		// Check that callee supports call_timeout.
		if callee.HasFeature("callee", "call_timeout") {
			details["timeout"] = timeout
		}

		// TODO: Start a goroutine to cancel the pending call on timeout.
		// Should be implemented like Cancel with mode=killnowait, and error
		// message argument should say "call timeout"
	}

	// TODO: handle trust levels

	// If the callee has requested disclosure of caller identity when the
	// registration was created, and this was allowed by the dealer.
	if reg.disclose {
		details["caller"] = caller.ID
	} else {
		// A Caller MAY request the disclosure of its identity (its WAMP
		// session ID) to endpoints of a routed call.  This is indicated by the
		// "disclose_me" flag in the message options.
		if wamp.OptionFlag(msg.Options, "disclose_me") {
			// Dealer MAY deny a Caller's request to disclose its identity.
			if !d.allowDisclose {
				caller.Send(&wamp.Error{
					Type:    msg.MessageType(),
					Request: msg.Request,
					Details: map[string]interface{}{},
					Error:   wamp.ErrOptionDisallowedDiscloseMe,
				})
			}
			if callee.HasFeature("callee", "caller_identification") {
				details["caller"] = caller.ID
			}
		}
	}

	// A Caller indicates its willingness to receive progressive results by
	// setting CALL.Options.receive_progress|bool := true
	if wamp.OptionFlag(msg.Options, "receive_progress") {
		// If the Callee supports progressive calls, the Dealer will
		// forward the Caller's willingness to receive progressive
		// results by setting.
		if callee.HasFeature("callee", "progressive_call_results") {
			details["receive_progress"] = true
		}
	}

	d.calls[msg.Request] = caller
	invocationID := d.idGen.Next()
	d.invocations[invocationID] = &invocation{
		callID: msg.Request,
		callee: callee,
	}
	d.invocationByCall[msg.Request] = invocationID

	// Send INVOCATION to the endpoint that has registered the requested
	// procedure.
	err := callee.Send(&wamp.Invocation{
		Request:      invocationID,
		Registration: reg.id,
		Details:      details,
		Arguments:    msg.Arguments,
		ArgumentsKw:  msg.ArgumentsKw,
	})
	if err != nil {
		d.error(&wamp.Error{
			Type:      wamp.INVOCATION,
			Request:   invocationID,
			Details:   map[string]interface{}{},
			Error:     wamp.ErrNetworkFailure,
			Arguments: []interface{}{"client blocked - cannot call procedure"},
		})
	}
}

func (d *dealer) cancel(caller *Session, msg *wamp.Cancel) {
	procCaller, ok := d.calls[msg.Request]
	if !ok {
		// There is no pending call to cancel.
		return
	}

	// Check if the caller of cancel is also the caller of the procedure.
	if caller != procCaller {
		// The caller it trying to cancel calls that it does not own.  It it
		// either confused or trying to do something bad.
		log.Println("CANCEL received from caller", caller,
			"for call owned by different session")
		return
	}

	// Find the pending invocation.
	invocationID, ok := d.invocationByCall[msg.Request]
	if !ok {
		// If there is no pending invocation, ignore cancel.
		log.Print("Found call with no pending invocation")
		return
	}
	invk, ok := d.invocations[invocationID]
	if !ok {
		log.Print("CRITICAL: missing caller for pending invocation")
		return
	}
	// For those who repeatedly press elevator buttons.
	if invk.canceled {
		return
	}
	invk.canceled = true

	// Cancel mode should be one of: "skip", "kill", "killnowait"
	mode := wamp.OptionString(msg.Options, "mode")
	if mode == "killnowait" || mode == "kill" {
		// Check that callee supports call canceling to see if it is alright to
		// send INTERRUPT to callee.
		if !invk.callee.HasFeature("callee", "call_canceling") {
			// Cancel in dealer without sending INTERRUPT to callee.
			log.Println("Callee", invk.callee, "does not support call canceling")
		} else {
			// Send INTERRUPT message to callee.
			err := invk.callee.Send(&wamp.Interrupt{
				Request: invocationID,
				Options: map[string]interface{}{},
			})
			if err == nil {
				log.Println("Dealer sent INTERRUPT to to cancel invocation",
					invocationID, "for call", msg.Request)

				// If mode is "kill" then let error from callee trigger the
				// response to the caller.  This is how the caller waits for
				// the callee to cancel the call.
				if mode == "kill" {
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
	delete(d.calls, msg.Request)
	delete(d.invocationByCall, msg.Request)

	// Send error to the caller.
	caller.Send(&wamp.Error{
		Type:    wamp.CALL,
		Request: msg.Request,
		Error:   wamp.ErrCanceled,
		Details: map[string]interface{}{},
	})
}

func (d *dealer) yield(callee *Session, msg *wamp.Yield) {
	// Find and delete pending invocation.
	invk, ok := d.invocations[msg.Request]
	if !ok {
		// WAMP does not allow sending error in response to YIELD message.
		log.Println("YIELD received with unknown invocation request ID:",
			msg.Request)
		return
	}
	callID := invk.callID
	progress := wamp.OptionFlag(msg.Options, "receive_progress")
	if !progress {
		delete(d.invocations, msg.Request)

		// Delete callID -> invocation.
		delete(d.invocationByCall, callID)
	}

	// Find and delete pending call.
	caller, ok := d.calls[callID]
	if !ok {
		// Found invocation id that does not have any call id.
		log.Println("!!! No matching caller for invocation from YIELD:",
			msg.Request)
		return
	}
	details := map[string]interface{}{}
	if !progress {
		delete(d.calls, callID)
		details["progress"] = true
	}

	// Send RESULT to the caller.  This forwards the YIELD from the callee.
	caller.Send(&wamp.Result{
		Request:     callID,
		Details:     details,
		Arguments:   msg.Arguments,
		ArgumentsKw: msg.ArgumentsKw,
	})
}

func (d *dealer) error(msg *wamp.Error) {
	// Find and delete pending invocation.
	invk, ok := d.invocations[msg.Request]
	if !ok {
		log.Println("Received ERROR (INVOCATION) with invalid request ID:",
			msg.Request)
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
		log.Println("Received ERROR for call that was already canceled:",
			callID)
		return
	}
	delete(d.calls, callID)

	// Send error to the caller.
	caller.Send(&wamp.Error{
		Type:        wamp.CALL,
		Request:     callID,
		Error:       msg.Error,
		Details:     msg.Details,
		Arguments:   msg.Arguments,
		ArgumentsKw: msg.ArgumentsKw,
	})
}

func (d *dealer) removeSession(callee *Session) {
	for regID := range d.calleeRegIDSet[callee] {
		delReg, err := d.delCalleeReg(callee, regID)
		if err != nil {
			// Should probably panic here.
			log.Print("!!! Callee had ID of nonexistent registration")
			continue
		}

		// Publish wamp.registration.on_unregister meta event.  Fired when a
		// callee session is removed from a subscription.
		d.metaClient.Send(&wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnUnregister,
			Arguments: []interface{}{callee.ID, regID},
		})

		if !delReg {
			continue
		}
		// Publish wamp.registration.on_delete meta event.  Fired when a
		// registration is deleted after the last session attached to it
		// has been removed.  The wamp.registration.on_delete event MUST be
		// preceded by a wamp.registration.on_unregister event.
		d.metaClient.Send(&wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnDelete,
			Arguments: []interface{}{callee.ID, regID},
		})
	}
	delete(d.calleeRegIDSet, callee)
}

// delCalleeReg deletes the the callee from the specified registration and
// deletes the registration from the set of registrations for the callee.
//
// If there are no more callees for the registration, then the registration is
// removed and true is returned to indicate that the last registration was
// deleted.
func (d *dealer) delCalleeReg(callee *Session, regID wamp.ID) (bool, error) {
	reg, ok := d.registrations[regID]
	if !ok {
		// The registration doesn't exist
		return false, fmt.Errorf("no such registration: %v", regID)
	}

	// Remove the callee from the registration.
	for i := range reg.callees {
		if reg.callees[i] == callee {
			log.Printf("Unregistered procedure %v (regID=%v) (callee=%v)",
				reg.procedure, regID, callee.ID)
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
		case "prefix":
			delete(d.pfxProcRegMap, reg.procedure)
		case "wildcard":
			delete(d.wcProcRegMap, reg.procedure)
		}
		log.Printf("Deleted registration %v for procedure %v", regID,
			reg.procedure)
		return true, nil
	}
	return false, nil
}

func (d *dealer) addCalleeRegistration(callee *Session, reg wamp.ID) {
}

// ----- Meta Procedure Handlers -----

// RegList retrieves registration IDs listed according to match policies.
func (d *dealer) RegList(msg *wamp.Invocation) wamp.Message {
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
		sync <- struct{}{}
	}
	<-sync
	dict := map[string]interface{}{
		"exact":    exactRegs,
		"prefix":   pfxRegs,
		"wildcard": wcRegs,
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: []interface{}{dict},
	}
}

// RegLookup retrieves registration IDs listed according to match policies.
func (d *dealer) RegLookup(msg *wamp.Invocation) wamp.Message {
	var regID wamp.ID
	if len(msg.Arguments) != 0 {
		if procedure, ok := wamp.AsURI(msg.Arguments[0]); ok {
			var match string
			if len(msg.Arguments) > 1 {
				opts := msg.Arguments[1].(map[string]interface{})
				match = wamp.OptionString(opts, "match")
			}
			sync := make(chan wamp.ID)
			d.actionChan <- func() {
				var r wamp.ID
				var reg *registration
				var ok bool
				switch match {
				default:
					reg, ok = d.procRegMap[procedure]
				case "prefix":
					reg, ok = d.pfxProcRegMap[procedure]
				case "wildcard":
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
		Arguments: []interface{}{regID},
	}
}

// RegMatch obtains the registration best matching a given procedure URI.
func (d *dealer) RegMatch(msg *wamp.Invocation) wamp.Message {
	var regID wamp.ID
	if len(msg.Arguments) != 0 {
		if procedure, ok := wamp.AsURI(msg.Arguments[0]); ok {
			sync := make(chan wamp.ID)
			d.actionChan <- func() {
				var r wamp.ID
				if reg, ok := d.matchProcedure(procedure); ok {
					r = reg.id
				}
				sync <- r
			}
			regID = <-sync
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: []interface{}{regID},
	}
}

// RegGet retrieves information on a particular registration.
func (d *dealer) RegGet(msg *wamp.Invocation) wamp.Message {
	var dict map[string]interface{}
	if len(msg.Arguments) != 0 {
		if i64, ok := wamp.AsInt64(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			regID := wamp.ID(i64)
			d.actionChan <- func() {
				if reg, ok := d.registrations[regID]; ok {
					dict = map[string]interface{}{
						"id":      regID,
						"created": reg.created,
						"uri":     reg.procedure,
						"match":   reg.match,
						"invoke":  reg.policy,
					}
				}
				sync <- struct{}{}
			}
			<-sync
		}
	}
	if dict == nil {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: map[string]interface{}{},
			Error:   wamp.ErrNoSuchRegistration,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: []interface{}{dict},
	}
}

// RegListCallees retrieves a list of session IDs for sessions currently
// attached to the registration.
func (d *dealer) RegListCallees(msg *wamp.Invocation) wamp.Message {
	var calleeIDs []wamp.ID
	if len(msg.Arguments) != 0 {
		if i64, ok := wamp.AsInt64(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			regID := wamp.ID(i64)
			d.actionChan <- func() {
				if reg, ok := d.registrations[regID]; ok {
					calleeIDs = make([]wamp.ID, len(reg.callees))
					for i := range reg.callees {
						calleeIDs[i] = reg.callees[i].ID
					}
				}
				sync <- struct{}{}
			}
			<-sync
		}
	}
	if calleeIDs == nil {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: map[string]interface{}{},
			Error:   wamp.ErrNoSuchRegistration,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: []interface{}{calleeIDs},
	}
}

// regCountCallees obtains the number of sessions currently attached to the
// registration.
func (d *dealer) RegCountCallees(msg *wamp.Invocation) wamp.Message {
	var count int
	var ok bool
	if len(msg.Arguments) != 0 {
		var i64 int64
		if i64, ok = wamp.AsInt64(msg.Arguments[0]); ok {
			sync := make(chan int)
			regID := wamp.ID(i64)
			d.actionChan <- func() {
				if reg, ok := d.registrations[regID]; ok {
					sync <- len(reg.callees)
				} else {
					sync <- -1
				}
			}
			count = <-sync
			if count == -1 {
				ok = false
			}
		}
	}
	if !ok {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: map[string]interface{}{},
			Error:   wamp.ErrNoSuchRegistration,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: []interface{}{count},
	}
}
