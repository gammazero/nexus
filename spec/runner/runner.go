// Package runner is the YAML-driven WAMP spec-conformance harness.
//
// Each test plan is a YAML file under spec/plans/ describing a sequence of
// `send` / `expect` steps against a freshly-started nexus router. Messages
// are written in raw WAMP list form (e.g. [1, "realm1", {...}] for HELLO);
// the runner serializes them through a real serializer and exchanges them
// over a real (rawsocket TCP loopback) transport, so deviations in the
// wire format are caught.
//
// Placeholders in `expect` lists allow matching against router-assigned
// values (session IDs, request IDs, etc.). See matcher.go for syntax.
//
// This package is a skeleton: the 5 seed plans in spec/plans/basic/ are
// representative cases. Phase 1 expands coverage to ~150-200 plans across
// basic profile, advanced features, auth flows, and standard error URIs.
package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// Plan is the on-disk format of a YAML test plan.
type Plan struct {
	Name        string     `yaml:"name"`
	SpecSection string     `yaml:"spec_section"`
	Realm       string     `yaml:"realm"`
	Serializer  string     `yaml:"serializer"`
	Skip        string     `yaml:"skip"` // non-empty → t.Skip with this reason
	Peers       []PeerSpec `yaml:"peers,omitempty"`
	Auth        *AuthSpec  `yaml:"auth,omitempty"`
	Steps       []Step     `yaml:"steps"`
}

// PeerSpec declares a named peer that the runner dials before any step
// runs. Single-peer plans omit this; the runner creates one peer named
// "default" automatically. Multi-peer plans must list every peer here
// and reference them by name in each step's `peer:` field.
type PeerSpec struct {
	Name string `yaml:"name"`
	// Realm overrides the plan's top-level realm for this peer.
	// Useful for cross-realm dispatch tests; unset = plan realm.
	Realm string `yaml:"realm,omitempty"`
}

// Step is one send-or-expect operation in a plan.
type Step struct {
	// Peer names which dialed peer this step acts on. Required when
	// the plan declares multiple peers; defaults to "default" otherwise.
	// Supports {{$name}} placeholder substitution against captures, so
	// a step can target whichever peer was selected in a prior
	// FromAny step.
	Peer string `yaml:"peer,omitempty"`

	Send    []any        `yaml:"send,omitempty"`
	Expect  []any        `yaml:"expect,omitempty"`
	Compute *ComputeSpec `yaml:"compute,omitempty"`

	// FromAny lists peer names to wait on simultaneously for an
	// `expect`. The first peer whose Recv() yields a message wins; the
	// chosen peer's name is captured under CaptureSource so subsequent
	// steps can target it. Useful where the spec doesn't pin which
	// peer receives a given message (e.g. shared registration with
	// invoke=random). Mutually exclusive with Peer for the same step.
	FromAny       []string `yaml:"from_any,omitempty"`
	CaptureSource string   `yaml:"capture_source,omitempty"`

	// TimeoutMS overrides the default per-step timeout.
	TimeoutMS int `yaml:"timeout_ms,omitempty"`

	// Note is a free-form annotation, ignored by the runner.
	Note string `yaml:"note,omitempty"`
}

// LoadPlan reads a YAML file into a Plan.
func LoadPlan(path string) (*Plan, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // plan paths come from walking spec/plans
	if err != nil {
		return nil, err
	}
	var p Plan
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if p.Serializer == "" {
		p.Serializer = "json"
	}
	if p.Realm == "" {
		p.Realm = "realm1"
	}
	return &p, nil
}

