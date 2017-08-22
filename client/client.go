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
	"time"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/wamp"
)

const (
	pubAcknowledge = "acknowledge"

	optError = "error"
	optMode  = "mode"

	helloAuthmethods = "authmethods"
	helloRoles       = "roles"

	cancelModeKill       = "kill"
	cancelModeKillNoWait = "killnowait"
	cancelModeSkip       = "skip"
)

// Features supported by nexus client.
var clientRoleFeatures = wamp.Dict{
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
	realm        string
	realmDetails wamp.Dict

	log   stdlog.StdLog
	debug bool

	done chan struct{}
}

// NewClient takes a connected Peer and returns a new Client.
//
// A non-zero responseTimeout specifies the amount of time that the client will
// block waiting for a response from the router.  A value of 0 uses the
// default.
//
// Each client can be give a separate StdLog instance, which my be desirable
// when clients are used for different purposes.
//
// JoinRealm must be called before other client functions.
func NewClient(p wamp.Peer, responseTimeout time.Duration, logger stdlog.StdLog) *Client {
	if responseTimeout == 0 {
		responseTimeout = defaultResponseTimeout
	}
	c := &Client{
		peer: p,

		responseTimeout: responseTimeout,
		awaitingReply:   map[wamp.ID]chan wamp.Message{},

		eventHandlers: map[wamp.ID]EventHandler{},
		topicSubID:    map[string]wamp.ID{},

		invHandlers:    map[wamp.ID]InvocationHandler{},
		nameProcID:     map[string]wamp.ID{},
		invHandlerKill: map[wamp.ID]context.CancelFunc{},

		actionChan: make(chan func()),
		idGen:      wamp.NewIDGen(),
		stopping:   make(chan struct{}),

		log: logger,
	}
	go c.run()
	return c
}

// Done returns a channel that signals when the client is no longer connected
// to a router and has shutdown.
func (c *Client) Done() <-chan struct{} { return c.done }

// ID returns the client's session ID which is assigned after attaching to a
// router and joining a realm.
func (c *Client) ID() wamp.ID { return c.id }

// SetDebug enables or disables debug logging.
func (c *Client) SetDebug(enable bool) {
	c.actionChan <- func() {
		c.debug = enable
	}
}

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
// This is used to create the authHandler map passed to JoinRealm.
type AuthFunc func(helloDetails wamp.Dict, challengeExtra wamp.Dict) (signature string, details wamp.Dict)

// JoinRealm joins a WAMP realm, handling challenge/response authentication if
// needed.  The authHandlers argument supplies a map of WAMP authmethod names
// to functions that handle each auth type.  This can be nil if router is
// expected to allow anonymous authentication.
func (c *Client) JoinRealm(realm string, details wamp.Dict, authHandlers map[string]AuthFunc) (wamp.Dict, error) {
	joinChan := make(chan bool)
	c.actionChan <- func() {
		joinChan <- (c.realm == "")
	}
	ok := <-joinChan
	if !ok {
		return nil, errors.New("client is already member of realm " + c.realm)
	}
	if details == nil {
		details = wamp.Dict{}
	}
	details[helloRoles] = clientRoles()
	if len(authHandlers) > 0 {
		authmethods := make(wamp.List, len(authHandlers))
		var i int
		for am := range authHandlers {
			authmethods[i] = am
			i++
		}
		details[helloAuthmethods] = authmethods
	}

	c.peer.Send(&wamp.Hello{Realm: wamp.URI(realm), Details: details})
	msg, err := wamp.RecvTimeout(c.peer, c.responseTimeout)
	if err != nil {
		c.peer.Close()
		close(c.actionChan)
		return nil, err
	}

	// Only expect CHALLENGE if client offered authmethod(s).
	if len(authHandlers) > 0 {
		// See if router sent CHALLENGE in response to client HELLO.
		if challenge, ok := msg.(*wamp.Challenge); ok {
			msg, err = c.handleCRAuth(challenge, details, authHandlers)
			if err != nil {
				c.peer.Close()
				close(c.actionChan)
				return nil, err
			}
		}
		// Do not error if the message is not a CHALLENGE, as the auth methods
		// may have allowed the router to authenticate without CR auth.
	}

	welcome, ok := msg.(*wamp.Welcome)
	if !ok {
		// Received unexpected message from router.
		c.peer.Close()
		close(c.actionChan)
		return nil, unexpectedMsgError(msg, wamp.WELCOME)
	}

	c.actionChan <- func() {
		c.realm = realm
		c.realmDetails = welcome.Details
		c.id = welcome.ID
		c.done = make(chan struct{})
		joinChan <- true
	}
	<-joinChan

	go c.receiveFromRouter()
	return welcome.Details, nil
}

// EventHandler is a function that handles a publish event.
type EventHandler func(args wamp.List, kwargs wamp.Dict, details wamp.Dict)

// Subscribe subscribes the client to the specified topic or topic pattern.
//
// The specified EventHandler is registered to be called every time an event is
// received for the topic.  To request a pattern-based subscription set:
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
			sync <- struct{}{}
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

