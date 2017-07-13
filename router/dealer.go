package router

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/gammazero/nexus/wamp"
)

// TODO: Implement the following:
// - call timeout (need timeout goroutine)
// - call trust levels
// - session_meta_api
// - registration_meta_api

// Features supported by this dealer.
var dealerFeatures = map[string]interface{}{
	"features": map[string]bool{
		"call_canceling":             true,
		"call_timeout":               true,
		"caller_identification":      true,
		"pattern_based_registration": true,
		"progressive_call_results":   true,
		"shared_registration":        true,
	},
}

// Dealers route calls incoming from Callers to Callees implementing the
// procedure called, and route call results back from Callees to Callers.
type Dealer interface {
	// Submit dispatches a Resister, Unregister, Call, Cancel, Yield, or Error
	// message to the dealer.
	Submit(sess *Session, msg wamp.Message)

	// Remove a callee's registrations.
	RemoveSession(*Session)

	// Close shuts down the dealer.
	Close()

	// Features returns the features supported by this dealer.
	//
	// The data returned is suitable for use as the "features" section of the
	// dealer role in a WELCOME message.
	Features() map[string]interface{}
}

// remoteProcedure tracks in-progress remote procedure call
type remoteProcedure struct {
	callee    *Session
	procedure wamp.URI
	match     string
	policy    string
	disclose  bool
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
	// procedure URI -> registration ID list
	// Multiple sessions can register as callees depending on invocation policy
	registrations    map[wamp.URI][]wamp.ID
	pfxRegistrations map[wamp.URI][]wamp.ID
	wcRegistrations  map[wamp.URI][]wamp.ID

	// registration ID -> procedure
	procedures map[wamp.ID]remoteProcedure

	// call ID -> caller session
	calls map[wamp.ID]*Session

	// invocation ID -> {call ID, callee, canceled}
	invocations map[wamp.ID]*invocation

	// call ID -> invocation ID (for cancel)
	invocationByCall map[wamp.ID]wamp.ID

	// callee session -> registration ID set.
	calleeRegIDSet map[*Session]map[wamp.ID]struct{}

	reqChan    chan routerReq
	closedChan chan struct{}
	syncChan   chan struct{}

	// Generate registration IDs.
	idGen *wamp.IDGen

	// Used for round-robin call invocation.
	prng *rand.Rand

	// Dealer behavior flags.
	strictURI     bool
	allowDisclose bool
}

// NewDealer creates a the default Dealer implementation.
func NewDealer(strictURI, allowDisclose bool) Dealer {
	d := &dealer{
		procedures: map[wamp.ID]remoteProcedure{},

		registrations:    map[wamp.URI][]wamp.ID{},
		pfxRegistrations: map[wamp.URI][]wamp.ID{},
		wcRegistrations:  map[wamp.URI][]wamp.ID{},

		calls:            map[wamp.ID]*Session{},
		invocations:      map[wamp.ID]*invocation{},
		invocationByCall: map[wamp.ID]wamp.ID{},
		calleeRegIDSet:   map[*Session]map[wamp.ID]struct{}{},

		// The request handler channel does not need to be more than size one,
		// since the incoming messages will be processed at the same rate
		// whether the messages sit in the recv channel of peers, or they sit
		// in the reqChan.
		reqChan:    make(chan routerReq, 1),
		closedChan: make(chan struct{}),
		syncChan:   make(chan struct{}),

		idGen: wamp.NewIDGen(),
		prng:  rand.New(rand.NewSource(time.Now().Unix())),

		strictURI:     strictURI,
		allowDisclose: allowDisclose,
	}
	go d.reqHandler()
	return d
}

// Submit dispatches a Resister, Unregister, Call, Cancel, Yield, or Error
// message to the dealer.
func (d *dealer) Submit(sess *Session, msg wamp.Message) {
	d.reqChan <- routerReq{session: sess, msg: msg}
}

func (d *dealer) Unregister(callee *Session, msg *wamp.Unregister) {
	d.reqChan <- routerReq{session: callee, msg: msg}
}