// RunPlan executes a single plan against a freshly-started router. It is
// the main entry point used by tests under spec/runner/.
func RunPlan(t *testing.T, p *Plan) {
	t.Helper()
	if p.Skip != "" {
		t.Skip(p.Skip)
	}

	ser, serID, err := pickSerializer(p.Serializer)
	require.NoErrorf(t, err, "plan %s", p.Name)

	// Build the peer set up front. Single-peer plans declare nothing
	// and get an implicit "default" peer; multi-peer plans must list
	// every peer here. All peers dial the same router.
	peerSpecs := p.Peers
	if len(peerSpecs) == 0 {
		peerSpecs = []PeerSpec{{Name: "default"}}
	}

	r := startRouter(t, p.Realm, p.Auth)
	addr := serveRawSocket(t, r)

	peers := make(map[string]wamp.Peer, len(peerSpecs))
	for _, ps := range peerSpecs {
		if ps.Name == "" {
			t.Fatalf("plan %s: peer with empty name", p.Name)
		}
		if _, dup := peers[ps.Name]; dup {
			t.Fatalf("plan %s: duplicate peer name %q", p.Name, ps.Name)
		}
		peer := dialPeer(t, addr, serID)
		t.Cleanup(func() { peer.Close() })
		peers[ps.Name] = peer
	}

	captures := map[string]any{}

	for i, step := range p.Steps {
		stepName := fmt.Sprintf("step %d", i+1)

		// Compute step: derive a value from captures and store it back.
		// No peer involvement.
		if step.Compute != nil {
			require.NoErrorf(t, runCompute(step.Compute, captures),
				"%s: compute failed", stepName)
			continue
		}

		// FromAny is its own dispatch path — no single peer is named up
		// front. Handled before the peer-resolution block below.
		if len(step.FromAny) > 0 {
			require.NotNilf(t, step.Expect,
				"%s: from_any requires `expect`", stepName)
			require.Emptyf(t, step.Peer,
				"%s: from_any and peer are mutually exclusive", stepName)
			timeout := time.Duration(step.TimeoutMS) * time.Millisecond
			if timeout == 0 {
				timeout = 2 * time.Second
			}
			source, actual, err := doExpectAny(peers, step.FromAny, ser, timeout)
			require.NoErrorf(t, err, "%s [from_any=%v]: receive failed",
				stepName, step.FromAny)
			require.NoErrorf(t, matchList(step.Expect, actual, captures),
				"%s [from_any=%v, source=%s]: expected %v, got %v",
				stepName, step.FromAny, source, step.Expect, actual)
			if step.CaptureSource != "" {
				captures[step.CaptureSource] = source
			}
			continue
		}

		// Resolve which peer the step targets. Supports placeholder
		// substitution so `peer: "{{$chosen}}"` can pick up whichever
		// peer a prior from_any step captured.
		peerName, err := resolvePeerName(step.Peer, peers, captures)
		require.NoErrorf(t, err, "%s", stepName)
		peer := peers[peerName]

		switch {
		case step.Send != nil:
			subbed, err := substituteCaptures(step.Send, captures)
			require.NoErrorf(t, err, "%s [peer=%s]: substitute placeholders",
				stepName, peerName)
			require.NoErrorf(t, doSend(peer, subbed.([]any), ser),
				"%s [peer=%s]: send failed", stepName, peerName)

		case step.Expect != nil:
			timeout := time.Duration(step.TimeoutMS) * time.Millisecond
			if timeout == 0 {
				timeout = 2 * time.Second
			}
			actual, err := doExpect(peer, ser, timeout)
			require.NoErrorf(t, err, "%s [peer=%s]: receive failed", stepName, peerName)
			require.NoErrorf(t, matchList(step.Expect, actual, captures),
				"%s [peer=%s]: expected %v, got %v",
				stepName, peerName, step.Expect, actual)

		default:
			t.Fatalf("%s: must specify either `send` or `expect`", stepName)
		}
	}
}

// resolvePeerName picks the peer for a step, with three rules:
//
//  1. A literal name → that peer must exist.
//  2. A {{$name}} placeholder → looked up in captures; must be a string
//     and reference an existing peer.
//  3. Empty + single-peer plan → that peer is implied.
//  4. Empty + multi-peer plan → error (caller must specify).
func resolvePeerName(raw string, peers map[string]wamp.Peer, captures map[string]any) (string, error) {
	if raw == "" {
		if len(peers) == 1 {
			for name := range peers {
				return name, nil
			}
		}
		return "", fmt.Errorf("multi-peer plan requires `peer:` on every step")
	}
	name := raw
	if isPlaceholder(name) {
		body := strings.TrimSuffix(strings.TrimPrefix(name, "{{$"), "}}")
		capName, _, _ := strings.Cut(body, ":")
		val, ok := captures[capName]
		if !ok {
			return "", fmt.Errorf("peer placeholder %q references uncaptured name", raw)
		}
		s, ok := val.(string)
		if !ok {
			return "", fmt.Errorf("peer placeholder %q resolved to non-string %T", raw, val)
		}
		name = s
	}
	if _, ok := peers[name]; !ok {
		return "", fmt.Errorf("unknown peer %q", name)
	}
	return name, nil
}

