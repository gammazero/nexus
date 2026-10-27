package router

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

// syncBuffer is a bytes.Buffer that is safe to write from several goroutines,
// as the router's logger does.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestRealmDebug checks that RealmConfig.Debug enables debug logging for only
// its realm, and that Config.Debug enables it for every realm.
func TestRealmDebug(t *testing.T) {
	for _, routerDebug := range []bool{false, true} {
		var logs syncBuffer
		config := &Config{
			RealmConfigs: []*RealmConfig{
				{URI: testRealm, AnonymousAuth: true, Debug: true},
				{URI: testRealm2, AnonymousAuth: true},
			},
			Debug: routerDebug,
		}
		r, err := NewRouter(config, log.New(&logs, "", 0))
		require.NoError(t, err)

		for uri, want := range map[wamp.URI]bool{testRealm: true, testRealm2: routerDebug} {
			rlm := r.(*router).realms[uri]
			require.Equal(t, want, rlm.debug, "realm %s, router debug %v", uri, routerDebug)
			require.Equal(t, want, rlm.broker.debug, "broker %s, router debug %v", uri, routerDebug)
			require.Equal(t, want, rlm.dealer.debug, "dealer %s, router debug %v", uri, routerDebug)

			// The realm's debug logging shows in the router's log.
			cli := testClientInRealm(t, r, uri)
			attached := fmt.Sprintf("Finished attaching session: %d", cli.ID)
			require.Equal(t, want, strings.Contains(logs.String(), attached),
				"debug log for realm %s, router debug %v", uri, routerDebug)
			cli.Close()
		}
		// Config.Debug does not change the caller's realm configs.
		require.False(t, config.RealmConfigs[1].Debug)
		r.Close()
	}
}