func (d *dealer) Call(caller *Session, msg *wamp.Call) {
	d.reqChan <- routerReq{session: caller, msg: msg}
}

func (d *dealer) Cancel(caller *Session, msg *wamp.Cancel) {
	d.reqChan <- routerReq{session: caller, msg: msg}
}

func (d *dealer) Yield(callee *Session, msg *wamp.Yield) {
	d.reqChan <- routerReq{session: callee, msg: msg}
}

func (d *dealer) Error(peer *Session, msg *wamp.Error) {
	d.reqChan <- routerReq{session: peer, msg: msg}
}

func (d *dealer) RemoveSession(peer *Session) {
	d.reqChan <- routerReq{session: peer}
}

// Close stops the dealer and waits message processing to stop.
func (d *dealer) Close() {
	if d.Closed() {
		return
	}
	close(d.reqChan)
	<-d.closedChan
}

// Closed returns true if the broker has been closed.
func (d *dealer) Closed() bool {
	select {
	case <-d.closedChan:
		return true
	default:
	}
	return false
}

// Wait until all previous requests have been processed.
func (d *dealer) sync() {
	d.reqChan <- routerReq{}
	<-d.syncChan
}

// reqHandler is dealer's main processing function that is run by a single
// goroutine.  All functions that access dealer data structures run on this
// routine.
func (d *dealer) reqHandler() {
	defer close(d.closedChan)
	for req := range d.reqChan {
		if req.session == nil {
			d.syncChan <- struct{}{}
			continue
		}
		if req.msg == nil {
			d.removeSession(req.session)
			continue
		}
		switch msg := req.msg.(type) {
		case *wamp.Register:
			d.register(req.session, msg)
		case *wamp.Unregister:
			d.unregister(req.session, msg)
		case *wamp.Call:
			d.call(req.session, msg)
		case *wamp.Cancel:
			d.cancel(req.session, msg)
		case *wamp.Yield:
			d.yield(req.session, msg)
		case *wamp.Error:
			d.error(req.session, msg)
		default:
			panic(fmt.Sprint("dealer received message type: ",
				req.msg.MessageType()))
		}
	}
}

