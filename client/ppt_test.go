package client //nolint:testpackage

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/wamp"
)

// TestPPTPackUnpackRoundTrip pins the round-trip semantics of
// packPPTPayload/unpackPPTPayload across each supported PPT serializer plus
// the "native" mode. Native must not envelope the payload at all (so it
// survives a typed-Go in-process transport AND any wire serializer); the
// encoded modes must produce a single binary blob in args[0] with nil
// ArgumentsKw, and the inverse must reconstruct the original (args, kwargs).
func TestPPTPackUnpackRoundTrip(t *testing.T) {
	cases := []struct {
		name       string
		serializer string // "" / "native" => native; otherwise envelope mode
	}{
		{"native_implicit", ""},
		{"native_explicit", "native"},
		{"msgpack_envelope", "msgpack"},
		{"cbor_envelope", "cbor"},
		{"json_envelope", "json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Plain string/bool only: different envelope serializers widen int64→uint64
			// inconsistently when round-tripping through interface{}. The PPT contract
			// here is "payload survives unmodified", which we verify with stable types.
			origArgs := wamp.List{"opaque", "second", true}
			origKwargs := wamp.Dict{"k1": "v1", "k2": "v2"}

			opts := wamp.Dict{wamp.OptPPTScheme: "mqtt"}
			if tc.serializer != "" {
				opts[wamp.OptPPTSerializer] = tc.serializer
			}

			gotArgs, gotKwargs, err := packPPTPayload(opts, origArgs, origKwargs)
			require.NoError(t, err)

			isNative := tc.serializer == "" || tc.serializer == "native"
			if isNative {
				require.Equal(t, origArgs, gotArgs, "native must pass args through unchanged")
				require.Equal(t, origKwargs, gotKwargs, "native must pass kwargs through unchanged")
			} else {
				require.Len(t, gotArgs, 1, "encoded PPT must produce a single-element args")
				_, isBytes := gotArgs[0].([]byte)
				require.True(t, isBytes, "encoded PPT payload must be []byte, got %T", gotArgs[0])
				require.Nil(t, gotKwargs, "encoded PPT must clear ArgumentsKw")
			}

			details := wamp.Dict{wamp.OptPPTScheme: "mqtt"}
			if tc.serializer != "" {
				details[wamp.OptPPTSerializer] = tc.serializer
			}

			recoveredArgs, recoveredKwargs, err := unpackPPTPayload(details, gotArgs, gotKwargs)
			require.NoError(t, err)
			require.Equal(t, origArgs, recoveredArgs)
			require.Equal(t, origKwargs, recoveredKwargs)
		})
	}
}

// TestPPTUnpackJSONWireBase64 covers the wire-quirk path: over a JSON wire
// transport, []byte arrives at the receiver as a base64-encoded Go string
// (ugorji's codec.JsonHandle behavior when decoding into interface{}).
// pptPayloadBytes must transparently base64-decode it so unpackPPTPayload can
// deserialize the envelope. Also accepts the leading-NUL convention used by
// serialize.BinaryData.
func TestPPTUnpackJSONWireBase64(t *testing.T) {
	origArgs := wamp.List{"opaque", "second"}
	origKwargs := wamp.Dict{"k": "v"}

	opts := wamp.Dict{
		wamp.OptPPTScheme:     "mqtt",
		wamp.OptPPTSerializer: "msgpack",
	}
	packed, _, err := packPPTPayload(opts, origArgs, origKwargs)
	require.NoError(t, err)
	bin, ok := packed[0].([]byte)
	require.True(t, ok)

	// Plain base64 (what ugorji JSON does).
	asString := base64.StdEncoding.EncodeToString(bin)
	gotArgs, gotKwargs, err := unpackPPTPayload(opts, wamp.List{asString}, nil)
	require.NoError(t, err)
	require.Equal(t, origArgs, gotArgs)
	require.Equal(t, origKwargs, gotKwargs)

	// Leading-NUL form (WAMP spec §3.2.1 BinaryData convention).
	asNULString := "\x00" + asString
	gotArgs, gotKwargs, err = unpackPPTPayload(opts, wamp.List{asNULString}, nil)
	require.NoError(t, err)
	require.Equal(t, origArgs, gotArgs)
	require.Equal(t, origKwargs, gotKwargs)
}

