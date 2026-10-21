package client

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// TestPackE2EEPayloadCBORRoundTrip pins the End-to-End-Encrypted
// payload pack/unpack helpers (sister of the PPT helpers tested in
// export_ppt_test.go). E2EE is hard-coded to the CBOR serializer
// (only entry in E2eeSerializers); this test verifies the wrap-bytes-
// then-unwrap-bytes round-trip.
func TestPackE2EEPayloadCBORRoundTrip(t *testing.T) {
	origArgs := wamp.List{"opaque", "second", true}
	origKwargs := wamp.Dict{"k1": "v1", "k2": "v2"}

	opts := wamp.Dict{wamp.OptPPTSerializer: "cbor"}
	packed, err := packE2EEPayload(opts, origArgs, origKwargs)
	require.NoError(t, err)
	require.Len(t, packed, 1, "E2EE pack must produce a single-element args list")
	bin, ok := packed[0].([]byte)
	require.True(t, ok, "E2EE pack must produce []byte payload, got %T", packed[0])
	require.NotEmpty(t, bin)

	details := wamp.Dict{wamp.OptPPTSerializer: "cbor"}
	gotArgs, gotKwargs, err := unpackE2EEPayload(details, packed)
	require.NoError(t, err)
	require.Equal(t, origArgs, gotArgs)
	require.Equal(t, origKwargs, gotKwargs)
}

// TestPackE2EEPayloadInvalidSerializer pins the error path: an
// unsupported ppt_serializer value (anything not in the
// E2eeSerializers whitelist) returns ErrPPTSerializerInvalid.
func TestPackE2EEPayloadInvalidSerializer(t *testing.T) {
	cases := []struct {
		name       string
		serializer string
	}{
		{"json_not_allowed_for_e2ee", "json"},
		{"msgpack_not_allowed_for_e2ee", "msgpack"},
		{"unknown_serializer", "x_unknown"},
		{"empty_string", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := wamp.Dict{wamp.OptPPTSerializer: tc.serializer}
			packed, err := packE2EEPayload(opts, wamp.List{"x"}, nil)
			require.ErrorIs(t, err, ErrPPTSerializerInvalid)
			require.Nil(t, packed)
		})
	}
}

// TestUnpackE2EEPayloadInvalidSerializer pins the inverse error
// path on the receive side.
func TestUnpackE2EEPayloadInvalidSerializer(t *testing.T) {
	details := wamp.Dict{wamp.OptPPTSerializer: "json"}
	args, kwargs, err := unpackE2EEPayload(details, wamp.List{[]byte("doesn't matter")})
	require.ErrorIs(t, err, ErrPPTSerializerInvalid)
	require.Nil(t, args)
	require.Nil(t, kwargs)
}

// TestUnpackE2EEPayloadCorruptBytes pins the deserialization error
// path: a well-typed []byte payload that isn't valid CBOR returns
// ErrSerialization.
func TestUnpackE2EEPayloadCorruptBytes(t *testing.T) {
	// A few definitely-not-cbor bytes.
	garbage := []byte{0xff, 0xff, 0xff, 0xff}
	// Sanity: the CBOR serializer rejects this for our PassthruPayload type.
	var pp wamp.PassthruPayload
	require.Error(t, (&serialize.CBORSerializer{}).DeserializeDataItem(garbage, &pp),
		"sanity: garbage must not deserialize as PassthruPayload")

	details := wamp.Dict{wamp.OptPPTSerializer: "cbor"}
	args, kwargs, err := unpackE2EEPayload(details, wamp.List{garbage})
	require.ErrorIs(t, err, ErrSerialization)
	require.Nil(t, args)
	require.Nil(t, kwargs)
}