// register registers a callee to handle calls to a procedure.
//
// If the shared_registration feature is supported, and if allowed by the
// invocation policy, multiple callees may register to handle the same
// procedure.
func (d *dealer) register(callee *Session, msg *wamp.Register) {
	// Validate procedure URI.  For REGISTER, must be valid URI (either strict
	// or loose), and all URI components must be non-empty other than for
	// wildcard or prefix matched procedures.
	match := wamp.OptionString(msg.Options, "match")
	if !msg.Procedure.ValidURI(d.strictURI, match) {
		errMsg := fmt.Sprintf(
			"register for invalid procedure URI %s (URI strict checking %s)",
			msg.Procedure, d.strictURI)
		callee.Send(&wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Error:     wamp.ErrInvalidURI,
			Arguments: []interface{}{errMsg},
		})
		return
	}

	// Disallow registration of procedures starting with "wamp.", except for
	// trusted sessions that are built into router.
	if callee.AuthRole != "" && callee.AuthRole != "trusted" {
		if strings.HasPrefix(string(msg.Procedure), "wamp.") {
			errMsg := fmt.Sprintf("register for restricted procedure URI %s",
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

	var regs []wamp.ID

	switch match {
	default:
		regs = d.registrations[msg.Procedure]
	case "prefix":
		regs = d.pfxRegistrations[msg.Procedure]
	case "wildcard":
		regs = d.wcRegistrations[msg.Procedure]
	}

	invokePolicy := wamp.OptionString(msg.Options, "invoke")
	if len(regs) != 0 {
		// Get invocation policy of existing registration.  All regs must have
		// the same policy, so use the first one.
		proc, ok := d.procedures[regs[0]]
		if !ok {
			panic("registration exist without any procedure")
		}
		foundPolicy := proc.policy

		// Found an existing registration has an invocation strategy that only
		// allows a single callee on a the given registration.
		if foundPolicy == "" || foundPolicy == "single" {
			log.Println("REGISTER for already registered procedure:",
				msg.Procedure)
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
		if foundPolicy != invokePolicy {
			log.Println("REGISTER for already registered procedure",
				msg.Procedure, "with conflicting invocation policy (has",
				foundPolicy, "and", invokePolicy, "was requested")
			callee.Send(&wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: map[string]interface{}{},
				Error:   wamp.ErrProcedureAlreadyExists,
			})
			return
		}
	}

	regID := d.idGen.Next()
	d.procedures[regID] = remoteProcedure{
		callee:    callee,
		procedure: msg.Procedure,
		match:     match,
		policy:    invokePolicy,
		disclose:  discloseCaller,
	}
	d.registrations[msg.Procedure] = append(d.registrations[msg.Procedure], regID)
	d.addCalleeRegistration(callee, regID)

	log.Printf("registered procedure %v (regID=%v) to callee %v",
		msg.Procedure, regID, callee)
	callee.Send(&wamp.Registered{
		Request:      msg.Request,
		Registration: regID,
	})
}

func (d *dealer) unregister(callee *Session, msg *wamp.Unregister) {
	proc, ok := d.procedures[msg.Registration]
	if !ok {
		// the registration doesn't exist
		log.Println("error: no such registration:", msg.Registration)
		callee.Send(&wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: map[string]interface{}{},
			Error:   wamp.ErrNoSuchRegistration,
		})
		return
	}

	delete(d.procedures, msg.Registration)

	// Delete the registration according to what match type it is.
	d.delRegistration(msg.Registration, proc.procedure, proc.match)
	d.removeCalleeRegistration(callee, msg.Registration)
	log.Printf("unregistered procedure %v (reg=%v)", proc.procedure,
		msg.Registration)
	callee.Send(&wamp.Unregistered{
		Request: msg.Request,
	})
}

func (d *dealer) call(caller *Session, msg *wamp.Call) {
	// Find registered procedures with exact match.
	regs, ok := d.registrations[msg.Procedure]
	if !ok {
		// No exact match was found.  So, search for a prefix or wildcard
		// match, and prefer the most specific math (longest matched pattern).
		// If there is a tie, then prefer the first longest prefix.
		var matchCount int
		for pfxProc, pfxRegs := range d.pfxRegistrations {
			if msg.Procedure.PrefixMatch(pfxProc) {
				if len(pfxProc) > matchCount {
					regs = pfxRegs
					matchCount = len(pfxProc)
				}
			}
		}
		for wcProc, wcRegs := range d.wcRegistrations {
			if msg.Procedure.WildcardMatch(wcProc) {
				if len(wcProc) > matchCount {
					regs = wcRegs
					matchCount = len(wcProc)
				}
			}
		}
	}

	// If no registered procedure, send error.
	if len(regs) == 0 {
		caller.Send(&wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: map[string]interface{}{},
			Error:   wamp.ErrNoSuchProcedure,
		})
		return
	}

	reg := regs[0]

	// Get the procedure for the found registration.
	proc, ok := d.procedures[reg]

	// If there are multiple callees registered for the procedure, then select
	// a registration based on procedure's invocation policy.
	if len(regs) > 1 && ok {
		// Note: It is OK to use the policy of the first registration because,
		// all other registrations must have the same policy.
		switch proc.policy {
		case "first":
		case "roundrobin":
			// Rotate (in-place) the slice of registrations.
			// Note: This is inefficient for large number of registrations.
			for i := 0; i < len(regs)-1; i++ {
				regs[0] = regs[i+1]
			}
			regs[len(regs)-1] = reg
		case "random":
			i := d.prng.Int63n(int64(len(regs)))
			reg = regs[i]
		case "last":
			reg = regs[len(regs)-1]
		default:
			errMsg := fmt.Sprint("multiple procedures registered for",
				msg.Procedure, "with 'single' policy")
			panic(errMsg)
		}
		// Get the procedure for the selected registration.
		proc, ok = d.procedures[reg]
	}

	if !ok {
		log.Println(
			"critical: found a registration id that has no remote procedure")
		caller.Send(&wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: map[string]interface{}{},
			Error:   wamp.ErrNoSuchProcedure,
		})
		return
	}

	details := map[string]interface{}{}

	// A Caller might want to issue a call providing a timeout for the call to
	// finish.
	//
	// A timeout allows to automatically cancel a call after a specified time
	// either at the Callee or at the Dealer.
	var timeout int32
	if _to, ok := msg.Options["timeout"]; ok && _to != nil {
		timeout = _to.(int32)
	}

	if timeout != 0 {
		// Check that callee supports call_timeout.
		if proc.callee.HasFeature("callee", "call_timeout") {
			details["timeout"] = timeout
		}

		// TODO: Start a goroutine to cancel the pending call on timeout.
		// Should be implemented like Cancel with mode=killnowait, and error
		// message argument should say "call timeout"
	}

	// TODO: handle trust levels

	// If the callee has requested disclosure of caller identity.
	if proc.disclose {
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
			// TODO: Is it really necessary to check that callee supports this
			// feature?  If the callee did not support this, then the info
			// in the message should be ignored, right?
			if proc.callee.HasFeature("callee", "caller_identification") {
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
		//
		// TODO: Check for feature support, or let callee ignore?
		if proc.callee.HasFeature("callee", "progressive_call_results") {
			details["receive_progress"] = true
		}
	}

	d.calls[msg.Request] = caller
	invocationID := d.idGen.Next()
	d.invocations[invocationID] = &invocation{
		callID: msg.Request,
		callee: proc.callee,
	}
	d.invocationByCall[msg.Request] = invocationID

	// Send INVOCATION to the endpoint that has registered the requested
	// procedure.
	proc.callee.Send(&wamp.Invocation{
		Request:      invocationID,
		Registration: reg,
		Details:      details,
		Arguments:    msg.Arguments,
		ArgumentsKw:  msg.ArgumentsKw,
	})
	log.Printf("dispatched CALL: %v [%v] to callee as INVOCATION %v",
		msg.Request, msg.Procedure, invocationID,
	)
}

