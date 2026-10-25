package serialize_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

// cborCall returns a CBOR encoded CALL, [48, 1, {}, "x.y", [arg]], where arg
// is the CBOR encoding of the only argument.
func cborCall(arg ...byte) []byte {
	b := []byte{0x85, 0x18, 0x30, 0x01, 0xa0, 0x63, 'x', '.', 'y', 0x81}
	return append(b, arg...)
}

// TestCBORDecimalFraction checks that a CBOR decimal fraction (tag 4) is
// decoded to an exact Decimal, and encoded again unchanged, so that the
// router passes it on as it was sent.
func TestCBORDecimalFraction(t *testing.T) {
	cases := []struct {
		name string
		arg  []byte
		want serialize.Decimal
	}{
		// 4([-2, 27315])
		{"273.15", []byte{0xc4, 0x82, 0x21, 0x19, 0x6a, 0xb3}, serialize.Decimal{Mantissa: 27315, Exponent: -2}},
		// 4([-1, -15])
		{"-1.5", []byte{0xc4, 0x82, 0x20, 0x2e}, serialize.Decimal{Mantissa: -15, Exponent: -1}},
		// 4([3, 7])
		{"7000", []byte{0xc4, 0x82, 0x03, 0x07}, serialize.Decimal{Mantissa: 7, Exponent: 3}},
		// 4([-1, 1]), 0.1, which a float64 cannot hold exactly.
		{"0.1", []byte{0xc4, 0x82, 0x20, 0x01}, serialize.Decimal{Mantissa: 1, Exponent: -1}},
	}
	var s serialize.CBORSerializer
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := cborCall(tc.arg...)
			msg, err := s.Deserialize(data)
			require.NoError(t, err)
			call, ok := msg.(*wamp.Call)
			require.True(t, ok, "expected CALL, got %T", msg)
			require.Equal(t, tc.want, call.Arguments[0])

			out, err := s.Serialize(call)
			require.NoError(t, err)
			require.Equal(t, data, out, "decimal fraction not encoded unchanged")
		})
	}
}

// TestCBORDecimalFractionOutOfRange checks that a decimal fraction that a
// Decimal cannot hold exactly is a decoding error, instead of being decoded
// to a wrong or rounded value.
func TestCBORDecimalFractionOutOfRange(t *testing.T) {
	cases := []struct {
		name string
		arg  []byte
	}{
		// 4([-2, 2(h'0102030405060708090a')]), a bignum mantissa.
		{"bignum mantissa", []byte{0xc4, 0x82, 0x21, 0xc2, 0x4a, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		// 4([-2, 18446744073709551615]), a mantissa over the int64 range.
		{"uint64 mantissa", []byte{0xc4, 0x82, 0x21, 0x1b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		// 4([-2]), not 2 numbers.
		{"one number", []byte{0xc4, 0x81, 0x21}},
	}
	var s serialize.CBORSerializer
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Deserialize(cborCall(tc.arg...))
			require.Error(t, err)
		})
	}
}

// TestDecimalOtherSerializers checks that the serializers without a decimal
// type encode a Decimal as the nearest float64.
func TestDecimalOtherSerializers(t *testing.T) {
	call := &wamp.Call{
		Request:   1,
		Options:   wamp.Dict{},
		Procedure: "x.y",
		Arguments: wamp.List{serialize.Decimal{Mantissa: 27315, Exponent: -2}},
	}
	for _, s := range []serialize.Serializer{&serialize.JSONSerializer{}, &serialize.MessagePackSerializer{}} {
		b, err := s.Serialize(call)
		require.NoError(t, err)
		msg, err := s.Deserialize(b)
		require.NoError(t, err)
		require.Equal(t, 273.15, msg.(*wamp.Call).Arguments[0], "%T", s)
	}
}

func TestDecimalString(t *testing.T) {
	cases := []struct {
		d    serialize.Decimal
		want string
	}{
		{serialize.Decimal{Mantissa: 27315, Exponent: -2}, "273.15"},
		{serialize.Decimal{Mantissa: -15, Exponent: -1}, "-1.5"},
		{serialize.Decimal{Mantissa: 5, Exponent: -3}, "0.005"},
		{serialize.Decimal{Mantissa: -5, Exponent: -3}, "-0.005"},
		{serialize.Decimal{Mantissa: 42, Exponent: 0}, "42"},
		{serialize.Decimal{Mantissa: 7, Exponent: 3}, "7e3"},
		{serialize.Decimal{Mantissa: 1, Exponent: -30}, "1e-30"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, tc.d.String())
	}
	require.InDelta(t, 273.15, serialize.Decimal{Mantissa: 27315, Exponent: -2}.Float64(), 0)

	// Code that reads a number argument with AsFloat64 keeps working when a
	// CBOR decimal fraction is now decoded to a Decimal.
	f, ok := wamp.AsFloat64(serialize.Decimal{Mantissa: 27315, Exponent: -2})
	require.True(t, ok)
	require.InDelta(t, 273.15, f, 0)
}

// TestDecimalDecodeSelf checks that a number decoded into a Decimal field,
// by a serializer without a decimal type, keeps its value.
func TestDecimalDecodeSelf(t *testing.T) {
	var s serialize.JSONSerializer
	b, err := s.SerializeDataItem(273.15)
	require.NoError(t, err)
	var d serialize.Decimal
	require.NoError(t, s.DeserializeDataItem(b, &d))
	require.Equal(t, "273.15", d.String())
}
