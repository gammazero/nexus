package filtertool

import (
	"context"
	"errors"
	"fmt"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

type FilterFunc func(sessDetails wamp.Dict) (allowed bool)

type FilterTool struct {
	monitor     *client.Client
	newSessChan chan wamp.Dict
	delSessChan chan wamp.ID
	reqWLChan   chan bool
	rspWLChan   chan []wamp.ID
	closeChan   chan struct{}
	doneChan    chan struct{}
	filter      FilterFunc
	whiteList   map[wamp.ID]struct{}
	wlStale     bool
}

const (
	metaOnJoin  = string(wamp.MetaEventSessionOnJoin)
	metaOnLeave = string(wamp.MetaEventSessionOnLeave)
	metaList    = string(wamp.MetaProcSessionList)
	metaGet     = string(wamp.MetaProcSessionGet)
)

func New(monitor *client.Client, filter FilterFunc) (*FilterTool, error) {
	f := &FilterTool{
		monitor:     monitor,
		newSessChan: make(chan wamp.Dict, 1),
		delSessChan: make(chan wamp.ID, 1),
		reqWLChan:   make(chan bool),
		rspWLChan:   make(chan []wamp.ID),
		closeChan:   make(chan struct{}),
		doneChan:    make(chan struct{}),
		filter:      filter,
		whiteList:   map[wamp.ID]struct{}{},
		wlStale:     true,
	}
	errChan := make(chan error)
	go f.updateHandler(errChan)
	err := <-errChan
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (f *FilterTool) UpdateWhiteList(wl []wamp.ID) []wamp.ID {
	var force bool
	if wl == nil {
		f.reqWLChan <- force
		return <-f.rspWLChan
	}
	upWL := <-f.rspWLChan
	if upWL == nil {
		return wl
	}
	return upWL
}

func (f *FilterTool) Close() {
	close(f.closeChan)
	<-f.doneChan
}

func (f *FilterTool) updateHandler(errChan chan error) {
	defer close(f.doneChan)

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
		if d, ok := wamp.AsDict(args[0]); ok {
			f.newSessChan <- d
		}
	}
	err := f.monitor.Subscribe(metaOnJoin, joinHandler, nil)
	if err != nil {
		errChan <- fmt.Errorf("subscribe error: %s", err)
		return
	}
	defer f.monitor.Unsubscribe(metaOnJoin)
	if err = f.monitor.Subscribe(metaOnLeave, leaveHandler, nil); err != nil {
		errChan <- fmt.Errorf("subscribe error: %s", err)
		return
	}
	defer f.monitor.Unsubscribe(metaOnLeave)

	if err = f.getCurrentSessionInfo(); err != nil {
		errChan <- fmt.Errorf("failed to get session info: %s", err)
		return
	}

	errChan <- nil

	for {
		select {
		case d := <-f.newSessChan:
			f.addSession(d)
		case s := <-f.delSessChan:
			f.delSession(s)
		case force := <-f.reqWLChan:
			var wl []wamp.ID
			if force || f.wlStale {
				wl = make([]wamp.ID, len(f.whiteList))
				var i int
				for sid := range f.whiteList {
					wl[i] = sid
					i++
				}
			}
			f.rspWLChan <- wl
		case <-f.closeChan:
			return
		}
	}
}

func (f *FilterTool) addSession(d wamp.Dict) {
	sid, ok := wamp.AsID(d["session"])
	if !ok {
		return
	}
	if f.filter(d) {
		f.whiteList[sid] = struct{}{}
	}
}

func (f *FilterTool) delSession(s wamp.ID) {
	delete(f.whiteList, s)
}

func (f *FilterTool) getCurrentSessionInfo() error {
	ctx := context.Background()
	result, err := f.monitor.Call(ctx, metaList, nil, nil, nil, "")
	if err != nil {
		return err
	}
	if len(result.Arguments) == 0 {
		return nil
	}
	list, ok := wamp.AsList(result.Arguments[0])
	if !ok {
		return errors.New("could not convert result to wamp.List")
	}
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
			return err
		}
		if len(result.Arguments) == 0 {
			continue
		}
		dict, ok := wamp.AsDict(result.Arguments[0])
		if !ok {
			return errors.New("could not convert result to wamp.Dict")
		}
		f.addSession(dict)
	}
	return nil
}
