/*
Package router provides a WAMP router implementation that supports most of the
WAMP advanced profile, offers multiple transports and TLS, and extends
publication filtering functionality.

*/
package router

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/wamp"
)

const helloTimeout = 5 * time.Second

// A Router handles new Peers and routes requests to the requested Realm.
type Router interface {
	// Attach connects a client to the router and to the requested realm.
	Attach(wamp.Peer) error

	// AttachClient connects a client to the router and to the requested realm.
	// It provides additional transport information details.
	AttachClient(wamp.Peer, wamp.Dict) error

	// Close stops the router and waits message processing to stop.
	Close()

	// Logger returns the logger the router is using.
	Logger() stdlog.StdLog

	// AddRealm will append a realm to this router
	AddRealm(*RealmConfig) error

	// RemoveRealm will attempt to remove a realm from this router
	RemoveRealm(wamp.URI)
}

// router is the default WAMP router implementation.
type router struct {
	realms map[wamp.URI]*realm

	actionChan chan func()
	waitRealms sync.WaitGroup

	realmTemplate *RealmConfig
	closed        bool

	log   stdlog.StdLog
	debug bool
}

// NewRouter creates a WAMP router instance.
func NewRouter(config *Config, logger stdlog.StdLog) (Router, error) {
	// If logger not provided, create one.
	if logger == nil {
		logger = log.New(os.Stdout, "", log.LstdFlags)
	}
	logger.Println("Starting router")

	r := &router{
		realms:        map[wamp.URI]*realm{},
		actionChan:    make(chan func()),
		realmTemplate: config.RealmTemplate,
		log:           logger,
		debug:         config.Debug,
	}

	for _, realmConfig := range config.RealmConfigs {
		if _, err := r.addRealm(realmConfig); err != nil {
			return nil, err
		}
	}

	// Create a realm from the template to validate the template
	if r.realmTemplate != nil {
		realmTemplate := *r.realmTemplate
		realmTemplate.URI = "some.valid.realm"
		if _, err := newRealm(&realmTemplate, nil, nil, r.log, r.debug); err != nil {
			return nil, fmt.Errorf("Invalid realmTemplate: %s", err)
		}
	}

	go r.run()
	return r, nil
}

// Logger returns the StdLog that the router uses for logging.
func (r *router) Logger() stdlog.StdLog { return r.log }

// Attach connects a client to the router and to the requested realm.  If
// successful, Attach returns after sending a WELCOME message to the client.
func (r *router) Attach(client wamp.Peer) error {
	return r.AttachClient(client, nil)
}

// AttachClient connects a client to the router and to the requested realm.  If
// successful, Attach returns after sending a WELCOME message to the client.
//
// Additional information is provided in transportDetails.  This information
// becomes part of HELLO.Details and session.Details, as details["transport"].
// This exposes it to authenticator and authorizer logic.  The information
// includes items useful for authentication, in details.transport.auth.
//
// See websocketpeer.WebSocketConfig for information provided by websocket
// connections.
func (r *router) AttachClient(client wamp.Peer, transportDetails wamp.Dict) error {
	sendAbort := func(reason wamp.URI, abortErr error) {
		abortMsg := wamp.Abort{Reason: reason}
		abortMsg.Details = wamp.Dict{}
		if abortErr != nil {
			abortMsg.Details["error"] = abortErr.Error()
			r.log.Println("Aborting client connection:", abortErr)
		}
		client.Send(&abortMsg) // Blocking OK; this is session goroutine.
		client.Close()
	}

	// Receive HELLO message from the client.
	msg, err := wamp.RecvTimeout(client, helloTimeout)
	if err != nil {
		return errors.New("did not receive HELLO: " + err.Error())
	}
	if r.debug {
		r.log.Printf("New client sent: %s: %+v", msg.MessageType(), msg)
	}

	// A WAMP session is initiated by the Client sending a HELLO message to the
	// Router.  The HELLO message MUST be the very first message sent by the
	// Client after the transport has been established.
	hello, ok := msg.(*wamp.Hello)
	if !ok {
		// Received unexpected message - protocol violation.
		err = fmt.Errorf("expected HELLO, received %s", msg.MessageType())
		sendAbort(wamp.ErrProtocolViolation, err)
		return err
	}

	// Client is required to provide a non-empty realm.
	if string(hello.Realm) == "" {
		err = errors.New("no realm requested")
		sendAbort(wamp.ErrNoSuchRealm, err)
		return err
	}
	// Lookup or create realm to attach to.
	var realm *realm
	sync := make(chan error)
	r.actionChan <- func() {
		if r.closed {
			sendAbort(wamp.ErrSystemShutdown, nil)
			sync <- errors.New("router is closing, not accepting new clients")
			return
		}
		// Realm is a string identifying the realm this session should attach
		// to.  Check if the requested realm exists.
		var found bool
		realm, found = r.realms[hello.Realm]
		if !found {
			// If the router is not configured to automatically create the
			// realm, then respond with an ABORT message.
			if r.realmTemplate == nil {
				sendAbort(wamp.ErrNoSuchRealm, nil)
				sync <- fmt.Errorf("no realm \"%s\" exists on this router",
					string(hello.Realm))
				return
			}

			// Create the new realm based on template
			config := *r.realmTemplate
			config.URI = hello.Realm
			if realm, err = r.addRealm(&config); err != nil {
				sendAbort(wamp.ErrNoSuchRealm, nil)
				sync <- fmt.Errorf("failed to create realm \"%s\"",
					string(hello.Realm))
				return

			}
			r.log.Println("Auto-added realm:", hello.Realm)
		}
		sync <- nil
	}
	err = <-sync
	if err != nil {
		return err
	}

	hello.Details = wamp.NormalizeDict(hello.Details)
	sid := wamp.GlobalID()

	// Create new session.
	sess := wamp.NewSession(client, sid, nil, hello.Details)

	// A Client must announce the roles it supports via
	// Hello.Details.roles|dict, where the keys can be: publisher, subscriber,
	// caller, callee.  Check that client has at least one supported role.
	var rolesOK bool
	for _, role := range []string{"publisher", "subscriber", "caller", "callee"} {
		if sess.HasRole(role) {
			rolesOK = true
			break
		}
	}
	if !rolesOK {
		err = errors.New("client did not announce any supported roles")
		sendAbort(wamp.ErrNoSuchRole, err)
		return err
	}

	// Include any transport details with HELLO.Details.
	if len(transportDetails) != 0 {
		hello.Details["transport"] = transportDetails
	}

	// Handle any necessary client auth.  This results in either a WELCOME
	// message or an error.
	//
	// Authentication may take some time.
	welcome, err := realm.authClient(sid, client, hello.Details)
	if err != nil {
		sendAbort(wamp.ErrAuthenticationFailed, err)
		return errors.New("authentication error: " + err.Error())
	}

	// Fill in the values of the welcome message and send to client.
	welcome.ID = sid

	// Session needs details from HELLO and from WELCOME, but roles from HELLO
	// only.
	sessDetails := make(wamp.Dict, len(hello.Details)+len(welcome.Details))
	for k, v := range hello.Details {
		if k == "authmethods" || k == "roles" {
			continue
		}
		sessDetails[k] = v
	}
	for k, v := range welcome.Details {
		if k == "roles" {
			continue
		}
		sessDetails[k] = v
	}
	sessDetails["session"] = sid

	sess.Details = sessDetails

	if err := realm.handleSession(sess); err != nil {
		// Any error returned here is a shutdown error.
		sendAbort(wamp.ErrSystemShutdown, nil)
		return err
	}

	client.Send(welcome) // Blocking OK; this is session goroutine.
	if r.debug {
		r.log.Println("Created session:", sid)
	}
	return nil
}