// cancel actively cancels a call that is in progress.
//
// Cancellation behaves differently depending on the mode:
//
// - "skip": The pending call is canceled and ERROR is send immediately back to
// the caller.  No INTERRUPT is sent to the callee and the result is discarded
// when received.
//
// - "kill": INTERRUPT is sent to the client, but ERROR is not returned to the
// caller until after the callee has responded to the canceled call.  In this
// case the caller may receive RESULT or ERROR depending whether the callee
// finishes processing the invocation or the interrupt first.
//
// - "killnowait": The pending call is canceled and ERROR is send immediately
// back to the caller.  INTERRUPT is sent to the callee and any response to the
// invocation or interrupt from the callee is discarded when received.
//
// If the callee does not support call canceling, then behavior is "skip".
func (d *dealer) cancel(caller *Session, msg *wamp.Cancel) {
	procCaller, ok := d.calls[msg.Request]
	if !ok {
		// There is no pending call to cancel.
		return
	}

	// Check if the caller of cancel is also the caller of the procedure.
	if caller != procCaller {
		log.Println("CANCEL received for call owned by different session")
		return
	}

	// Find the pending invocation.
	invocationID, ok := d.invocationByCall[msg.Request]
	if !ok {
		// If there is no pending invocation, ignore cancel.
		log.Println("found call with no pending invocation")
		return
	}
	invk, ok := d.invocations[invocationID]
	if !ok {
		log.Println("critical: missing caller for pending invocation")
		return
	}
	// For those who repeatedly press elevator buttons.
	if invk.canceled {
		return
	}
	invk.canceled = true

	// Cancel mode should be one of: "skip", "kill", "killnowait"
	mode := wamp.OptionString(msg.Options, "mode")
	if mode == "killnowait" || mode == "skip" {
		// Immediately delete the pending call and send ERROR back to the
		// caller.  This will prevent the possibility of the client receiving
		// either RESULT or ERROR following a cancel.
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

		// Only canceling the call on the caller side.  Let the invocation
		// continue and drop the callee's response to it when received.
		if mode == "skip" {
			return
		}
	}

	// Check that callee supports call canceling.
	if !invk.callee.HasFeature("callee", "call_canceling") {
		log.Println("callee", invk.callee, "does not support call canceling")
		return
	}

	// Send INTERRUPT message to callee.
	invk.callee.Send(&wamp.Interrupt{
		Request: invocationID,
		Options: map[string]interface{}{},
	})
	log.Printf("sent INTERRUPT to to cancel invocation %v for call %v",
		invocationID, msg.Request)
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
		log.Println("CRITICAL: no matching caller for invocation from YIELD:",
			msg.Request)
		return
	}
	details := map[string]interface{}{}
	if !progress {
		delete(d.calls, callID)
		details["progress"] = true
	}

	// Send result to the caller.
	caller.Send(&wamp.Result{
		Request:     callID,
		Details:     details,
		Arguments:   msg.Arguments,
		ArgumentsKw: msg.ArgumentsKw,
	})
	log.Printf("sent RESULT %v to caller from YIELD %v", callID, msg.Request)
}

