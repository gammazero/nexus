package filtertool

import (
	"context"
	"errors"
	"fmt"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

// FilterFunc is a function that is called, when a client joins the realm of the monitor client.
type FilterFunc func(sessDetails wamp.Dict) (allowed bool)

type filter struct {
	checkAllowed FilterFunc
	whiteset     map[wamp.ID]struct{}
	whitelist    []wamp.ID
}

type FilterTool struct {
	monitor     *client.Client
	newSessChan chan wamp.Dict
	delSessChan chan wamp.ID
	actChan     chan func()
	filters     map[string]*filter
	sessSignal  chan struct{}
}

// Meta events and procedures to get information about sessions.
const (
	metaOnJoin  = string(wamp.MetaEventSessionOnJoin)
	metaOnLeave = string(wamp.MetaEventSessionOnLeave)
	metaList    = string(wamp.MetaProcSessionList)
	metaGet     = string(wamp.MetaProcSessionGet)
)

// New creates a new FilterTool instance, using the provided client to monitor
// the sessions joining and leaving a realm.
//
// To use the FilterTool instance, add filters to define a filter function and
// create an associated whitelist that can be retrieved at anytime.
//
// FilterTool only cares about session details, not about topics that each
// session is subscribed to.  This allows the whitelist to be computed when a
// client joins the realm, instead of when it subscribes or unsubscribes.  The
// reduces whitelist recomputation and helps ensure that sessions do not miss
// events they were published close to the time a client subscribed.
//
// The monitor session is not treated as special, and may be included in
// whitelists if filters allow it.
func New(monitor *client.Client) (*FilterTool, error) {
	f := &FilterTool{
		monitor:     monitor,
		newSessChan: make(chan wamp.Dict),
		delSessChan: make(chan wamp.ID),
		actChan:     make(chan func()),
		filters:     map[string]*filter{},
		sessSignal:  make(chan struct{}, 1),
	}
	if err := f.runMonitor(); err != nil {
		return nil, err
	}
	return f, nil
}

// AddFilter creates a new named filter, or replaces an existing one.  The
// named filter can any string, or URI, that uniquely identifies the filter.
// The name is used to retrieve the session ID whitelist associated with the
// filter.
func (f *FilterTool) AddFilter(name string, function FilterFunc) error {
	fltr := &filter{
		checkAllowed: function,
		whiteset:     map[wamp.ID]struct{}{},
	}
	err := f.initFilter(fltr)
	if err != nil {
		return fmt.Errorf("failed to initialize \"%s\" filter: %s", name, err)
	}

	sync := make(chan struct{})
	f.actChan <- func() {
		f.filters[name] = fltr
		close(sync)
	}
	<-sync

	return nil
}

// Remove filter removes the named filter.
func (f *FilterTool) RemoveFilter(filterName string) {
	sync := make(chan struct{})
	f.actChan <- func() {
		delete(f.filters, filterName)
		close(sync)
	}
	<-sync
}

// Whitelist returns the list of IDs of allowed session.  This list is sutiable
// for use as the whitelist supplied when publishing an event.  A boolean is
// also returned to indicate is the named filter exists.
//
// Only whitelists may be used for client-side filtering since a blacklist may
// fail to exclude a disallowed client that joined after the blacklist was
// retrieved.
func (f *FilterTool) Whitelist(name string) ([]wamp.ID, bool) {
	var wl []wamp.ID
	var ok bool
	sync := make(chan struct{})
	f.actChan <- func() {
		var fltr *filter
		if fltr, ok = f.filters[name]; ok {
			wl = fltr.whitelist
		}
		close(sync)
	}
	<-sync
	return wl, ok
}

// SessionEvent returns a channel to wait on for a signal that FilterTool has
// completed processing a session join or leave event.  This is used when
// adding or removing a client, to wait for any potential change to whitelists
// that may have happened.
func (f *FilterTool) SessionEvent() <-chan struct{} { return f.sessSignal }

// runMonitor starts a goroutine that reads information from the monitor
// session's meta event handlers.  The goroutine exits when the client is
// closed.
func (f *FilterTool) runMonitor() error {
	go func() {
		for {
			select {
			case sessionDetails := <-f.newSessChan:
				f.addSession(sessionDetails)
				f.signalSessionEvent()
			case s := <-f.delSessChan:
				f.delSession(s)
				f.signalSessionEvent()
			case action := <-f.actChan:
				action()
			case <-f.monitor.Done():
				return
			}
		}
	}()

	// Publisher subscribes to on_join and on_leave events.
	leaveHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) == 0 {
			return
		}
		if sid, ok := wamp.AsID(args[0]); ok {
			f.delSessChan <- sid
		}
	}
	joinHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		if len(args) == 0 {
			return
		}
		if sessionDetails, ok := wamp.AsDict(args[0]); ok {
			f.newSessChan <- sessionDetails
		}
	}
	err := f.monitor.Subscribe(metaOnJoin, joinHandler, nil)
	if err != nil {
		return fmt.Errorf("subscribe error: %s", err)
	}

	if err = f.monitor.Subscribe(metaOnLeave, leaveHandler, nil); err != nil {
		return fmt.Errorf("subscribe error: %s", err)
	}

	return nil
}

