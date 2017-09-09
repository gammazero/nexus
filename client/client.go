/*
Package client provides a WAMP client implementation that is interoperable with
any standard WAMP router and is capable of using all of the advanced profile
features supported by the nexus WAMP router.

*/
package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/wamp"
)

const (
	pubAcknowledge = "acknowledge"

	optError   = "error"
	optMode    = "mode"
	optTimeout = "timeout"

	helloAuthmethods = "authmethods"
	helloRoles       = "roles"

	cancelModeKill       = "kill"
	cancelModeKillNoWait = "killnowait"
	cancelModeSkip       = "skip"
)

// ClientConfig configures a client with everything needed to begin a session
// with a WAMP router.
type ClientConfig struct {
	// Realm is the URI of the realm the client will join.
	Realm string

	// HelloDetails contains details about the client.  The client provides the
	// roles, unless already supplied by the user.
	HelloDetails wamp.Dict

	// AuthHandlers is a map of authmethod to AuthFunc.  All authmethod keys
	// from this map are automatically added to HelloDetails["authmethods"]
	AuthHandlers map[string]AuthFunc

	// ResponseTimeout specifies the amount of time that the client will block
	// waiting for a response from the router.  A value of 0 uses the default.
	ResponseTimeout time.Duration

	// Enable debug logging for client.
	Debug bool
}

// Features supported by nexus client.
var clientRoles = wamp.Dict{
	"publisher": wamp.Dict{
		"features": wamp.Dict{
			"subscriber_blackwhite_listing": true,
			"publisher_exclusion":           true,
		},
	},
	"subscriber": wamp.Dict{
		"features": wamp.Dict{
			"pattern_based_subscription": true,
			"publisher_identification":   true,
		},
	},
	"callee": wamp.Dict{
		"features": wamp.Dict{
			"pattern_based_registration": true,
			"shared_registration":        true,
			"call_canceling":             true,
			"call_timeout":               true,
			"caller_identification":      true,
			"progressive_call_results":   true,
		},
	},
	"caller": wamp.Dict{
		"features": wamp.Dict{
			"call_canceling":        true,
			"call_timeout":          true,
			"caller_identification": true,
		},
	},
}

// InvokeResult represents the result of invoking a procedure.
type InvokeResult struct {
	Args   wamp.List
	Kwargs wamp.Dict
	Err    wamp.URI
}

// A Client routes messages to/from a WAMP router.
type Client struct {
	peer wamp.Peer

	responseTimeout time.Duration
	awaitingReply   map[wamp.ID]chan wamp.Message

	authHandlers map[string]AuthFunc

	eventHandlers map[wamp.ID]EventHandler
	topicSubID    map[string]wamp.ID

	invHandlers    map[wamp.ID]InvocationHandler
	nameProcID     map[string]wamp.ID
	invHandlerKill map[wamp.ID]context.CancelFunc

	actionChan chan func()
	idGen      *wamp.IDGen

	stopping          chan struct{}
	activeInvHandlers sync.WaitGroup

	id           wamp.ID
	realmDetails wamp.Dict

	log   stdlog.StdLog
	debug bool

	closed int32
	done   chan struct{}
}

// NewClient takes a connected Peer, joins the realm specified in cfg, and if
// successful, returns a new client.
//
// Each client can be given a separate StdLog instance, which my be desirable
// when clients are used for different purposes.
//
// NOTE: This method is provided for clients that use a Peer implementation not
// provided with the nexus package.  Generally, clients are created using the
// NewLocalPeer() or NewWebsocketPeer() functions.
func NewClient(p wamp.Peer, cfg ClientConfig, logger stdlog.StdLog) (*Client, error) {
	if cfg.ResponseTimeout == 0 {
		cfg.ResponseTimeout = defaultResponseTimeout
	}

	welcome, err := joinRealm(p, cfg)
	if err != nil {
		p.Close()
		return nil, err
	}

	c := &Client{
		peer: p,

		responseTimeout: cfg.ResponseTimeout,
		awaitingReply:   map[wamp.ID]chan wamp.Message{},

		eventHandlers: map[wamp.ID]EventHandler{},
		topicSubID:    map[string]wamp.ID{},

		invHandlers:    map[wamp.ID]InvocationHandler{},
		nameProcID:     map[string]wamp.ID{},
		invHandlerKill: map[wamp.ID]context.CancelFunc{},

		actionChan: make(chan func()),
		idGen:      wamp.NewIDGen(),
		stopping:   make(chan struct{}),
		done:       make(chan struct{}),

		log:   logger,
		debug: cfg.Debug,

		realmDetails: welcome.Details,
		id:           welcome.ID,
	}
	go c.run()
	go c.receiveFromRouter()
	return c, nil
}