func (d *dealer) error(peer *Session, msg *wamp.Error) {
	// Find and delete pending invocation.
	invk, ok := d.invocations[msg.Request]
	if !ok {
		log.Println("received ERROR (INVOCATION) with invalid request ID:",
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
		log.Println("received ERROR for call that was already canceled:",
			callID)
		return
	}
	delete(d.calls, callID)

	// Send error to the caller.
	caller.Send(&wamp.Error{
		Type:        wamp.CALL,
		Request:     callID,
		Error:       msg.Error,
		Details:     map[string]interface{}{},
		Arguments:   msg.Arguments,
		ArgumentsKw: msg.ArgumentsKw,
	})
	log.Printf("sent ERROR %v to caller as ERROR %v", msg.Request, callID)
}

func (d *dealer) removeSession(callee *Session) {
	for reg := range d.calleeRegIDSet[callee] {
		if proc, ok := d.procedures[reg]; ok {
			delete(d.procedures, reg)
			d.delRegistration(reg, proc.procedure, proc.match)
		}
		d.removeCalleeRegistration(callee, reg)
	}
}

func (d *dealer) delRegistration(reg wamp.ID, proc wamp.URI, match string) {
	// Delete the registration according to what type it is.
	switch match {
	default:
		regs := d.registrations[proc]
		if len(regs) <= 1 {
			delete(d.registrations, proc)
		} else {
			// If there multiple registrations for the procedure, only remove
			// the one that is being unregistered.
			for i := range regs {
				if regs[i] == reg {
					// Delete; preserve order in case expected for invocation.
					d.registrations[proc] = append(regs[:i], regs[i+1:]...)
					break
				}
			}
		}
	case "prefix":
		regs := d.pfxRegistrations[proc]
		if len(regs) <= 1 {
			delete(d.pfxRegistrations, proc)
		} else {
			for i := range regs {
				if regs[i] == reg {
					d.pfxRegistrations[proc] = append(regs[:i], regs[i+1:]...)
					break
				}
			}
		}
	case "wildcard":
		regs := d.wcRegistrations[proc]
		if len(regs) <= 1 {
			delete(d.wcRegistrations, proc)
		} else {
			for i := range regs {
				if regs[i] == reg {
					d.wcRegistrations[proc] = append(regs[:i], regs[i+1:]...)
					break
				}
			}
		}
	}
}

func (d *dealer) addCalleeRegistration(callee *Session, reg wamp.ID) {
	if _, ok := d.calleeRegIDSet[callee]; !ok {
		d.calleeRegIDSet[callee] = map[wamp.ID]struct{}{}
	}
	d.calleeRegIDSet[callee][reg] = struct{}{}
}

func (d *dealer) removeCalleeRegistration(callee *Session, reg wamp.ID) {
	if _, ok := d.calleeRegIDSet[callee]; !ok {
		return
	}
	delete(d.calleeRegIDSet[callee], reg)
	if len(d.calleeRegIDSet[callee]) == 0 {
		delete(d.calleeRegIDSet, callee)
	}
}