// TestE2EEUnpackJSONWireBase64 covers the same JSON wire quirk as
// TestPPTUnpackJSONWireBase64 for end-to-end encrypted payloads.
func TestE2EEUnpackJSONWireBase64(t *testing.T) {
	origArgs := wamp.List{"secret"}
	origKwargs := wamp.Dict{"k": "v"}

	opts := wamp.Dict{
		wamp.OptPPTScheme:     WampPPTScheme,
		wamp.OptPPTSerializer: "cbor",
	}
	packed, err := packE2EEPayload(opts, origArgs, origKwargs)
	require.NoError(t, err)
	bin, ok := packed[0].([]byte)
	require.True(t, ok)

	asString := base64.StdEncoding.EncodeToString(bin)
	for _, s := range []string{asString, "\x00" + asString} {
		gotArgs, gotKwargs, err := unpackE2EEPayload(opts, wamp.List{s})
		require.NoError(t, err)
		require.Equal(t, origArgs, gotArgs)
		require.Equal(t, origKwargs, gotKwargs)
	}
}

// TestPPTUnpackRejectsMalformedPayload checks that payload details from a
// remote peer that do not make sense are reported as errors instead of
// panicking or being passed to the application.
func TestPPTUnpackRejectsMalformedPayload(t *testing.T) {
	cases := []struct {
		name    string
		details wamp.Dict
		args    wamp.List
		wantErr error
	}{
		{
			name:    "e2ee missing serializer",
			details: wamp.Dict{wamp.OptPPTScheme: WampPPTScheme},
			args:    wamp.List{[]byte{1}},
			wantErr: ErrPPTSerializerInvalid,
		},
		{
			name: "e2ee non-string serializer",
			details: wamp.Dict{
				wamp.OptPPTScheme:     WampPPTScheme,
				wamp.OptPPTSerializer: 2,
			},
			args:    wamp.List{[]byte{1}},
			wantErr: ErrPPTSerializerInvalid,
		},
		{
			name: "e2ee no arguments",
			details: wamp.Dict{
				wamp.OptPPTScheme:     WampPPTScheme,
				wamp.OptPPTSerializer: "cbor",
			},
			wantErr: ErrSerialization,
		},
		{
			name: "e2ee payload not binary",
			details: wamp.Dict{
				wamp.OptPPTScheme:     WampPPTScheme,
				wamp.OptPPTSerializer: "cbor",
			},
			args:    wamp.List{int64(7)},
			wantErr: ErrSerialization,
		},
		{
			name: "e2ee serializer not allowed",
			details: wamp.Dict{
				wamp.OptPPTScheme:     WampPPTScheme,
				wamp.OptPPTSerializer: "json",
			},
			args:    wamp.List{[]byte{1}},
			wantErr: ErrPPTSerializerInvalid,
		},
		{
			name: "e2ee payload not cbor",
			details: wamp.Dict{
				wamp.OptPPTScheme:     WampPPTScheme,
				wamp.OptPPTSerializer: "cbor",
			},
			args:    wamp.List{[]byte{0xff, 0xff, 0xff, 0xff}},
			wantErr: ErrSerialization,
		},
		{
			name: "ppt non-string serializer",
			details: wamp.Dict{
				wamp.OptPPTScheme:     MqttPPTScheme,
				wamp.OptPPTSerializer: 2,
			},
			args:    wamp.List{[]byte{1}},
			wantErr: ErrPPTSerializerInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			require.NotPanics(t, func() {
				if tc.details[wamp.OptPPTScheme] == WampPPTScheme {
					_, _, err = unpackE2EEPayload(tc.details, tc.args)
				} else {
					_, _, err = unpackPPTPayload(tc.details, tc.args, nil)
				}
			})
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// TestE2EEPackRejectsMissingSerializer checks that a missing ppt_serializer is
// reported as an error instead of panicking.
func TestE2EEPackRejectsMissingSerializer(t *testing.T) {
	opts := wamp.Dict{wamp.OptPPTScheme: WampPPTScheme}
	var err error
	require.NotPanics(t, func() {
		_, err = packE2EEPayload(opts, wamp.List{"secret"}, nil)
	})
	require.ErrorIs(t, err, ErrPPTSerializerInvalid)
}

// TestE2EEPackRejectsUnsupportedSerializer checks that packing an end-to-end
// encrypted payload with a serializer other than cbor is reported as an
// error.
func TestE2EEPackRejectsUnsupportedSerializer(t *testing.T) {
	for _, serializer := range []string{"json", "msgpack", "x_unknown", ""} {
		opts := wamp.Dict{
			wamp.OptPPTScheme:     WampPPTScheme,
			wamp.OptPPTSerializer: serializer,
		}
		packed, err := packE2EEPayload(opts, wamp.List{"secret"}, nil)
		require.ErrorIsf(t, err, ErrPPTSerializerInvalid, "serializer %q", serializer)
		require.Nil(t, packed)
	}
}