// Done returns a channel that signals when the client is no longer connected
// to a router and has shutdown.
func (c *Client) Done() <-chan struct{} { return c.done }

// ID returns the client's session ID which is assigned after attaching to a
// router and joining a realm.
func (c *Client) ID() wamp.ID { return c.id }

// AuthFunc takes the HELLO details and CHALLENGE extra data and returns the
// signature string and a details map.  If the signature is accepted, the
// details are used to populate the welcome message, as well as the session
// attributes.
//
// In response to a CHALLENGE message, the Client MUST send an AUTHENTICATE
// message.  Therefore, AuthFunc does not return an error.  If an error is
// encountered within AuthFunc, then an empty signature should be returned
// since the client cannot give a valid signature response.
//
// This is used in the AuthHandler map, in a ClientConfig, and is used when
// the client joins a realm.
type AuthFunc func(helloDetails wamp.Dict, challengeExtra wamp.Dict) (signature string, details wamp.Dict)

// RealmDetails returns the realm information received in the WELCOME message.
func (c *Client) RealmDetails() wamp.Dict { return c.realmDetails }

// EventHandler is a function that handles a publish event.
type EventHandler func(args wamp.List, kwargs wamp.Dict, details wamp.Dict)

// Subscribe subscribes the client to the specified topic or topic pattern.
//
// The specified EventHandler is registered to be called every time an event is
// received for the topic.  The subscription can specify an exact event URI to
// match or it can specify a URI pattern to match multiple events for the same
// handler.
//
// Options
//
// To request a pattern-based subscription set:
//   options["match"] = "prefix" or "wildcard"
func (c *Client) Subscribe(topic string, fn EventHandler, options wamp.Dict) error {
	if options == nil {
		options = wamp.Dict{}
	}
	id := c.idGen.Next()
	c.expectReply(id)
	sub := &wamp.Subscribe{
		Request: id,
		Options: options,
		Topic:   wamp.URI(topic),
	}
	c.peer.Send(sub)

	// Wait to receive SUBSCRIBED message.
	msg, err := c.waitForReply(id)
	if err != nil {
		return err
	}
	switch msg := msg.(type) {
	case *wamp.Subscribed:
		// Register the event handler for this subscription.
		sync := make(chan struct{})
		c.actionChan <- func() {
			c.eventHandlers[msg.Subscription] = fn
			c.topicSubID[topic] = msg.Subscription
			close(sync)
		}
		<-sync
	case *wamp.Error:
		return fmt.Errorf("error subscribing to topic '%v': %v", topic,
			msg.Error)
	default:
		return unexpectedMsgError(msg, wamp.SUBSCRIBED)
	}
	return nil
}

// SubscriptionID returns the subscription ID for the specified topic.  If the
// client does not have an active subscription to the topic, then returns false
// for second boolean return value.
func (c *Client) SubscriptionID(topic string) (subID wamp.ID, ok bool) {
	sync := make(chan struct{})
	c.actionChan <- func() {
		subID, ok = c.topicSubID[topic]
		close(sync)
	}
	<-sync
	return
}

