package router

import (
	"github.com/gammazero/nexus/v3/wamp"
	"testing"
)

func TestRealm_sessionList(t *testing.T) {
	realm := new(realm)
	invocation := wamp.Invocation{
		Arguments: make(wamp.List, 1),
	}

	response := realm.sessionList(&invocation)
	errorMessage, ok := response.(*wamp.Error)
	if ok {
		t.Fatal("Response contains error", errorMessage)
	}
}
