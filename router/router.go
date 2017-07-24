package router

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gammazero/alog"
	"github.com/gammazero/nexus/wamp"
)

var log alog.StdLogger

var once sync.Once

// Set logger sets the logger for the router package.
func SetLogger(logger alog.StdLogger) {
	once.Do(func() { log = logger })
}

// Log returns the logger that the router package i
func Logger() alog.StdLogger { return log }

// Enable debug logging for router package.
var DebugEnabled bool

const helloTimeout = 5 * time.Second

// Advertise roles supported by this router.  Feature information is provided
// by the broker and dealer implementations.
var routerRoles = map[string]interface{}{
	"roles": map[string]interface{}{
		"broker": map[string]interface{}{},
		"dealer": map[string]interface{}{},
	},
}

// A Router handles new Peers and routes requests to the requested Realm.
type Router interface {
	// AddRealm creates a new Realm and adds that to the router.
	AddRealm(wamp.URI, bool, bool) (*Realm, error)

	// AddCustomRealm the given realm to the router.
	//AddCustomRealm(realm *Realm) error

	// Attach connects a client to the router and to the requested realm.
	Attach(wamp.Peer) error

	// LocalClient starts a local session and returns a Peer to communicate
	// with that session.
	LocalClient(wamp.URI, map[string]interface{}) (wamp.Peer, error)

	// AddSessionCreateCallback registers a function to call when a new router
	// session is created.
	AddSessionCreateCallback(func(*Session, string))

	// AddSessionCloseCallback registers a function to call when a router
	// session is closed.
	AddSessionCloseCallback(func(*Session, string))

	// Close stops the router and waits message processing to stop.
	Close()
}

type routerReq struct {
	session *Session
	msg     wamp.Message
}

// DefaultRouter is the default WAMP router implementation.
type router struct {
	realms                 map[wamp.URI]*Realm
	sessionCreateCallbacks []func(*Session, string)
	sessionCloseCallbacks  []func(*Session, string)

	actionChan  chan func()
	closingChan chan struct{}
	waitReamls  sync.WaitGroup

	autoRealm bool
	strictURI bool
}

// NewRouter creates a WAMP router.
//
// If authRealm is true, realms that do not exist are automatically created on
// client HELLO.  Caution, enabling this allows unauthenticated clients to
// create new realms.
//
// The strictURI parameter enabled strict URI validation.
func NewRouter(autoRealm, strictURI bool) Router {
	r := &router{
		realms:                 map[wamp.URI]*Realm{},
		sessionCreateCallbacks: []func(*Session, string){},
		sessionCloseCallbacks:  []func(*Session, string){},
		closingChan:            make(chan struct{}),
		actionChan:             make(chan func()),

		autoRealm: autoRealm,
		strictURI: strictURI,
	}
	go r.routerRun()
	return r
}

// Single goroutine used to safely access router data.
func (r *router) routerRun() {
	for action := range r.actionChan {
		action()
	}
}

// AddRealm creates a new Realm and adds that to the router.
//
// At least one realm is needed, unless automatic realm creation is enabled.
func (r *router) AddRealm(uri wamp.URI, anonymousAuth, allowDisclose bool) (*Realm, error) {
	if !uri.ValidURI(r.strictURI, "") {
		return nil, fmt.Errorf(
			"invalid realm URI %v (URI strict checking %v)", uri, r.strictURI)
	}
	var realm *Realm
	sync := make(chan error)
	r.actionChan <- func() {
		if _, ok := r.realms[uri]; ok {
			sync <- errors.New("realm already exists: " + string(uri))
			return
		}
		realm = NewRealm(uri, r.strictURI, anonymousAuth, allowDisclose)
		r.realms[uri] = realm
		sync <- nil
	}
	err := <-sync
	if err != nil {
		return nil, fmt.Errorf("error adding realm: %v", err)
	}
	log.Print("Added realm: ", uri)
	return realm, nil
}

