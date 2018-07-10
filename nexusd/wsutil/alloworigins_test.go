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

func TestAllowOriginsWithPorts(t *testing.T) {
	r, err := http.NewRequest("GET", "http://nowhere.net:", nil)
	if err != nil {
		t.Fatal("Failed to create request:", err)
	}

	// Test single port
	check, err := AllowOrigins([]string{"*.somewhere.com:8080"})
	if err != nil {
		t.Error(err)
	}
	allowed := "http://happy.somewhere.com:8080"
	r.Header.Set("Origin", allowed)
	if !check(r) {
		t.Error("Should have allowed:", allowed)
	}

	denied := "http://happy.somewhere.com:8081"
	r.Header.Set("Origin", denied)
	if check(r) {
		t.Error("Should have denied:", denied)
	}

	// Test multiple ports
	check, err = AllowOrigins([]string{
		"*.somewhere.com:8080",
		"*.somewhere.com:8905",
		"*.somewhere.com:8908",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, allowed := range []string{"http://larry.somewhere.com:8080",
		"http://moe.somewhere.com:8905", "http://curley.somewhere.com:8908"} {
		r.Header.Set("Origin", allowed)
		if !check(r) {
			t.Error("Should have allowed:", allowed)
		}
	}
	for _, denied := range []string{"http://larry.somewhere.com:9080",
		"http://moe.somewhere.com:8906", "http://curley.somewhere.com:8708"} {
		r.Header.Set("Origin", denied)
		if check(r) {
			t.Error("Should have denied:", denied)
		}
	}

	// Test any port
	check, err = AllowOrigins([]string{"*.somewhere.com:*"})
	if err != nil {
		t.Error(err)
	}
	allowed = "http://happy.somewhere.com:1313"
	r.Header.Set("Origin", allowed)
	if !check(r) {
		t.Error("Should have allowed:", allowed)
	}
}
