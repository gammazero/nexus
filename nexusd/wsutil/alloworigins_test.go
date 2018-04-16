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
	r, err := http.NewRequest("GET", "http://nowhere.net", nil)
	if err != nil {
		t.Fatal("Failed to create request:", err)
	}
	for _, allowed := range []string{"http://foo.bar.com",
		"http://snafoo.bar.com", "http://a.b.c.baz.bar.net",
		"http://hello.世界", "http://hello.世界.X.com",
		"https://sevastopol.seegson.com"} {
		r.Header.Set("Origin", allowed)
		if !check(r) {
			t.Error("Should have allowed:", allowed)
		}
	}
	for _, denied := range []string{"http://cat.bar.com",
		"http://a.bar.net.com", "http://hello.世界.X.nex"} {
		r.Header.Set("Origin", denied)
		if check(r) {
			t.Error("Should have denied:", denied)
		}
	}
	if _, err = AllowOrigins([]string{"foo.bar.co["}); err == nil {
		t.Fatal("Expected error")
	}
}