// Unsubscribe removes the registered EventHandler from the topic.
func (c *Client) Unsubscribe(topic string) error {
	sync := make(chan struct{})
	var subID wamp.ID
	var err error
	c.actionChan <- func() {
		var ok bool
		subID, ok = c.topicSubID[topic]
		if !ok {
			err = errors.New("not subscribed to: " + topic)
		} else {
			// Delete the subscription anyway, regardless of whether or not the
			// the router succeeds or fails to unsubscribe.  If the client
			// called Unsubscribe() then it has no interest in receiving any
			// more events for the topic, and may expect any.
			delete(c.topicSubID, topic)
			delete(c.eventHandlers, subID)
		}
		close(sync)
	}
	<-sync
	if err != nil {
		return err
	}

	id := c.idGen.Next()
	c.expectReply(id)
	sub := &wamp.Unsubscribe{
		Request:      id,
		Subscription: subID,
	}
	c.peer.Send(sub)

	// Wait to receive UNSUBSCRIBED message.
	msg, err := c.waitForReply(id)
	if err != nil {
		return err
	}
	switch msg := msg.(type) {
	case *wamp.Unsubscribed:
		// Already deleted the event handler for the topic.
		return nil
	case *wamp.Error:
		return fmt.Errorf("Error unsubscribing to '%s': %v", topic, msg.Error)
	}
	return unexpectedMsgError(msg, wamp.UNSUBSCRIBED)
}

// Publish publishes an EVENT to all subscribed clients.
//
// Options
//
// To receive a PUBLISHED response set:
//   options["acknowledge"] = true
//
// To request subscriber blacklisting by subscriber, authid, or authrole, set:
//   options["exclude"] = [subscriberID, ...]
//   options["exclude_authid"] = ["authid", ..]
//   options["exclude_authrole"] = ["authrole", ..]
//
// To request subscriber whitelisting by subscriber, authid, or authrole, set:
//   options["eligible"] = [subscriberID, ...]
//   options["eligible_authid"] = ["authid", ..]
//   options["eligible_authrole"] = ["authrole", ..]
//
// When connecting to a nexus router, blacklisting and whitelisting can be used
// with any attribute assigned to the subscriber session, by setting:
//   options["exclude_xxx"] = [val1, val2, ..]
// and
//   options["eligible_xxx"] = [val1, val2, ..]
// where xxx is the name of any session attribute, typically supplied with the
// HELLO message.
//
// To request that publisher's identity is disclosed to subscribers, set:
//   options["disclose_me"] = true
func (c *Client) Publish(topic string, options wamp.Dict, args wamp.List, kwargs wamp.Dict) error {
	if options == nil {
		options = make(wamp.Dict)
	}

	// Check if the client is asking for a PUBLISHED response.
	pubAck, _ := options[pubAcknowledge].(bool)

	id := c.idGen.Next()
	if pubAck {
		c.expectReply(id)
	}
	c.peer.Send(&wamp.Publish{
		Request:     id,
		Options:     options,
		Topic:       wamp.URI(topic),
		Arguments:   args,
		ArgumentsKw: kwargs,
	})

	if !pubAck {
		return nil
	}

	// Wait to receive PUBLISHED message.
	msg, err := c.waitForReply(id)
	if err != nil {
		return err
	}
	switch msg := msg.(type) {
	case *wamp.Published:
	case *wamp.Error:
		return fmt.Errorf("error waiting for published message: %v", msg.Error)
	default:
		return unexpectedMsgError(msg, wamp.UNREGISTERED)
	}
	return nil
}

// InvocationHandler handles a remote procedure call.
//
// The Context is used to signal that the router issues an INTERRUPT request to
// cancel the call-in-progress.  The client application can use this to
// abandon what it is doing, if it chooses to pay attention to ctx.Done().
type InvocationHandler func(context.Context, wamp.List, wamp.Dict, wamp.Dict) (result *InvokeResult)

// Register registers the client to handle invocations of the specified
// procedure.  The InvocationHandler is set to be called for each procedure
// call received.
//
// Options
//
// To request a pattern-based registration set:
//   options["match"] = "prefix" or "wildcard"
//
// To request a shared registration pattern set:
//   options["invoke"] = "single", "roundrobin", "random", "first", "last"
//
// To request that caller identification is disclosed to callees:
//   options["disclose_caller"] = true
func (c *Client) Register(procedure string, fn InvocationHandler, options wamp.Dict) error {
	id := c.idGen.Next()
	c.expectReply(id)
	register := &wamp.Register{
		Request:   id,
		Options:   options,
		Procedure: wamp.URI(procedure),
	}
	c.peer.Send(register)

	// Wait to receive REGISTERED message.
	msg, err := c.waitForReply(id)
	if err != nil {
		return err
	}
	switch msg := msg.(type) {
	case *wamp.Registered:
		// Register the event handler for this registration.
		sync := make(chan struct{})
		c.actionChan <- func() {
			c.invHandlers[msg.Registration] = fn
			c.nameProcID[procedure] = msg.Registration
			close(sync)
		}
		<-sync
		if c.debug {
			c.log.Println("Registered", procedure, "as registration",
				msg.Registration)
		}
	case *wamp.Error:
		return fmt.Errorf("Error registering procedure '%v': %v", procedure,
			msg.Error)
	default:
		return unexpectedMsgError(msg, wamp.REGISTERED)
	}
	return nil
}