// Attach connects a client to the router and to the requested realm.
func (r *router) Attach(client wamp.Peer) error {
	sendAbort := func(reason wamp.URI, abortErr error) {
		abortMsg := wamp.Abort{Reason: reason}
		if abortErr != nil {
			abortMsg.Details = map[string]interface{}{"error": abortErr.Error()}
			log.Print("Aborting client connection: ", abortErr)
		}
		client.Send(&abortMsg)
		client.Close()
	}

	// Check that router is not shutting down.
	if r.closing() {
		sendAbort(wamp.ErrSystemShutdown, nil)
		return errors.New("router is closing, not accepting new clients")
	}

	// Receive HELLO message from the client.
	msg, err := wamp.RecvTimeout(client, helloTimeout)
	if err != nil {
		return errors.New("did not receive HELLO: " + err.Error())
	}
	log.Printf("New client sent: %s: %+v", msg.MessageType(), msg)

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
	var realm *Realm
	sync := make(chan error)
	r.actionChan <- func() {
		// Realm is a string identifying the realm this session should attach
		// to.  Check if the requested realm exists.
		var ok bool
		realm, ok = r.realms[hello.Realm]
		if !ok {
			// If the router is not configured to automatically create the
			// realm, then respond with an ABORT message.
			if !r.autoRealm {
				sendAbort(wamp.ErrNoSuchRealm, nil)
				sync <- fmt.Errorf("no realm \"%s\" exists on this router",
					string(hello.Realm))
				return
			}
			// Create the new realm that allows anonymous authentication and
			// allows disclosing caller ID.
			realm = NewRealm(hello.Realm, r.strictURI, true, true)
			r.realms[hello.Realm] = realm
			log.Print("Auto-added realm: ", hello.Realm)
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
	roleVals, ok := _roleVals.(map[string]interface{})
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
			hello.Details = map[string]interface{}{}
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
	if welcome.Details == nil {
		welcome.Details = map[string]interface{}{}
	}
	for k, v := range routerRoles {
		if _, ok := welcome.Details[k]; !ok {
			welcome.Details[k] = v
		}
	}
	roles := welcome.Details["roles"].(map[string]interface{})
	roles["broker"] = realm.broker.Features()
	roles["dealer"] = realm.dealer.Features()

	client.Send(welcome)

	// Populate session details.
	details := map[string]interface{}{}
	details["realm"] = hello.Realm
	details["roles"] = roles
	details["authid"] = welcome.Details["authid"]
	details["authrole"] = welcome.Details["authrole"]
	details["authmethod"] = welcome.Details["authmethod"]
	details["authprovider"] = welcome.Details["authprovider"]

	// Create new session.
	sess := &Session{
		Peer:    client,
		ID:      welcome.ID,
		Details: details,
		stop:    make(chan wamp.URI, 1),
	}

	log.Print("Created session: ", welcome.ID)

	// Need synchronized access to r.sessionCreateCallbacks.
	r.actionChan <- func() {
		for _, callback := range r.sessionCreateCallbacks {
			go callback(sess, string(hello.Realm))
		}
	}
	r.waitReamls.Add(1)
	go func() {
		realm.handleSession(sess, false)
		sess.Close()

		// Need synchronized access to r.sessionCloseCallbacks.
		r.actionChan <- func() {
			for _, callback := range r.sessionCloseCallbacks {
				go callback(sess, string(hello.Realm))
			}
		}
		r.waitReamls.Done()
	}()
	return nil
}

// LocalClient returns a Peer connected to a local session running within the
// specified realm.
//
// This allows creation and attachment of a client session without having to
// go through the process of HELLO... auth... WELCOME.
func (r *router) LocalClient(realmURI wamp.URI, details map[string]interface{}) (wamp.Peer, error) {
	sync := make(chan *Realm)
	r.actionChan <- func() {
		realm := r.realms[realmURI]
		sync <- realm
	}
	realm := <-sync
	if realm == nil {
		return nil, errors.New("no such realm: " + string(realmURI))
	}

	// Start internal session and return remote leg of router uplink.
	return realm.bridgeSession(details, false)
}

// AddSessionCreateCallback registers a function to call when a new router
// session is created.
func (r *router) AddSessionCreateCallback(fn func(*Session, string)) {
	r.actionChan <- func() {
		r.sessionCreateCallbacks = append(r.sessionCreateCallbacks, fn)
	}
}

// AddSessionCloseCallback registers a function to call when a router session
// is closed.
func (r *router) AddSessionCloseCallback(fn func(*Session, string)) {
	r.actionChan <- func() {
		r.sessionCloseCallbacks = append(r.sessionCloseCallbacks, fn)
	}
}

// Close stops the router and waits message processing to stop.
func (r *router) Close() {
	if r.closing() {
		return
	}
	close(r.closingChan)
	sync := make(chan struct{})
	r.actionChan <- func() {
		for i := range r.realms {
			r.realms[i].Close()
		}
		sync <- struct{}{}
	}
	<-sync
	r.waitReamls.Wait()
	close(r.actionChan)
}

// Closed returns true if the router has been closed.
func (r *router) closing() bool {
	select {
	case <-r.closingChan:
		return true
	default:
	}
	return false
}
