package filtertool

import (
	"log"
	"os"
	"testing"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/wamp"
)

const (
	testRealm = "nexus.test"
)

var logger stdlog.StdLog

func init() {
	logger = log.New(os.Stdout, "", log.LstdFlags)
}

func getTestRouter(realmConfig *router.RealmConfig) (router.Router, error) {
	config := &router.Config{
		RealmConfigs: []*router.RealmConfig{realmConfig},
	}
	return router.NewRouter(config, logger)
}

func newTestClient(r router.Router, helloDetails wamp.Dict) (*client.Client, error) {
	cfg := client.Config{
		Realm:        testRealm,
		HelloDetails: helloDetails,
		Logger:       logger,
		Debug:        false,
	}
	return client.ConnectLocal(r, cfg)
}

func TestFilter(t *testing.T) {
	defer leaktest.Check(t)()

	realmConfig := &router.RealmConfig{
		URI:       wamp.URI(testRealm),
		StrictURI: true,
	}
	r, err := getTestRouter(realmConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	client1, err := newTestClient(r, wamp.Dict{"org_id": "mycorp"})
	if err != nil {
		t.Fatal(err)
	}
	defer client1.Close()

	monitor, err := newTestClient(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()

	ft, err := New(monitor)
	if err != nil {
		t.Fatal(err)
	}

	const (
		filterMyCorp = "allow_mycorp"
		filterOthers = "allow_others"
	)

	allowMyCorp := func(sessDetails wamp.Dict) bool {
		org_id, _ := wamp.AsString(sessDetails["org_id"])
		return org_id == "mycorp"
	}
	ft.AddFilter(filterMyCorp, allowMyCorp)

	allowOthers := func(sessDetails wamp.Dict) bool {
		org_id, _ := wamp.AsString(sessDetails["org_id"])
		return org_id != "" && org_id != "mycorp"
	}
	ft.AddFilter(filterOthers, allowOthers)

	wl, ok := ft.Whitelist(filterMyCorp)
	if !ok {
		t.Fatal("did not have expected whitelist")
	}
	if len(wl) != 1 {
		t.Fatal("whitelist should have one ID")
	}
	if wl[0] != client1.ID() {
		t.Fatal("whitelist did not have expected ID")
	}

	wlOthers, ok := ft.Whitelist(filterOthers)
	if !ok {
		t.Fatal("did not have", filterOthers, "whitelist")
	}
	if len(wlOthers) != 0 {
		t.Fatal(filterOthers, "whitelist should be empty")
	}

	client2, err := newTestClient(r, wamp.Dict{"org_id": "other_corp"})
	if err != nil {
		t.Fatal(err)
	}
	defer client2.Close()
	<-ft.SessionEvent()

	wl, ok = ft.Whitelist(filterMyCorp)
	if !ok {
		t.Fatal("did not have expected whitelist")
	}
	if len(wl) != 1 {
		t.Fatal("whitelist should have one ID")
	}

	wlOthers, ok = ft.Whitelist(filterOthers)
	if !ok {
		t.Fatal("did not have", filterOthers, "whitelist")
	}
	if len(wlOthers) != 1 {
		t.Fatal(filterOthers, "whitelist should have 1 ID, has", len(wlOthers))
	}

	client3, err := newTestClient(r, wamp.Dict{"org_id": "mycorp"})
	if err != nil {
		t.Fatal(err)
	}
	defer client3.Close()
	<-ft.SessionEvent()

	wl, ok = ft.Whitelist(filterMyCorp)
	if !ok {
		t.Fatal("did not have expected whitelist")
	}
	if len(wl) != 2 {
		t.Fatal("whitelist should have one ID")
	}
	if wl[0] != client1.ID() && wl[1] != client1.ID() {
		t.Fatal("whitelist did not have expected ID")
	}
	if wl[1] != client3.ID() && wl[0] != client3.ID() {
		t.Fatal("whitelist did not have expected ID")
	}

	client3.Close()
	<-ft.SessionEvent()
	wl, ok = ft.Whitelist(filterMyCorp)
	if !ok {
		t.Fatal("did not have expected whitelist")
	}
	if len(wl) != 1 {
		t.Fatal(filterMyCorp, "whitelist should have had one ID")
	}
	if wl[0] != client1.ID() {
		t.Fatal("whitelist did not have expected ID")
	}

	wlOthers, ok = ft.Whitelist(filterOthers)
	if !ok {
		t.Fatal("did not have", filterOthers, "whitelist")
	}
	if len(wlOthers) != 1 {
		t.Fatal(filterOthers, "whitelist should have 1 ID, had", len(wlOthers))
	}

	client2.Close()
	<-ft.SessionEvent()

	wlOthers, ok = ft.Whitelist(filterOthers)
	if !ok {
		t.Fatal("did not have", filterOthers, "whitelist")
	}
	if len(wlOthers) != 0 {
		t.Fatal(filterOthers, "whitelist should be empty")
	}

	ft.RemoveFilter(filterOthers)
	if _, ok := ft.Whitelist(filterOthers); ok {
		t.Fatal("filter should no exist")
	}

	monitor.Close()
	client1.Close()
}