// RegistrationID returns the registration ID for the specified procedure.  If
// the client is not registered for the procedure, then returns false for
// second boolean return value.
func (c *Client) RegistrationID(procedure string) (regID wamp.ID, ok bool) {
	sync := make(chan struct{})
	c.actionChan <- func() {
		regID, ok = c.nameProcID[procedure]
		close(sync)
	}
	<-sync
	return
}

// Unregister removes the registration of a procedure from the router.
func (c *Client) Unregister(procedure string) error {
	sync := make(chan struct{})
	var procID wamp.ID
	var err error
	c.actionChan <- func() {
		var ok bool
		procID, ok = c.nameProcID[procedure]
		if !ok {
			err = errors.New("not registered to handle procedure " + procedure)
		} else {
			// Delete the registration anyway, regardless of whether or not the
			// the router succeeds or fails to unregister.  If the client
			// called Unregister() then it has no interest in receiving any
			// more invocations for the procedure, and may not expect any.
			delete(c.nameProcID, procedure)
			delete(c.invHandlers, procID)
		}
		close(sync)
	}
	<-sync
	if err != nil {
		return err
	}

	id := c.idGen.Next()
	c.expectReply(id)
	unregister := &wamp.Unregister{
		Request:      id,
		Registration: procID,
	}
	c.peer.Send(unregister)

	// Wait to receive UNREGISTERED message.
	msg, err := c.waitForReply(id)
	if err != nil {
		return err
	}
	switch msg := msg.(type) {
	case *wamp.Unregistered:
		// Already deleted the invocation handler for the procedure.
	case *wamp.Error:
		return fmt.Errorf("error unregistering procedure '%s': %v", procedure,
			msg.Error)
	default:
		return unexpectedMsgError(msg, wamp.UNREGISTERED)
	}
	return nil
}

// Call calls the procedure corresponding to the given URI.
//
// If an ERROR message is received from the router, the error value returned
// can be type asserted to RPCError to provide access to the returned ERROR
// message.  This may be necessary for the client application to process error
// data from the RPC invocation.
//
// Call Canceling
//
// The provided Context can be used to cancel a call, or to set a deadline that
// cancels the call when the deadline expires.  There is no separate Cancel()
// API to do this.  If the call is canceled before a result is received, then a
// CANCEL message is sent to the router to cancel the call according to the
// specified mode.
//
// If cancelMode must be one of the following:
//     "kill", "killnowait', "skip".
// Setting  to  ""  specifies  using  the  default  value:  "killnowait".   The
// cancelMode is  an option  for a  CANCEL message, not  for the  CALL message,
// which is why it is specified as a parameter to the Call() API.
//
// Cancellation behaves differently depending on the mode:
//
// "skip": The pending call is canceled and ERROR is sent immediately back to
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
//
// Call Timeout
//
// The nexus router also supports call timeout.  If a timeout is provided in
// the options, and the callee supports call timeout, then the timeout value is
// passed to the callee so that the invocation can be canceled by the callee
// if the timeout is reached before a response is returned.  This is the
// behavior implemented by the nexus client in the callee role.
//
// To request a remote call timeout, specify a timeout in milliseconds:
//   options["timeout"] = 30000
//
// Progressive Call Results
//
// A caller can indicate its willingness to receive progressive results by
// setting the "receive_progress" option.  If the callee supports progressive
// calls, the dealer will forward this indication from the caller to the
// callee.
//
// To indicate willingness to receive progressive results:
//   options["receive_progress"] = true
//
// Caller Identification
//
// A caller may request the disclosure of its identity (its WAMP session ID) to
// callees, if allowed by the dealer.
//
// To request that this caller's identity by disclosed:
//   options["disclose_me"] = true
func (c *Client) Call(ctx context.Context, procedure string, options wamp.Dict, args wamp.List, kwargs wamp.Dict, cancelMode string) (*wamp.Result, error) {
	switch cancelMode {
	case cancelModeKill, cancelModeKillNoWait, cancelModeSkip:
	case "":
		cancelMode = cancelModeKillNoWait
	default:
		return nil, fmt.Errorf("cancel mode not one of: '%s', '%s', '%s'",
			cancelModeKill, cancelModeKillNoWait, cancelModeSkip)
	}

	id := c.idGen.Next()
	c.expectReply(id)
	call := &wamp.Call{
		Request:     id,
		Procedure:   wamp.URI(procedure),
		Options:     options,
		Arguments:   args,
		ArgumentsKw: kwargs,
	}
	c.peer.Send(call)

	// Wait to receive RESULT message.
	var msg wamp.Message
	var err error
	msg, err = c.waitForReplyWithCancel(ctx, id, cancelMode, procedure)
	if err != nil {
		return nil, err
	}
	switch msg := msg.(type) {
	case *wamp.Result:
		return msg, nil
	case *wamp.Error:
		return nil, RPCError{msg, procedure}
	default:
		return nil, unexpectedMsgError(msg, wamp.RESULT)
	}
}