// Close stops the router and waits message processing to stop.
func (r *router) Close() {
	sync := make(chan struct{})
	r.actionChan <- func() {
		// Prevent new or attachment to existing realms.
		r.closed = true
		// Close all existing realms.
		for uri, realm := range r.realms {
			realm.close()
			// Delete the realm
			delete(r.realms, uri)
			r.log.Println("Realm", uri, "completed shutdown")
		}
		close(sync)
	}
	<-sync
	// Wait for all existing realms to close.
	r.waitRealms.Wait()
	close(r.actionChan)
	r.log.Println("Router stopped")
}

// AddRealm allows the addition of a realm after construction
func (r *router) AddRealm(config *RealmConfig) error {
	var err error
	sync := make(chan struct{})
	r.actionChan <- func() {
		_, err = r.addRealm(config)
		close(sync)
	}
	<-sync
	return err
}

// RemoveRealm will close and then remove a realm from this router, if the realm exists.
func (r *router) RemoveRealm(name wamp.URI) {
	// Because we want to force atomicity as briefly as possible, the atomic
	// func will be used purely to attempt to locate the realm
	var realm *realm
	var ok bool
	sync := make(chan struct{})
	r.actionChan <- func() {
		if realm, ok = r.realms[name]; ok {
			// if found, go ahead and remove the realm from the router to
			// prevent new clients from joining it.
			delete(r.realms, name)
			r.log.Printf("Removed realm: %s", name)
		}
		close(sync)
	}
	// wait until the atomic func has completed
	<-sync
	// if the realm was found within the router, close it outside of the atomic
	// func while still blocking the caller
	if ok {
		realm.close()
		r.log.Println("Realm", name, "was removed and completed shutdown")
	}
}

// addRealm attempts to create and add a realm to this router.
//
// this method should ONLY be called from within an atomic func
func (r *router) addRealm(config *RealmConfig) (*realm, error) {
	if _, ok := r.realms[config.URI]; ok {
		return nil, errors.New("realm already exists: " + string(config.URI))
	}

	realm, err := newRealm(
		config,
		newBroker(r.log, config.StrictURI, config.AllowDisclose, r.debug, config.PublishFilterFactory),
		newDealer(r.log, config.StrictURI, config.AllowDisclose, r.debug),
		r.log, r.debug)
	if err != nil {
		return nil, err
	}
	r.realms[config.URI] = realm

	r.waitRealms.Add(1)
	go func() {
		realm.run()
		r.waitRealms.Done()
	}()

	realm.waitReady()
	r.log.Println("Added realm:", config.URI)
	return realm, nil
}

// Single goroutine used to safely access router data.
func (r *router) run() {
	for action := range r.actionChan {
		action()
	}
}
