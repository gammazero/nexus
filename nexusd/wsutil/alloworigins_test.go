package wsutil

import (
	"net/http"
	"testing"
)

func TestAllowOrigins(t *testing.T) {
	check, err := AllowOrigins([]string{"*foo.bAr.CoM", "*.bar.net",
		"Hello.世界", "Hello.世界.*.com", "Sevastopol.Seegson.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = AllowOrigins([]string{"foo.bar.co["}); err == nil {
		t.Fatal("Expected error")
	}

	r, err := http.NewRequest("GET", "http://nowhere.net", nil)
	if err != nil {
		t.Fatal("Failed to create request:", err)
	}
	for _, allowed := range []string{"http://foo.bar.com",
		"http://snafoo.bar.com", "https://a.b.c.baz.bar.net",
		"http://hello.世界", "http://hello.世界.X.com",
		"https://sevastopol.seegson.com", "http://nowhere.net/whatever"} {
		r.Header.Set("Origin", allowed)
		if !check(r) {
			t.Error("Should have allowed:", allowed)
		}
	}

	for _, denied := range []string{"http://cat.bar.com",
		"https://a.bar.net.com", "http://hello.世界.X.nex"} {
		r.Header.Set("Origin", denied)
		if check(r) {
			t.Error("Should have denied:", denied)
		}
	}

	// Check allow all.
	check, err = AllowOrigins([]string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, allowed := range []string{"http://foo.bar.com",
		"https://o.fortuna.imperatrix.mundi", "http://a.???.bb.??.net"} {
		if !check(r) {
			t.Error("Should have allowed:", allowed)
		}
	}

}