// RPCError is a wrapper for a WAMP ERROR message that is received as a result
// of a CALL.  This allows the client application to type assert the error to a
// RPCError and inspect the the ERROR message contents, as may be necessary to
// process an error response from the callee.
type RPCError struct {
	Err       *wamp.Error
	Procedure string
}

// Error implements the error interface, returning an error string for the
// RPCError.
func (werr RPCError) Error() string {
	e := fmt.Sprintf("error calling remote procedure '%s': %v", werr.Procedure,
		werr.Err.Error)
	if len(werr.Err.Arguments) != 0 {
		e += fmt.Sprintf(": %v", werr.Err.Arguments)
	}
	if len(werr.Err.ArgumentsKw) != 0 {
		e += fmt.Sprintf(": %v", werr.Err.ArgumentsKw)
	}
	return e
}

// Close causes the client to leave the realm it has joined, and closes the
// connection to the router.
func (c *Client) Close() error {
	if !atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		return nil
	}

	// Cancel any running invocation handlers and wait for them to finish.
	// This makes it safe to close the actionChan.  Do this before leaving the
	// realm so that any invocation handlers do not hang waiting to send to the
	// router after it has stopped receiving from the client.
	close(c.stopping)
	c.activeInvHandlers.Wait()

	c.leaveRealm()

	// Stop the client's main goroutine.
	close(c.actionChan)

	c.realmDetails = nil
	return nil
}

// joinRealm joins a WAMP realm, handling challenge/response authentication if
// needed.  The authHandlers argument supplies a map of WAMP authmethod names
// to functions that handle each auth type.  This can be nil if router is
// expected to allow anonymous authentication.
func joinRealm(peer wamp.Peer, cfg ClientConfig) (*wamp.Welcome, error) {
	if cfg.Realm == "" {
		return nil, errors.New("realm not specified")
	}
	details := cfg.HelloDetails
	if details == nil {
		details = wamp.Dict{}
	}
	if _, ok := details[helloRoles]; !ok {
		details[helloRoles] = clientRoles
	}
	if len(cfg.AuthHandlers) > 0 {
		authmethods := make(wamp.List, len(cfg.AuthHandlers))
		var i int
		for am := range cfg.AuthHandlers {
			authmethods[i] = am
			i++
		}
		details[helloAuthmethods] = authmethods
	}

	peer.Send(&wamp.Hello{Realm: wamp.URI(cfg.Realm), Details: details})
	msg, err := wamp.RecvTimeout(peer, cfg.ResponseTimeout)
	if err != nil {
		return nil, err
	}

	// Only expect CHALLENGE if client offered authmethod(s).
	if len(cfg.AuthHandlers) > 0 {
		// See if router sent CHALLENGE in response to client HELLO.
		if challenge, ok := msg.(*wamp.Challenge); ok {
			msg, err = handleCRAuth(peer, challenge, details, cfg.AuthHandlers,
				cfg.ResponseTimeout)
			if err != nil {
				return nil, err
			}
		}
		// Do not error if the message is not a CHALLENGE, as the auth methods
		// may have allowed the router to authenticate without CR auth.
	}

	welcome, ok := msg.(*wamp.Welcome)
	if !ok {
		// Received unexpected message from router.
		return nil, unexpectedMsgError(msg, wamp.WELCOME)
	}
	return welcome, nil
}

