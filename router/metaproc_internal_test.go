package router

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport"
	"github.com/gammazero/nexus/v3/wamp"
)

// newMetaProcRealm returns a realm with only what registerMetaProcedure uses,
// and the peer that plays the router side of the realm's meta peer.
func newMetaProcRealm() (*realm, wamp.Peer) {
	cli, rtr := transport.LinkedPeers()
	r := &realm{
		metaPeer:    cli,
		metaIDGen:   new(wamp.IDGen),
		metaProcMap: map[wamp.ID]func(*wamp.Invocation) wamp.Message{},
		log:         logger,
	}
	return r, rtr
}

// replyToRegister receives the REGISTER that registerMetaProcedure sends to
// rtr, checks it, and replies with the message that reply returns for it.
func replyToRegister(t *testing.T, rtr wamp.Peer, procedure wamp.URI, reply func(*wamp.Register) wamp.Message) {
	go func() {
		msg := <-rtr.Recv()
		reg, ok := msg.(*wamp.Register)
		if !ok {
			t.Errorf("expected REGISTER, got %v", msg)
			rtr.Close()
			return
		}
		if reg.Procedure != procedure {
			t.Errorf("wrong procedure: %s", reg.Procedure)
		}
		if !wamp.OptionFlag(reg.Options, "disclose_caller") {
			t.Error("REGISTER does not have disclose_caller")
		}
		if replyMsg := reply(reg); replyMsg != nil {
			rtr.Send() <- replyMsg
		} else {
			rtr.Close()
		}
	}()
}

func TestRegisterMetaProcedure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, rtr := newMetaProcRealm()

		const proc1, proc2 = wamp.URI("test.meta.one"), wamp.URI("test.meta.two")
		var requests []wamp.ID
		registered := func(regID wamp.ID) func(*wamp.Register) wamp.Message {
			return func(reg *wamp.Register) wamp.Message {
				requests = append(requests, reg.Request)
				return &wamp.Registered{Request: reg.Request, Registration: regID}
			}
		}
		handler := func(name string) func(*wamp.Invocation) wamp.Message {
			return func(inv *wamp.Invocation) wamp.Message {
				return &wamp.Yield{Request: inv.Request, Arguments: wamp.List{name}}
			}
		}

		replyToRegister(t, rtr, proc1, registered(101))
		r.registerMetaProcedure(proc1, handler("one"))
		replyToRegister(t, rtr, proc2, registered(202))
		r.registerMetaProcedure(proc2, handler("two"))

		// Each request has the next meta request ID.
		require.Equal(t, []wamp.ID{1, 2}, requests)

		// Each handler is stored by the registration ID from the dealer.
		require.Len(t, r.metaProcMap, 2)
		for regID, name := range map[wamp.ID]string{101: "one", 202: "two"} {
			f := r.metaProcMap[regID]
			require.NotNil(t, f, "no handler for registration %d", regID)
			rsp := f(&wamp.Invocation{Request: 7, Registration: regID})
			require.Equal(t, &wamp.Yield{Request: 7, Arguments: wamp.List{name}}, rsp)
		}
	})
}

func TestRegisterMetaProcedureError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, rtr := newMetaProcRealm()
		const proc = wamp.URI("test.meta.proc")
		replyToRegister(t, rtr, proc, func(reg *wamp.Register) wamp.Message {
			return &wamp.Error{
				Type:      wamp.REGISTER,
				Request:   reg.Request,
				Error:     wamp.ErrProcedureAlreadyExists,
				Arguments: wamp.List{"already registered"},
			}
		})
		require.PanicsWithValue(t,
			"PANIC! Failed to register session meta procedure: "+
				string(wamp.ErrProcedureAlreadyExists)+": already registered",
			func() { r.registerMetaProcedure(proc, nil) })
		require.Empty(t, r.metaProcMap)
	})
}

func TestRegisterMetaProcedureUnexpected(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, rtr := newMetaProcRealm()
		const proc = wamp.URI("test.meta.proc")
		replyToRegister(t, rtr, proc, func(reg *wamp.Register) wamp.Message {
			return &wamp.Subscribed{Request: reg.Request, Subscription: 1}
		})
		require.PanicsWithValue(t, "cannot register meta procedure",
			func() { r.registerMetaProcedure(proc, nil) })
		require.Empty(t, r.metaProcMap)
	})
}

// TestRegisterMetaProcedureShutdown checks that registerMetaProcedure returns
// without registering anything if the meta session ends or is closed while it
// waits for the reply.
func TestRegisterMetaProcedureShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const proc = wamp.URI("test.meta.proc")

		r, rtr := newMetaProcRealm()
		replyToRegister(t, rtr, proc, func(*wamp.Register) wamp.Message {
			return &wamp.Goodbye{Reason: wamp.CloseSystemShutdown}
		})
		r.registerMetaProcedure(proc, nil)
		require.Empty(t, r.metaProcMap, "registered after GOODBYE")

		r, rtr = newMetaProcRealm()
		replyToRegister(t, rtr, proc, func(*wamp.Register) wamp.Message {
			return nil // close the peer
		})
		r.registerMetaProcedure(proc, nil)
		require.Empty(t, r.metaProcMap, "registered after peer closed")
	})
}
