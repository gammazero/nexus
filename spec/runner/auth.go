package runner

import (
	"errors"
	"fmt"
	"time"

	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/gammazero/nexus/v3/wamp"
	"github.com/gammazero/nexus/v3/wamp/crsign"
)

// ComputeSpec is a non-wire side-step that derives a value from
// captures and stores it back into captures. Currently only
// `wampcra_sign` is implemented; future helpers (e.g. cryptosign,
// hex/base64 transforms) can extend this struct.
type ComputeSpec struct {
	WAMPCRASign *WAMPCRASignSpec `yaml:"wampcra_sign,omitempty"`
}

// WAMPCRASignSpec computes an HMAC-SHA256 signature over a previously
// captured challenge string, using the given secret. The result
// (base64-encoded) lands in captures under `Capture` so a later
// `send: [5, "{{$signature}}", {}]` AUTHENTICATE step can use it.
//
// Example:
//
//	steps:
//	  - expect: [4, "wampcra", {challenge: "{{$challenge:string}}"}]
//	  - compute:
//	      wampcra_sign:
//	        challenge: "{{$challenge}}"
//	        secret: "alice-secret"
//	        capture: signature
//	  - send: [5, "{{$signature}}", {}]
type WAMPCRASignSpec struct {
	// Challenge is the challenge string emitted by the router in
	// CHALLENGE.Extra.challenge. Use a placeholder like "{{$challenge}}"
	// to pull a previously captured value.
	Challenge string `yaml:"challenge"`
	// Secret is the user's WAMP-CRA secret as bytes (utf-8 string).
	Secret string `yaml:"secret"`
	// Capture is the placeholder name to store the computed signature
	// under. The signature is the base64 HMAC-SHA256 string.
	Capture string `yaml:"capture"`
}

// runCompute executes a Compute step, resolving placeholders in its
// inputs against captures and writing the output back.
func runCompute(spec *ComputeSpec, captures map[string]any) error {
	if spec == nil {
		return errors.New("nil compute spec")
	}
	if spec.WAMPCRASign != nil {
		s := spec.WAMPCRASign
		if s.Capture == "" {
			return errors.New("wampcra_sign requires `capture` name")
		}
		challenge, err := resolveStringPlaceholder(s.Challenge, captures)
		if err != nil {
			return fmt.Errorf("wampcra_sign challenge: %w", err)
		}
		captures[s.Capture] = crsign.SignChallenge(challenge, []byte(s.Secret))
		return nil
	}
	return errors.New("compute step has no operation set")
}

// resolveStringPlaceholder yields s as-is, OR if s is a "{{$name}}"
// placeholder, the captured value (must be a string).
func resolveStringPlaceholder(s string, captures map[string]any) (string, error) {
	if !isPlaceholder(s) {
		return s, nil
	}
	body := s[3 : len(s)-2] // strip {{$ and }}
	name := body
	if i := indexByte(body, ':'); i != -1 {
		name = body[:i]
	}
	val, ok := captures[name]
	if !ok {
		return "", fmt.Errorf("placeholder %q references uncaptured name", s)
	}
	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("placeholder %q resolved to non-string %T", s, val)
	}
	return str, nil
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// AuthSpec is the on-disk YAML auth declaration for a plan. All fields
// are optional. If the whole block is absent (Auth==nil), the runner
// uses the legacy default (anonymous-only, role="anonymous").
//
// Example:
//
//	auth:
//	  anonymous: { role: anonymous }
//	  ticket:
//	    users:
//	      tester: { ticket: "ticket-X-1234", role: "user" }
//	  wampcra:
//	    users:
//	      jdoe: { secret: "squeemishosafradge", role: "user" }
type AuthSpec struct {
	Anonymous *AnonymousSpec `yaml:"anonymous,omitempty"`
	Ticket    *TicketSpec    `yaml:"ticket,omitempty"`
	WAMPCRA   *WAMPCRASpec   `yaml:"wampcra,omitempty"`
}

// AnonymousSpec configures anonymous auth. Empty role defaults to "anonymous".
type AnonymousSpec struct {
	Role string `yaml:"role,omitempty"`
}

// TicketSpec configures ticket-based dynamic CR auth with a static
// authid → ticket+role map (see test/spec_auth_ticket_test.go for the
// runtime semantics).
type TicketSpec struct {
	Users map[string]TicketUser `yaml:"users"`
}

type TicketUser struct {
	Ticket string `yaml:"ticket"`
	Role   string `yaml:"role"`
}

// WAMPCRASpec configures WAMP-CRA auth with a static authid → secret+role map.
type WAMPCRASpec struct {
	Users map[string]WAMPCRAUser `yaml:"users"`
}

type WAMPCRAUser struct {
	Secret string `yaml:"secret"`
	Role   string `yaml:"role"`
}

// staticKeyStore implements auth.KeyStore over an in-memory user map
// loaded from a plan's AuthSpec. It supports both ticket and wampcra
// authmethods on the same authid, picking the right key by method.
type staticKeyStore struct {
	tickets  map[string]TicketUser  // authid → ticket user
	wampcras map[string]WAMPCRAUser // authid → wampcra user
}

func (ks *staticKeyStore) AuthKey(authid, authmethod string) ([]byte, error) {
	switch authmethod {
	case "ticket":
		u, ok := ks.tickets[authid]
		if !ok {
			return nil, errors.New("no ticket user: " + authid)
		}
		return []byte(u.Ticket), nil
	case "wampcra":
		u, ok := ks.wampcras[authid]
		if !ok {
			return nil, errors.New("no wampcra user: " + authid)
		}
		return []byte(u.Secret), nil
	}
	return nil, errors.New("unsupported authmethod: " + authmethod)
}

func (ks *staticKeyStore) AuthRole(authid string) (string, error) {
	if u, ok := ks.tickets[authid]; ok {
		return u.Role, nil
	}
	if u, ok := ks.wampcras[authid]; ok {
		return u.Role, nil
	}
	return "", errors.New("no such user: " + authid)
}

func (ks *staticKeyStore) PasswordInfo(string) (string, int, int) { return "", 0, 0 }
func (ks *staticKeyStore) Provider() string                       { return "spec-runner-static" }

// buildAuthenticators converts an AuthSpec into the list of
// authenticators to wire into the per-plan router. Returns whether
// anonymous auth should be enabled on the realm and the role to assign
// to anonymous sessions (empty = use realm's default).
func buildAuthenticators(spec *AuthSpec) (anonOn bool, anonRole string, auths []auth.Authenticator) {
	if spec == nil {
		// Legacy default: anonymous auth, anonymous role.
		return true, "", nil
	}

	ks := &staticKeyStore{
		tickets:  map[string]TicketUser{},
		wampcras: map[string]WAMPCRAUser{},
	}
	hasTicketUsers := spec.Ticket != nil && len(spec.Ticket.Users) > 0
	hasWAMPCRAUsers := spec.WAMPCRA != nil && len(spec.WAMPCRA.Users) > 0

	if hasTicketUsers {
		for id, u := range spec.Ticket.Users {
			ks.tickets[id] = u
		}
		auths = append(auths, auth.NewTicketAuthenticator(ks, time.Second))
	}
	if hasWAMPCRAUsers {
		for id, u := range spec.WAMPCRA.Users {
			ks.wampcras[id] = u
		}
		auths = append(auths, auth.NewCRAuthenticator(ks, time.Second))
	}
	if spec.Anonymous != nil {
		anonOn = true
		anonRole = spec.Anonymous.Role
	}
	return anonOn, anonRole, auths
}

// suppress unused warnings for wamp import if these helpers grow.
var _ = wamp.URI("")