// leaveRealm leaves the current realm without closing the connection to the
// router.
func (c *Client) leaveRealm() {
	// Send GOODBYE to router.  The router will respond with a GOODBYE message
	// which is handled by receiveFromRouter, and causes it to exit.
	c.peer.Send(&wamp.Goodbye{
		Details: wamp.Dict{},
		Reason:  wamp.ErrCloseRealm,
	})

	// Close the peer.  This causes receiveFromRouter to exit if it has not
	// already done so after receiving GOODBYE from router.
	c.peer.Close()

	// Wait for receiveFromRouter to exit.
	<-c.done
	// Elvis has left the building!
}

func (c *Client) run() {
	for action := range c.actionChan {
		action()
	}
}

func handleCRAuth(peer wamp.Peer, challenge *wamp.Challenge, details wamp.Dict, authHandlers map[string]AuthFunc, rspTimeout time.Duration) (wamp.Message, error) {
	// Look up the authentication function for the specified authmethod.
	authFunc, ok := authHandlers[challenge.AuthMethod]
	if !ok {
		// The router send a challenge for an auth method the client does not
		// know how to deal with.  In response to a CHALLENGE message, the
		// Client MUST send an AUTHENTICATE message.  So, send empty
		// AUTHENTICATE since client does not know what to put in it.
		peer.Send(&wamp.Authenticate{})
	} else {
		// Create signature and send AUTHENTICATE.
		signature, authDetails := authFunc(details, challenge.Extra)
		peer.Send(&wamp.Authenticate{
			Signature: signature,
			Extra:     authDetails,
		})
	}
	msg, err := wamp.RecvTimeout(peer, rspTimeout)
	if err != nil {
		return nil, err
	}
	// If router sent back ABORT in response to client's authentication attempt
	// return error.
	if abort, ok := msg.(*wamp.Abort); ok {
		authErr := wamp.OptionString(abort.Details, optError)
		if authErr == "" {
			authErr = "authentication failed"
		}
		return nil, errors.New(authErr)
	}

	// Return the router's response to AUTHENTICATE, this should be WELCOME.
	return msg, nil
}

// unexpectedMsgError creates an error with information about the unexpected
// message that was received from the router.
func unexpectedMsgError(msg wamp.Message, expected wamp.MessageType) error {
	s := fmt.Sprint("received unexpected ", msg.MessageType(),
		" message when expecting ", expected)

	var details wamp.Dict
	var reason string
	switch m := msg.(type) {
	case *wamp.Abort:
		reason = string(m.Reason)
		details = m.Details
	case *wamp.Goodbye:
		reason = string(m.Reason)
		details = m.Details
	}
	var extra []string
	if reason != "" {
		extra = append(extra, reason)
	}
	if len(details) != 0 {
		var ds []string
		for k, v := range details {
			ds = append(ds, fmt.Sprintf("%s=%v", k, v))
		}
		extra = append(extra, strings.Join(ds, " "))
	}
	if len(extra) != 0 {
		s = fmt.Sprint(s, ": ", strings.Join(extra, " "))
	}
	return errors.New(s)
}

