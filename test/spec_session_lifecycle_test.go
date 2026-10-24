package test_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
)

// TestSpecSessionWelcomeFeatures pins the router's advertised broker and
// dealer features in the WELCOME details. If a feature is unintentionally
// dropped from router/broker.go or router/dealer.go, this test fails.
func TestSpecSessionWelcomeFeatures(t *testing.T) {
	checkGoLeaks(t)
	cli := connectClient(t)

	brokerFeatures := []string{
		wamp.FeaturePatternSub,
		wamp.FeaturePubExclusion,
		wamp.FeaturePubIdent,
		wamp.FeatureSessionMetaAPI,
		wamp.FeatureSubBlackWhiteListing,
		wamp.FeatureSubMetaAPI,
		wamp.FeaturePayloadPassthruMode,
		wamp.FeatureEventHistory,
	}
	for _, f := range brokerFeatures {
		require.Truef(t, cli.HasFeature(wamp.RoleBroker, f),
			"broker missing advertised feature %q", f)
	}

	dealerFeatures := []string{
		wamp.FeatureCallCanceling,
		wamp.FeatureCallTimeout,
		wamp.FeatureCallerIdent,
		wamp.FeaturePatternBasedReg,
		wamp.FeatureProgCallResults,
		wamp.FeatureProgCallInvocations,
		wamp.FeatureSessionMetaAPI,
		wamp.FeatureSharedReg,
		wamp.FeatureRegMetaAPI,
		wamp.FeatureTestamentMetaAPI,
		wamp.FeaturePayloadPassthruMode,
	}
	for _, f := range dealerFeatures {
		require.Truef(t, cli.HasFeature(wamp.RoleDealer, f),
			"dealer missing advertised feature %q", f)
	}
}

// TestSpecSessionConnectNonExistentRealm verifies HELLO to an unknown realm is
// rejected. Per WAMP spec §3.4.4 the router responds with ABORT carrying
// wamp.error.no_such_realm; the client surfaces it as a connect error.
func TestSpecSessionConnectNonExistentRealm(t *testing.T) {
	checkGoLeaks(t)
	cfg := client.Config{
		Realm:           "no.such.realm.exists",
		ResponseTimeout: clientResponseTimeout,
	}
	cli, err := connectClientCfgErr(cfg)
	require.Error(t, err, "expected error connecting to non-existent realm")
	require.Nil(t, cli)
	// Surface the URI so accidental error-mapping changes are caught.
	require.ErrorContains(t, err, string(wamp.ErrNoSuchRealm))
}

// TestSpecSessionGoodbyeRoundTrip verifies a clean session shutdown:
// client.Close() sends GOODBYE and the router's Done channel fires.
func TestSpecSessionGoodbyeRoundTrip(t *testing.T) {
	checkGoLeaks(t)
	cli := connectClient(t)

	done := cli.Done()
	select {
	case <-done:
		t.Fatal("client Done channel closed before Close")
	default:
	}

	require.NoError(t, cli.Close())

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("client Done channel did not close after GOODBYE")
	}

	// Re-Close is a no-op error per client contract.
	require.ErrorIs(t, cli.Close(), client.ErrAlreadyClosed)
}

// TestSpecSessionReconnectAfterGoodbye exercises that a clean session teardown
// does not poison subsequent connections (port/socket reuse, router state
// cleanup).
func TestSpecSessionReconnectAfterGoodbye(t *testing.T) {
	checkGoLeaks(t)

	cli1 := connectClient(t)
	id1 := cli1.ID()
	require.NoError(t, cli1.Close())

	cli2 := connectClient(t)
	id2 := cli2.ID()
	require.NotEqual(t, id1, id2, "expected fresh session id after reconnect")
}

// TestSpecSessionWelcomeDetails verifies the WELCOME message's details dict
// contains the realm-assigned authrole and authmethod for the anonymous path.
func TestSpecSessionWelcomeDetails(t *testing.T) {
	checkGoLeaks(t)
	cli := connectClient(t)

	details := cli.RealmDetails()
	require.NotNil(t, details, "WELCOME details missing")

	authrole, _ := wamp.AsString(details["authrole"])
	require.NotEmpty(t, authrole, "WELCOME details missing authrole")

	authmethod, _ := wamp.AsString(details["authmethod"])
	require.NotEmpty(t, authmethod, "WELCOME details missing authmethod")
	// Local in-process connections set authmethod=local; networked anonymous
	// connections set authmethod=anonymous. Both are valid for the default test
	// realm which has AnonymousAuth=true.
	require.Contains(t, []string{"anonymous", "local"}, authmethod,
		"unexpected authmethod %q", authmethod)
}

// TestSpecSessionRouterRoleAdvertised verifies the WELCOME roles dict has both
// broker and dealer entries, which a full WAMP router requires per spec §3.2.
func TestSpecSessionRouterRoleAdvertised(t *testing.T) {
	checkGoLeaks(t)
	cli := connectClient(t)

	details := cli.RealmDetails()
	roles, _ := wamp.AsDict(details["roles"])
	require.NotEmpty(t, roles, "WELCOME details missing roles dict")

	_, hasBroker := roles[string(wamp.RoleBroker)]
	require.True(t, hasBroker, "router did not advertise broker role")

	_, hasDealer := roles[string(wamp.RoleDealer)]
	require.True(t, hasDealer, "router did not advertise dealer role")
}

// TestSpecSessionContextCancelDuringConnect checks that canceling the context
// while ConnectNet waits for the router makes it return the cancellation
// error. The test listens on its own socket, accepts the connection and never
// answers, so the connect can only end by being canceled.
func TestSpecSessionContextCancelDuringConnect(t *testing.T) {
	if scheme == "" {
		t.Skip("an in-process connection has no network connect to cancel")
	}
	checkGoLeaks(t)

	network, address := "tcp", "127.0.0.1:0"
	if scheme == "unix" {
		// t.TempDir() can exceed the 104-byte Unix socket path limit on macOS.
		dir, err := os.MkdirTemp("/tmp", "nexus-test-")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		network, address = "unix", filepath.Join(dir, "silent.sock")
	}
	l, err := net.Listen(network, address)
	require.NoError(t, err)
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		if conn, err := l.Accept(); err == nil {
			accepted <- conn
		}
	}()

	url := fmt.Sprintf("%s://%s/", scheme, l.Addr())
	if scheme == "unix" {
		url = fmt.Sprintf("unix://%s", l.Addr())
	}
	cfg := client.Config{
		Realm:           testRealm,
		ResponseTimeout: clientResponseTimeout,
	}
	if scheme == "wss" || scheme == "tcps" {
		cfg.TlsCfg = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errChan := make(chan error, 1)
	go func() {
		_, err := client.ConnectNet(ctx, url, cfg)
		errChan <- err
	}()

	// Cancel only once the connect is under way.
	select {
	case conn := <-accepted:
		defer conn.Close()
	case <-time.After(clientResponseTimeout):
		require.FailNow(t, "connection was never accepted")
	}
	cancel()

	select {
	case err := <-errChan:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(clientResponseTimeout):
		require.FailNow(t, "ConnectNet did not return after its context was canceled")
	}
}