// SubscriptionID returns the subscription id for the specified topic.  If the
// client does not have an active subscription to the topic, then returns false
// for second boolean return value.
func (c *Client) SubscriptionID(topic string) (subID wamp.ID, ok bool) {
	subID, ok = c.topicSubID[topic]
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
		sync <- struct{}{}
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
// To request a pattern-based registration set:
//   options["match"] = "prefix" or "wildcard"
//
// To request a shared registration pattern set:
//   options["invoke"] = "single", "roundrobin", "random", "first", "last"
//
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
			sync <- struct{}{}
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
		sync <- struct{}{}
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

// RPCError is a wrapper for a WAMP ERROR message that is received as a result
// of a CALL.  This allows the client application to type assert the error to a
// RPCError and inspect the the ERROR message contents, as may be necessary to
// process an error response from the callee.
type RPCError struct {
	Err       *wamp.Error
	Procedure string
}

// Error implements the error interface, returning an error string.
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

// Call calls the procedure corresponding to the given URI.
//
// If an ERROR message is received from the router, the error value returned
// can be type asserted to RPCError to provide access to the returned ERROR
// message.  This may be necessary for the client application to process error
// data from the RPC invocation.
//
// The provided Context can be used to cancel a call, or to set a deadline that
// cancels the call when the deadline expires.  If the call is canceled before
// a result is received, then a CANCEL message is sent to the router to cancel
// the call according to the specified mode.
//
// If cancelMode must be one of the following:
//     "kill", "killnowait', "skip".
// Setting to "" specifies using the default value: "killnowait"
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

// LeaveRealm leaves the current realm without closing the connection to the
// router.
func (c *Client) LeaveRealm() error {
	leaveChan := make(chan bool)
	c.actionChan <- func() {
		if c.realm == "" {
			leaveChan <- false
			return
		}
		c.realm = ""
		c.realmDetails = nil
		leaveChan <- true
	}
	ok := <-leaveChan
	if !ok {
		return errors.New("client has not joined a realm")
	}

	// Send GOODBYE to router.  The router will respond with a GOODBYE message
	// which is handled by receiveFromRouter, and causes it to exit.
	c.peer.Send(&wamp.Goodbye{
		Details: wamp.Dict{},
		Reason:  wamp.ErrCloseRealm,
	})
	return nil
}

// Close closes the connection to the router.
func (c *Client) Close() error {
	if err := c.LeaveRealm(); err != nil {
		return err
	}
	// Cancel any invocation handlers that are still running, without them
	// returning results to the router, since router has already said goodbye.
	close(c.stopping)

	// Wait for message handler to exit.
	<-c.done

	// Wait for any active invocation handlers to finish, so that is is safe to
	// close the actionChan which stops the client.
	c.activeInvHandlers.Wait()
	close(c.actionChan)
	c.peer.Close()
	return nil
}

func (c *Client) run() {
	for action := range c.actionChan {
		action()
	}
}

func (c *Client) handleCRAuth(challenge *wamp.Challenge, details wamp.Dict, authHandlers map[string]AuthFunc) (wamp.Message, error) {
	// Look up the authentication function for the specified authmethod.
	authFunc, ok := authHandlers[challenge.AuthMethod]
	if !ok {
		// The router send a challenge for an auth method the client does not
		// know how to deal with.  In response to a CHALLENGE message, the
		// Client MUST send an AUTHENTICATE message.  So, send empty
		// AUTHENTICATE since client does not know what to put in it.
		c.peer.Send(&wamp.Authenticate{})
	} else {
		// Create signature and send AUTHENTICATE.
		signature, authDetails := authFunc(details, challenge.Extra)
		c.peer.Send(&wamp.Authenticate{
			Signature: signature,
			Extra:     authDetails,
		})
	}
	msg, err := wamp.RecvTimeout(c.peer, c.responseTimeout)
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

// clientRoles advertises support for these client roles.
func clientRoles() wamp.Dict {
	return clientRoleFeatures
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
	//for msg := range c.peer.Recv() {
RecvLoop:
	for {
		var msg wamp.Message
		var open bool
		select {
		case msg, open = <-c.peer.Recv():
			if !open {
				break RecvLoop
			}
		case <-c.stopping:
			break RecvLoop
		}

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
			break RecvLoop

		default:
			c.log.Println("Unhandled message from router:", msg.MessageType(), msg)
		}
	}

	close(c.done)
	if c.debug {
		c.log.Println("Client", c.id, "closed")
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
		sync <- struct{}{}
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
		ctx, cancel := context.WithCancel(context.Background())
		c.invHandlerKill[msg.Request] = cancel
		c.activeInvHandlers.Add(1)
		go func() {
			defer cancel()
			resChan := make(chan *InvokeResult)
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
				result = &InvokeResult{Err: wamp.ErrCanceled}
				c.log.Println("INVOCATION", msg.Request, "canceled by router")
			}

			if result.Err != "" {
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
		cancel()
		delete(c.invHandlerKill, msg.Request)
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
		sync <- struct{}{}
	}
	<-sync
}

func (c *Client) waitForReply(id wamp.ID) (wamp.Message, error) {
	sync := make(chan struct{})
	var wait chan wamp.Message
	var ok bool
	c.actionChan <- func() {
		wait, ok = c.awaitingReply[id]
		sync <- struct{}{}
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
		err = fmt.Errorf("timeout while waiting for reply")
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
		sync <- struct{}{}
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
			err = fmt.Errorf("timeout while waiting for reply")
		}
	}
	c.actionChan <- func() {
		delete(c.awaitingReply, id)
	}
	return msg, err
}