// receiveFromRouter handles messages from the router until client closes.
func (c *Client) receiveFromRouter() {
	defer close(c.done)
	if c.debug {
		defer c.log.Println("Client", c.id, "closed")
	}
	for msg := range c.peer.Recv() {
		if c.debug {
			c.log.Println("Client", c.id, "received", msg.MessageType())
		}
		switch msg := msg.(type) {
		case *wamp.Event:
			c.handleEvent(msg)

		case *wamp.Invocation:
			c.handleInvocation(msg)
		case *wamp.Interrupt:
			c.handleInterrupt(msg)

		case *wamp.Registered:
			c.signalReply(msg, msg.Request)
		case *wamp.Subscribed:
			c.signalReply(msg, msg.Request)
		case *wamp.Unsubscribed:
			c.signalReply(msg, msg.Request)
		case *wamp.Unregistered:
			c.signalReply(msg, msg.Request)
		case *wamp.Result:
			c.signalReply(msg, msg.Request)
		case *wamp.Error:
			c.signalReply(msg, msg.Request)

		case *wamp.Goodbye:
			return

		default:
			c.log.Println("Unhandled message from router:", msg.MessageType(), msg)
		}
	}
}

// handleEvent calls the event handler function a subscriber designated for
// handling EVENT messages.
//
// The eventHandlers are called serially so that they execute in the same order
// as the messages are received in.  This could not be guaranteed if executing
// concurrently.
func (c *Client) handleEvent(msg *wamp.Event) {
	var handler EventHandler
	var ok bool
	sync := make(chan struct{})
	c.actionChan <- func() {
		handler, ok = c.eventHandlers[msg.Subscription]
		close(sync)
	}
	<-sync
	if !ok {
		c.log.Println("No handler registered for subscription:",
			msg.Subscription)
		return
	}
	handler(msg.Arguments, msg.ArgumentsKw, msg.Details)
}

func (c *Client) handleInvocation(msg *wamp.Invocation) {
	c.actionChan <- func() {
		handler, ok := c.invHandlers[msg.Registration]
		if !ok {
			errMsg := fmt.Sprintf("Client has no handler for registration %v",
				msg.Registration)
			// The dealer has a procedure registered to this client, but this
			// client does not recognize the registration ID.  This is not
			// reported as ErrNoSuchProcedure, since the dealer has a procedure
			// registered.  It is reported as ErrInvalidArgument to denote that
			// the client has a problem with the registration ID argument.
			c.peer.Send(&wamp.Error{
				Type:      wamp.INVOCATION,
				Request:   msg.Request,
				Details:   wamp.Dict{},
				Error:     wamp.ErrInvalidArgument,
				Arguments: wamp.List{errMsg},
			})
			c.log.Print(errMsg)
			return
		}

		// Create a kill switch so that invocation can be canceled.
		var cancel context.CancelFunc
		var ctx context.Context
		timeout := wamp.OptionInt64(msg.Details, optTimeout)
		if timeout > 0 {
			// The caller specified a timeout, in milliseconds.
			ctx, cancel = context.WithTimeout(context.Background(),
				time.Millisecond*time.Duration(timeout))
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}
		c.invHandlerKill[msg.Request] = cancel
		c.activeInvHandlers.Add(1)
		go func() {
			defer cancel()
			// Create channel to hold result.  Channel must be buffered.
			// Otherwise, canceling the call will leak the goroutine that is
			// blocked forever waiting to send the result to the channel.
			resChan := make(chan *InvokeResult, 1)
			go func() {
				// The Context is passed into the handler to tell the client
				// application to stop whatever it is doing if it cares to pay
				// attention.
				resChan <- handler(ctx, msg.Arguments, msg.ArgumentsKw,
					msg.Details)
			}()

			// Remove the kill switch when done processing invocation.
			defer func() {
				c.actionChan <- func() {
					delete(c.invHandlerKill, msg.Request)
					c.activeInvHandlers.Done()
				}
			}()

			// Wait for the handler to finish or for the call be to canceled.
			var result *InvokeResult
			select {
			case result = <-resChan:
			case <-c.stopping:
				c.log.Print("Client stopping, invocation handler canceled")
				// Return without sending response to server.  This will also
				// cancel the context.
				return
			case <-ctx.Done():
				// Received an INTERRUPT message from the router.
				// Note: handler is also just as likely to return on INTERRUPT.
				result = &InvokeResult{Err: wamp.ErrCanceled}
				c.log.Println("INVOCATION", msg.Request, "canceled")
			}

			if result.Err != "" {
				// If the cancel is already deleted, this is the signal that
				// kill mode was "killnowait", in which case do not send a
				// response to the router.  Check is done here since a cancel
				// can cause either the handler to return or ctx.Done() to
				// close, indeterminately .
				okChan := make(chan bool)
				c.actionChan <- func() {
					_, ok := c.invHandlerKill[msg.Request]
					okChan <- ok
				}
				ok := <-okChan
				if !ok {
					return
				}

				c.peer.Send(&wamp.Error{
					Type:        wamp.INVOCATION,
					Request:     msg.Request,
					Details:     wamp.Dict{},
					Arguments:   result.Args,
					ArgumentsKw: result.Kwargs,
					Error:       result.Err,
				})
				return
			}
			c.peer.Send(&wamp.Yield{
				Request:     msg.Request,
				Options:     wamp.Dict{},
				Arguments:   result.Args,
				ArgumentsKw: result.Kwargs,
			})
		}()
	}
}

