package wamp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	numConv   = 1234
	uriConv   = URI("some.test.uri")
	strConv   = "hello"
	bytesConv = []byte{41, 42, 43}
)

const wrongValueMsg = "Converted to wrong value"

func TestAsList(t *testing.T) {
	const (
		failMsg       = "Failed to convert to List"
		shouldFailMsg = "Should fail converting to List"
	)
	list := List{numConv, uriConv, strConv, bytesConv}
	ilist := []interface{}{interface{}(numConv), uriConv, strConv, bytesConv}

	l, ok := AsList(ilist)
	require.True(t, ok, failMsg)
	require.Equal(t, len(ilist), len(l), failMsg)

	l, ok = AsList(list)
	require.True(t, ok, failMsg)
	require.Equal(t, len(list), len(l), failMsg)

	l, ok = AsList(bytesConv)
	require.True(t, ok, failMsg)
	require.Equal(t, len(bytesConv), len(l), failMsg)

	l, ok = AsList(numConv)
	require.False(t, ok, shouldFailMsg)
	require.Nil(t, l, shouldFailMsg)

	l, ok = AsList(nil)
	require.True(t, ok, failMsg)
	require.Nil(t, l, failMsg)
}

func TestAsDict(t *testing.T) {
	const (
		failMsg       = "Failed to convert to Dict"
		shouldFailMsg = "Should fail converting to Dict"
	)
	dict := Dict{"num": numConv, "uri": uriConv, "str": strConv, "bytes": bytesConv}

	d, ok := AsDict(interface{}(dict))
	require.True(t, ok, failMsg)
	require.NotZero(t, len(d), failMsg)

	d, ok = AsDict(interface{}(numConv))
	require.False(t, ok, shouldFailMsg)
	require.Nil(t, d, shouldFailMsg)

	d, ok = AsDict(nil)
	require.True(t, ok, failMsg)
	require.Nil(t, d, failMsg)
}

func TestAsID(t *testing.T) {
	const (
		failMsg       = "Failed to convert to ID"
		shouldFailMsg = "Should fail converting to ID"
	)
	id, ok := AsID(numConv)
	require.True(t, ok, failMsg)
	require.NotZero(t, id, failMsg)
	require.Equal(t, ID(numConv), id, wrongValueMsg)

	id, ok = AsID(strConv)
	require.False(t, ok, shouldFailMsg)
	require.Zero(t, id, shouldFailMsg)

	_, ok = AsID(nil)
	require.False(t, ok, shouldFailMsg)
}

func TestAsURI(t *testing.T) {
	const (
		failMsg       = "Failed to convert to URI"
		shouldFailMsg = "Should fail converting to URI"
	)

	u, ok := AsURI(uriConv)
	require.True(t, ok, failMsg)
	require.Equal(t, uriConv, u, wrongValueMsg)

	_, ok = AsURI(strConv)
	require.True(t, ok, failMsg)

	_, ok = AsURI(bytesConv)
	require.True(t, ok, failMsg)

	u, ok = AsURI(numConv)
	require.False(t, ok, shouldFailMsg)
	require.Equal(t, URI(""), u, shouldFailMsg)

	u, ok = AsURI(nil)
	require.False(t, ok, shouldFailMsg)
	require.Equal(t, URI(""), u, shouldFailMsg)
}

func TestAsString(t *testing.T) {
	const (
		failMsg       = "String conversion failed"
		shouldFailMsg = "Should fail converting to string"
	)

	s, ok := AsString(strConv)
	require.True(t, ok, failMsg)
	require.Equal(t, strConv, s, wrongValueMsg)

	_, ok = AsString(uriConv)
	require.True(t, ok, failMsg)

	_, ok = AsString(bytesConv)
	require.True(t, ok, failMsg)

	s, ok = AsString(numConv)
	require.False(t, ok, shouldFailMsg)
	require.Equal(t, "", s, shouldFailMsg)

	s, ok = AsString(nil)
	require.False(t, ok, shouldFailMsg)
	require.Equal(t, "", s, shouldFailMsg)
}

func TestAsInt64(t *testing.T) {
	const (
		failMsg       = "Failed to convert to int64"
		shouldFailMsg = "Should fail converting to int64"
	)

	i64, ok := AsInt64(int64(numConv))
	require.True(t, ok, failMsg)
	require.Equal(t, int64(numConv), i64, wrongValueMsg)

	_, ok = AsInt64(ID(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsInt64(uint64(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsInt64(numConv)
	require.True(t, ok, failMsg)

	_, ok = AsInt64(int32(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsInt64(int64(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsInt64(uint(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsInt64(uint32(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsInt64(uint64(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsInt64(float32(numConv))
	require.True(t, ok, failMsg)

	i64, ok = AsInt64(float64(numConv))
	require.True(t, ok, failMsg)
	require.Equal(t, int64(numConv), i64, wrongValueMsg)

	_, ok = AsInt64(strConv)
	require.False(t, ok, shouldFailMsg)

	_, ok = AsInt64(nil)
	require.False(t, ok, shouldFailMsg)
}

func TestAsFloat64(t *testing.T) {
	const (
		failMsg       = "Failed to convert to float64"
		shouldFailMsg = "Should fail converting to float64"
	)

	f64, ok := AsFloat64(float64(numConv))
	require.True(t, ok, failMsg)
	require.Equal(t, float64(numConv), f64, wrongValueMsg)

	_, ok = AsFloat64(float32(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsFloat64(ID(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsFloat64(uint64(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsFloat64(numConv)
	require.True(t, ok, failMsg)

	_, ok = AsFloat64(int32(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsFloat64(int64(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsFloat64(uint(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsFloat64(uint32(numConv))
	require.True(t, ok, failMsg)

	_, ok = AsFloat64(uint64(numConv))
	require.True(t, ok, failMsg)

	f64, ok = AsFloat64(int64(numConv))
	require.True(t, ok, failMsg)
	require.Equal(t, float64(numConv), f64, wrongValueMsg)

	f64, ok = AsFloat64(strConv)
	require.False(t, ok, shouldFailMsg)
	require.Zero(t, f64, shouldFailMsg)

	_, ok = AsFloat64(nil)
	require.False(t, ok, shouldFailMsg)
}

func TestListToStrings(t *testing.T) {
	strs, ok := ListToStrings(List{"hello", "world"})
	require.True(t, ok, "not convered")
	require.Equal(t, []string{"hello", "world"}, strs, "bad conversion")

	_, ok = ListToStrings(List{"hello", 123})
	require.False(t, ok, "should not have converted")
}
