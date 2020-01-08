package wamp

import (
	"testing"
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
	if !ok || len(l) != len(list) {
		t.Error(failMsg)
	}
	if l, ok = AsList(list); !ok || len(l) != len(list) {
		t.Error(failMsg)
	}
	if l, ok = AsList(bytesConv); !ok || len(l) != len(bytesConv) {
		t.Error(failMsg)
	}
	if l, ok = AsList(numConv); ok || l != nil {
		t.Error(shouldFailMsg)
	}
	if l, ok = AsList(nil); !ok && l == nil {
		t.Error(failMsg)
	}
}

func TestAsDict(t *testing.T) {
	const (
		failMsg       = "Failed to convert to Dict"
		shouldFailMsg = "Should fail converting to Dict"
	)
	dict := Dict{"num": numConv, "uri": uriConv, "str": strConv, "bytes": bytesConv}

	d, ok := AsDict(interface{}(dict))
	if !ok || len(d) == 0 {
		t.Error(failMsg)
	}
	if d, ok = AsDict(interface{}(numConv)); ok || d != nil {
		t.Error(shouldFailMsg)
	}
	if d, ok = AsDict(nil); !ok && d == nil {
		t.Error(failMsg)
	}
}

func TestAsID(t *testing.T) {
	const (
		failMsg       = "Failed to convert to ID"
		shouldFailMsg = "Should fail converting to ID"
	)
	id, ok := AsID(numConv)
	if !ok || id == ID(0) {
		t.Error(failMsg)
	}
	if id != ID(numConv) {
		t.Error(wrongValueMsg)
	}
	if id, ok = AsID(strConv); ok || id != ID(0) {
		t.Error(shouldFailMsg)
	}
	if _, ok := AsID(nil); ok {
		t.Error(shouldFailMsg)
	}
}

func TestAsURI(t *testing.T) {
	const (
		failMsg       = "Failed to convert to URI"
		shouldFailMsg = "Should fail converting to URI"
	)

	u, ok := AsURI(uriConv)
	if !ok || u == URI("") {
		t.Error(failMsg)
	}
	if u != uriConv {
		t.Error(wrongValueMsg)
	}
	if _, ok = AsURI(strConv); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsURI(bytesConv); !ok {
		t.Error(failMsg)
	}
	if u, ok = AsURI(numConv); ok || u != "" {
		t.Error(shouldFailMsg)
	}
	if u, ok = AsURI(nil); ok || u != "" {
		t.Error(shouldFailMsg)
	}
}

func TestAsString(t *testing.T) {
	const (
		failMsg       = "String conversion failed"
		shouldFailMsg = "Should fail converting to string"
	)

	s, ok := AsString(strConv)
	if !ok {
		t.Error(failMsg)
	}
	if s != strConv {
		t.Error(wrongValueMsg)
	}
	if _, ok = AsString(uriConv); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsString(bytesConv); !ok {
		t.Error(failMsg)
	}
	s, ok = AsString(numConv)
	if ok || s != "" {
		t.Error(shouldFailMsg)
	}
	s, ok = AsString(nil)
	if ok || s != "" {
		t.Error(shouldFailMsg)
	}
}

func TestAsInt64(t *testing.T) {
	const (
		failMsg       = "Failed to convert to int64"
		shouldFailMsg = "Should fail converting to int64"
	)

	i64, ok := AsInt64(int64(numConv))
	if !ok || i64 == 0 {
		t.Error(failMsg)
	}
	if i64 != int64(numConv) {
		t.Error(wrongValueMsg)
	}
	if _, ok = AsInt64(ID(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsInt64(uint64(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsInt64(int(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsInt64(int32(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsInt64(int64(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsInt64(uint(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsInt64(uint32(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsInt64(uint64(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsInt64(float32(numConv)); !ok {
		t.Error(failMsg)
	}
	if i64, ok = AsInt64(float64(numConv)); !ok {
		t.Error(failMsg)
	}
	if i64 != int64(numConv) {
		t.Error(wrongValueMsg)
	}
	if _, ok = AsInt64(strConv); ok {
		t.Error(shouldFailMsg)
	}
	if _, ok = AsInt64(nil); ok {
		t.Error(shouldFailMsg)
	}
}

func TestAsFloat64(t *testing.T) {
	const (
		failMsg       = "Failed to convert to float64"
		shouldFailMsg = "Should fail converting to float64"
	)

	f64, ok := AsFloat64(float64(numConv))
	if !ok || f64 == 0.0 {
		t.Error(failMsg)
	}
	if f64 != float64(numConv) {
		t.Error(wrongValueMsg)
	}
	if _, ok = AsFloat64(float32(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsFloat64(ID(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsFloat64(uint64(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsFloat64(int(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsFloat64(int32(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsFloat64(int64(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsFloat64(uint(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsFloat64(uint32(numConv)); !ok {
		t.Error(failMsg)
	}
	if _, ok = AsFloat64(uint64(numConv)); !ok {
		t.Error(failMsg)
	}
	if f64, ok = AsFloat64(int64(numConv)); !ok {
		t.Error(failMsg)
	}
	if f64 != float64(numConv) {
		t.Error(wrongValueMsg)
	}
	if f64, ok = AsFloat64(strConv); ok || f64 != 0.0 {
		t.Error(shouldFailMsg)
	}
	if _, ok = AsFloat64(nil); ok {
		t.Error(shouldFailMsg)
	}
}

func TestListToStrings(t *testing.T) {
	strs, ok := ListToStrings(List{"hello", "world"})
	if !ok {
		t.Fatal("not convered")
	}
	if strs[0] != "hello" || strs[1] != "world" {
		t.Fatal("bad conversion")
	}

	if _, ok = ListToStrings(List{"hello", 123}); ok {
		t.Fatal("should not have converted")
	}
}