// handleInterrupt processes an INTERRUPT message from the from the router
// requesting that a pending call be canceled.
func (c *Client) handleInterrupt(msg *wamp.Interrupt) {
	c.actionChan <- func() {
		cancel, ok := c.invHandlerKill[msg.Request]
		if !ok {
			c.log.Print("Received INTERRUPT for message that no longer exists")
			return
		}
		// If the interrupt mode is "killnowait", then the router is not
		// waiting for a response, so do not send one.  This is indicated by
		// deleting the cancel for the invocation early.
		if wamp.OptionString(msg.Options, optMode) == cancelModeKillNoWait {
			delete(c.invHandlerKill, msg.Request)
		}
		cancel()
	}
}

func (c *Client) signalReply(msg wamp.Message, requestID wamp.ID) {
	c.actionChan <- func() {
		w, ok := c.awaitingReply[requestID]
		if !ok {
			c.log.Println("Received", msg.MessageType(), requestID,
				"that client is no longer waiting for")
			return
		}
		w <- msg
	}
}

func (c *Client) expectReply(id wamp.ID) {
	wait := make(chan wamp.Message, 1)
	sync := make(chan struct{})
	c.actionChan <- func() {
		c.awaitingReply[id] = wait
		close(sync)
	}
	<-sync
}

func (c *Client) waitForReply(id wamp.ID) (wamp.Message, error) {
	sync := make(chan struct{})
	var wait chan wamp.Message
	var ok bool
	c.actionChan <- func() {
		wait, ok = c.awaitingReply[id]
		close(sync)
	}
	<-sync
	if !ok {
		return nil, fmt.Errorf("not expecting reply for ID: %v", id)
	}

	var msg wamp.Message
	var err error
	select {
	case msg = <-wait:
	case <-time.After(c.responseTimeout):
		err = errors.New("timeout while waiting for reply")
	}
	c.actionChan <- func() {
		delete(c.awaitingReply, id)
	}
	return msg, err
}

func (c *Client) waitForReplyWithCancel(ctx context.Context, id wamp.ID, mode, procedure string) (wamp.Message, error) {
	sync := make(chan struct{})
	var wait chan wamp.Message
	var ok bool
	c.actionChan <- func() {
		wait, ok = c.awaitingReply[id]
		close(sync)
	}
	<-sync
	if !ok {
		return nil, fmt.Errorf("not expecting reply for ID: %v", id)
	}

	// If the context does not have a deadline, then give it the default
	// response timeout.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.responseTimeout)
		defer cancel()
	}

	var msg wamp.Message
	var err error
	select {
	case msg = <-wait:
	case <-ctx.Done():
		c.log.Printf("Call to '%s' canceled (mode=%s)", procedure, mode)
		c.peer.Send(&wamp.Cancel{
			Request: id,
			Options: wamp.SetOption(nil, optMode, mode),
		})
	}
	if msg == nil {
		select {
		case msg = <-wait:
		case <-time.After(c.responseTimeout):
			err = errors.New("timeout while waiting for reply")
		}
	}
	c.actionChan <- func() {
		delete(c.awaitingReply, id)
	}
	return msg, err
}
