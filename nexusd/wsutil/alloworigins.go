/*
Package wsutil provides websocket server utilities.

*/
package wsutil

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

// AllowOriginsGlob returns a function, suitable for setting
// WebsocketServer.Upgrader.CheckOrigin, that returns true if: the origin is
// not set, is equal to the request host, or matches one of the allowed
// patterns.  A pattern is in the form of a shell glob as described here:
// https://golang.org/pkg/path/filepath/#Match
//
// Origins with Ports
//
// To allow origins that have a port number, specify the port as part of the
// origin pattern, or use a wildcard to match the ports.  See examples.
//
// Allow a specific port, by specifying the expected port number:
//  check, err := AllowOrigins([]string{"*.somewhere.com:8080"})
//  if err != nil {
//      log.Fatal(err)
//  }
//  wsServer.Upgrader.CheckOrigin = check
//
// Allow individual ports, by specifying each port:
//  check, err := AllowOrigins([]string{
//      "*.somewhere.com:8080",
//      "*.somewhere.com:8905",
//      "*.somewhere.com:8908",
//  })
//  if err != nil {
//      log.Fatal(err)
//  }
//  wsServer.Upgrader.CheckOrigin = check
//
// Allow any port, by specifying a wildcard. Be sure to include the colon ":"
// so that the pattern does not match a longer origin.  Without the ":" this
// example would also match "x.somewhere.comics.net"
//  check, err := AllowOrigins([]string{"*.somewhere.com:*"})
//  if err != nil {
//      log.Fatal(err)
//  }
//  wsServer.Upgrader.CheckOrigin = check
func AllowOrigins(origins []string) (func(r *http.Request) bool, error) {
	if len(origins) == 0 {
		return nil, nil
	}
	var exacts, globs []string
	for _, o := range origins {
		// If allowing any origins, then return simple "true" function.
		if o == "*" {
			return func(r *http.Request) bool { return true }, nil
		}

		// Do exact matching whenever possible, since it is more efficient.
		if strings.ContainsAny(o, "*?[]^") {
			if _, err := filepath.Match(o, o); err != nil {
				return nil, fmt.Errorf("error allowing origin, %s: %s", err, o)
			}
			globs = append(globs, strings.ToLower(o))
		} else {
			exacts = append(exacts, o)
		}
	}
	return func(r *http.Request) bool {
		return checkOrigin(exacts, globs, r)
	}, nil
}

// checkOrigin returns true if the origin is not set, is equal to the
// request host, or matches one of the allowed patterns.
func checkOrigin(exacts, globs []string, r *http.Request) bool {
	origin := r.Header["Origin"]
	if len(origin) == 0 {
		return true
	}
	u, err := url.Parse(origin[0])
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}

	for i := range exacts {
		if strings.EqualFold(u.Host, exacts[i]) {
			return true
		}
	}
	if len(globs) != 0 {
		host := strings.ToLower(u.Host)
		for i := range globs {
			if ok, _ := filepath.Match(globs[i], host); ok {
				return true
			}
		}
	}
	return false
}
