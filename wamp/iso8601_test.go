package wamp

import (
	"testing"
	"time"
)

func TestISO8601(t *testing.T) {
	date := time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
	if ISO8601(date) != "2009-11-10T23:00:00Z" {
		t.Fatal("Incorrect ISO8601 string")
	}

	pst, _ := time.LoadLocation("America/Los_Angeles")
	date = time.Date(2009, time.November, 10, 23, 0, 0, 0, pst)
	if ISO8601(date) != "2009-11-10T23:00:00-0800" {
		t.Fatal("Incorrect ISO8601 string")
	}

	mos, _ := time.LoadLocation("Europe/Moscow")
	date = time.Date(2009, time.November, 10, 23, 0, 0, 0, mos)
	if ISO8601(date) != "2009-11-10T23:00:00+0300" {
		t.Fatal("Incorrect ISO8601 string")
	}

	if len(NowISO8601()) < 20 {
		t.Fatal("Bad response from NowISO8601")
	}
}
