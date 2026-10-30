package client_test

import (
	"log"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

// chanWriter is an io.Writer that sends each write to a channel, so that a
// test can read what a log.Logger writes from another goroutine.
type chanWriter chan string

func (w chanWriter) Write(p []byte) (int, error) {
	w <- string(p)
	return len(p), nil
}

func TestConnectLocal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := getTestRouter(t, newTestRealmConfig(testRealm))

		// The client joins the realm, and has a logger if none is given.
		cl, err := client.ConnectLocal(r, client.Config{Realm: testRealm})
		require.NoError(t, err)
		defer cl.Close()
		require.NotEqual(t, wamp.ID(0), cl.ID())
		require.NotNil(t, cl.Logger())
		result, err := cl.Call(t.Context(), string(wamp.MetaProcSessionCount),
			nil, nil, nil, nil)
		require.NoError(t, err)
		count, _ := wamp.AsInt64(result.Arguments[0])
		require.Equal(t, int64(1), count)

		// The router failing to attach the client is an error, which is
		// written to the logger.
		logs := make(chanWriter, 10)
		_, err = client.ConnectLocal(r, client.Config{
			Realm:           "nexus.no.such.realm",
			ResponseTimeout: time.Second,
			Logger:          log.New(logs, "", 0),
		})
		require.ErrorContains(t, err, string(wamp.ErrNoSuchRealm))
		synctest.Wait()
		var attachErr bool
		for len(logs) != 0 {
			if strings.Contains(<-logs, `no realm "nexus.no.such.realm"`) {
				attachErr = true
			}
		}
		require.True(t, attachErr, "attach error not logged")

		// Logging the attach error does not need a logger.
		_, err = client.ConnectLocal(r, client.Config{
			Realm:           "nexus.no.such.realm",
			ResponseTimeout: time.Second,
		})
		require.ErrorContains(t, err, string(wamp.ErrNoSuchRealm))
		synctest.Wait()
	})
}