// doExpectAny waits for the next message to arrive on any of the named
// peers. Returns the name of the peer that delivered, the deserialized
// message list, or an error on timeout / closed peer / unknown name.
func doExpectAny(peers map[string]wamp.Peer, names []string, ser serialize.Serializer, timeout time.Duration) (string, []any, error) {
	cases := make([]reflect.SelectCase, 0, len(names)+1)
	for _, n := range names {
		p, ok := peers[n]
		if !ok {
			return "", nil, fmt.Errorf("from_any references unknown peer %q", n)
		}
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(p.Recv()),
		})
	}
	cases = append(cases, reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(time.After(timeout)),
	})

	chosen, value, recvOK := reflect.Select(cases)
	if chosen == len(names) {
		return "", nil, fmt.Errorf("timeout waiting for message on from_any peers %v", names)
	}
	if !recvOK {
		return "", nil, fmt.Errorf("peer %q closed", names[chosen])
	}
	msg, ok := value.Interface().(wamp.Message)
	if !ok {
		return "", nil, fmt.Errorf("non-message value on peer %q channel: %T",
			names[chosen], value.Interface())
	}
	bytes, err := ser.Serialize(msg)
	if err != nil {
		return "", nil, fmt.Errorf("re-serialize received: %w", err)
	}
	list, err := jsonBytesToList(bytes)
	if err != nil {
		return "", nil, err
	}
	return names[chosen], list, nil
}

// RunDir walks a directory of YAML plans and runs each as a subtest. The
// subtest name is derived from the file path so test selection works
// naturally with `go test -run`.
func RunDir(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	require.NoError(t, err)
	if len(matches) == 0 {
		t.Skipf("no plans found in %s", dir)
	}
	for _, path := range matches {
		path := path
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			plan, err := LoadPlan(path)
			require.NoError(t, err)
			RunPlan(t, plan)
		})
	}
}

func pickSerializer(name string) (serialize.Serializer, serialize.Serialization, error) {
	switch name {
	case "json", "":
		return &serialize.JSONSerializer{}, serialize.JSON, nil
	case "msgpack":
		return &serialize.MessagePackSerializer{}, serialize.MSGPACK, nil
	case "cbor":
		return &serialize.CBORSerializer{}, serialize.CBOR, nil
	default:
		return nil, 0, fmt.Errorf("unknown serializer %q", name)
	}
}

// doSend converts the YAML list form to a wamp.Message and writes it to
// the peer. The first element is interpreted as the message-type integer.
func doSend(peer wamp.Peer, list []any, ser serialize.Serializer) error {
	if len(list) == 0 {
		return fmt.Errorf("empty send list")
	}
	mt, err := asInt(list[0])
	if err != nil {
		return fmt.Errorf("first element must be message-type integer: %w", err)
	}

	// Round-trip through serializer so the test exercises the full
	// encode/decode path and the message struct is correctly populated.
	msg := wamp.NewMessage(wamp.MessageType(mt))
	if msg == nil {
		return fmt.Errorf("unsupported message type %d", mt)
	}

	jsonBytes, err := serializeListAsBytes(list)
	if err != nil {
		return err
	}
	deserialized, err := ser.Deserialize(jsonBytes)
	if err != nil {
		return fmt.Errorf("deserialize send list: %w", err)
	}

	select {
	case peer.Send() <- deserialized:
	case <-time.After(time.Second):
		return fmt.Errorf("send timed out")
	}
	return nil
}

func doExpect(peer wamp.Peer, ser serialize.Serializer, timeout time.Duration) ([]any, error) {
	select {
	case msg, ok := <-peer.Recv():
		if !ok {
			return nil, fmt.Errorf("peer closed")
		}
		// Re-serialize to bytes then to []any so the matcher operates on
		// the same shape as the YAML expected list.
		bytes, err := ser.Serialize(msg)
		if err != nil {
			return nil, fmt.Errorf("re-serialize received: %w", err)
		}
		return jsonBytesToList(bytes)
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for message")
	}
}
