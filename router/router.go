package router

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/wamp"
)

const helloTimeout = 5 * time.Second

// RouterConfig configures the router with realms, and optionally a template
// for creating new realms.
type RouterConfig struct {
	// RealmConfigs defines the configurations for realms within the router.
	RealmConfigs []*RealmConfig `json:"realms"`

	// RealmTemplate, if defined, is used by the router to create new realms
	// when a client joins a realm that does not yet exist.  If RealmTemplate
	// is nil (the default), then clients must join existing realms.
	RealmTemplate *RealmConfig `json:"realm_template"`

	// Enable debug logging for router, realm, broker, dealer
	Debug bool
}

// A Router handles new Peers and routes requests to the requested Realm.
type Router interface {
	// Attach connects a client to the router and to the requested realm.
	Attach(wamp.Peer) error

	// Close stops the router and waits message processing to stop.
	Close()

	// Logger returns the logger the router is using.
	Logger() stdlog.StdLog
}

// DefaultRouter is the default WAMP router implementation.
type router struct {
	realms map[wamp.URI]*realm

	actionChan chan func()
	waitRealms sync.WaitGroup

	realmTemplate *RealmConfig
	closed        bool

	log   stdlog.StdLog
	debug bool
}

// NewRouter creates a WAMP router.
//
// If authRealm is true, realms that do not exist are automatically created on
// client HELLO.  Caution, enabling this allows unauthenticated clients to
// create new realms.
//
// The strictURI parameter enabled strict URI validation.
func NewRouter(config *RouterConfig, logger stdlog.StdLog) (Router, error) {
	if len(config.RealmConfigs) == 0 && config.RealmTemplate == nil {
		return nil, fmt.Errorf("invalid router config. Must define either realms or realmsTemplate, or both")

	}
	// If logger not provided, create one.
	if logger == nil {
		logger = log.New(os.Stdout, "", log.LstdFlags)
	}
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
		if _, err := newRealm(&realmTemplate, r.log, r.debug); err != nil {
			return nil, fmt.Errorf("Invalid realmTemplate: %s", err)
		}
	}

	go r.run()
	return r, nil
}

func (r *router) Logger() stdlog.StdLog { return r.log }

// Single goroutine used to safely access router data.
func (r *router) run() {
	for action := range r.actionChan {
		action()
	}
}

// addRealm creates a new Realm and adds that to the router.
//
// At least one realm is needed, unless automatic realm creation is enabled.
func (r *router) addRealm(config *RealmConfig) (*realm, error) {
	if _, ok := r.realms[config.URI]; ok {
		return nil, errors.New("realm already exists: " + string(config.URI))
	}
	realm, err := newRealm(config, r.log, r.debug)
	if err != nil {
		return nil, err
	}
	r.realms[config.URI] = realm

	r.waitRealms.Add(1)
	go func() {
		realm.run()
		r.waitRealms.Done()
	}()

	r.log.Println("Added realm:", config.URI)
	return realm, nil
}

// Attach connects a client to the router and to the requested realm.
func (r *router) Attach(client wamp.Peer) error {
	sendAbort := func(reason wamp.URI, abortErr error) {
		abortMsg := wamp.Abort{Reason: reason}
		if abortErr != nil {
			abortMsg.Details = wamp.Dict{"error": abortErr.Error()}
			r.log.Println("Aborting client connection:", abortErr)
		}
		client.Send(&abortMsg)
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
		// Note: This URI is not official and there is no requirement to send
		// an error back to the client in this case.  Seems helpful to at least
		// let the client know what was wrong.
		err = fmt.Errorf("protocol error: expected HELLO, received %s",
			msg.MessageType())
		sendAbort(wamp.URI("wamp.exception.protocol_violation"), err)
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
		var ok bool
		realm, ok = r.realms[hello.Realm]
		if !ok {
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
				sync <- fmt.Errorf("failed to create realm \"%s\"", string(hello.Realm))
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

	// A Client must announce the roles it supports via
	// Hello.Details.roles|dict, where the keys can be: publisher, subscriber,
	// caller, callee.  If the client announces any roles, to list specific
	// features for the role, then check that the role is something this router
	// recognizes.
	_roleVals, err := wamp.DictValue(hello.Details, []string{"roles"})
	if err != nil {
		err = errors.New("no client roles specified")
		sendAbort(wamp.ErrNoSuchRole, err)
		return err
	}
	roleVals, ok := _roleVals.(wamp.Dict)
	if !ok || len(roleVals) == 0 {
		err = errors.New("no client roles specified")
		sendAbort(wamp.ErrNoSuchRole, err)
		return err
	}
	for roleName, _ := range roleVals {
		switch roleName {
		case "publisher", "subscriber", "caller", "callee":
		default:
			err = errors.New("invalid client role specified: " + roleName)
			sendAbort(wamp.ErrNoSuchRole, err)
			return err
		}
	}

	// The default authentication method is "WAMP-Anonymous" if client does not
	// specify otherwise.
	if _, ok = hello.Details["authmethods"]; !ok {
		if hello.Details == nil {
			hello.Details = wamp.Dict{}
		}
		hello.Details["authmethods"] = []string{"anonymous"}
	}

	// Handle any necessary client auth.  This results in either a WELCOME
	// message or an error.
	//
	// Authentication may take some some.
	welcome, err := realm.authClient(client, hello.Details)
	if err != nil {
		sendAbort(wamp.ErrAuthenticationFailed, err)
		return errors.New("authentication error: " + err.Error())
	}

	// Fill in the values of the welcome message and send to client.
	welcome.ID = wamp.GlobalID()

	// Session needs details from HELLO and from WELCOME, but roles from HELLO
	// only.
	sessDetails := make(wamp.Dict, len(hello.Details)+len(welcome.Details))
	for k, v := range hello.Details {
		sessDetails[k] = v
	}
	for k, v := range welcome.Details {
		if k == "roles" {
			continue
		}
		sessDetails[k] = v
	}
	sessDetails["session"] = welcome.ID

	// Create new session.
	sess := &Session{
		Peer:    client,
		ID:      welcome.ID,
		Details: sessDetails,
		stop:    make(chan wamp.URI, 1),
	}

	if err := realm.handleSession(sess); err != nil {
		// N.B. assume, for now, that any error is a shutdown error
		sendAbort(wamp.ErrSystemShutdown, nil)
		return err
	}

	client.Send(welcome)
	if r.debug {
		r.log.Println("Created session:", welcome.ID)
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
		sync <- struct{}{}
	}
	<-sync
	// Wait for all existing realms to close.
	r.waitRealms.Wait()
}