// addSession is called when a session joins the realm, to add the session to
// the whitelist of filters that allow it.
func (f *FilterTool) addSession(details wamp.Dict) {
	sid, ok := wamp.AsID(details["session"])
	if !ok {
		return
	}
	for i := range f.filters {
		fltr := f.filters[i]
		// Check if session alrady allowed.
		if _, ok = fltr.whiteset[sid]; ok {
			continue
		}
		// Check if session allowed.
		if fltr.checkAllowed(details) {
			fltr.whiteset[sid] = struct{}{}
			// Create a new read-only slice of session IDs.
			newlist := make([]wamp.ID, len(fltr.whiteset))
			copy(newlist, fltr.whitelist)
			newlist[len(fltr.whitelist)] = sid
			fltr.whitelist = newlist
		}
	}
}

// delSession is called when a session leaves the realm, to remove the session
// from the whitelist of any sessions.
func (f *FilterTool) delSession(sid wamp.ID) {
	for i := range f.filters {
		fltr := f.filters[i]
		if _, ok := fltr.whiteset[sid]; !ok {
			continue
		}
		delete(fltr.whiteset, sid)
		if len(fltr.whiteset) == 0 {
			fltr.whitelist = nil
			return
		}
		// Create a new real-only slice of session IDs.
		newlist := make([]wamp.ID, len(fltr.whiteset))
		var j int
		for s := range fltr.whiteset {
			newlist[j] = s
			j++
		}
		fltr.whitelist = newlist
	}
}

func (f *FilterTool) signalSessionEvent() {
	select {
	case f.sessSignal <- struct{}{}:
	default:
	}
}

// initFilter initializes a filter by building a whitelist from all existing
// sessions.
func (f *FilterTool) initFilter(fltr *filter) error {
	// Call meta procedure to get list of sessions.
	ctx := context.Background()
	result, err := f.monitor.Call(ctx, metaList, nil, nil, nil, "")
	if err != nil {
		return fmt.Errorf("could not get session list: %s", err)
	}
	if len(result.Arguments) == 0 {
		return nil
	}
	list, ok := wamp.AsList(result.Arguments[0])
	if !ok {
		return errors.New("could not convert result to wamp.List")
	}

	// Iterate the list of sessions, and get info for each.
	for i := range list {
		// Call meta procedure to get session info.
		sid, ok := wamp.AsID(list[i])
		if !ok {
			continue
		}
		result, err = f.monitor.Call(ctx, metaGet, nil, wamp.List{sid}, nil, "")
		if err != nil {
			if rpcErr, ok := err.(client.RPCError); ok {
				if rpcErr.Err.Error == wamp.ErrNoSuchSession {
					continue
				}
			}
			return fmt.Errorf("could not get session info: %s", err)
		}
		if len(result.Arguments) == 0 {
			continue
		}
		sessionDetails, ok := wamp.AsDict(result.Arguments[0])
		if !ok {
			return errors.New("could not convert result to wamp.Dict")
		}

		if fltr.checkAllowed(sessionDetails) {
			fltr.whiteset[sid] = struct{}{}
			fltr.whitelist = append(fltr.whitelist, sid)
		}
	}
	return nil
}
