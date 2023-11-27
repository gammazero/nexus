package router

import (
	"errors"
	//"fmt"
	//"strconv"
	//"sync"

	//"github.com/dtegapp/nexus/v3/router/auth"
	//"github.com/dtegapp/nexus/v3/stdlog"
	//"github.com/dtegapp/nexus/v3/transport"
	"github.com/dtegapp/nexus/v3/wamp"
)

// killSession closes the session identified by session ID.  The meta session
// cannot be closed.
func (r *realm) SessionKill(sid wamp.ID, reason wamp.URI, message string) error {
	goodbye := makeGoodbye(reason, message)
	errChan := make(chan error)
	r.actionChan <- func() {
		sess, ok := r.clients[sid]
		if !ok {
			errChan <- errors.New("no such session")
			return
		}
		sess.EndRecv(goodbye)
		close(errChan)
	}
	return <-errChan
}
