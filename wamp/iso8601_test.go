package wamp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestISO8601(t *testing.T) {
	date := time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
	require.Equal(t, "2009-11-10T23:00:00Z", ISO8601(date))

	pst, _ := time.LoadLocation("America/Los_Angeles")
	date = time.Date(2009, time.November, 10, 23, 0, 0, 0, pst)
	require.Equal(t, "2009-11-10T23:00:00-0800", ISO8601(date))

	mos, _ := time.LoadLocation("Europe/Moscow")
	date = time.Date(2009, time.November, 10, 23, 0, 0, 0, mos)
	require.Equal(t, "2009-11-10T23:00:00+0300", ISO8601(date))

	require.GreaterOrEqual(t, len(NowISO8601()), 20)
}
