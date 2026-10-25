package serialize

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/ugorji/go/codec"
)

// cborTagDecimalFraction is the CBOR tag for a decimal fraction, RFC 8949
// section 3.4.4.
const cborTagDecimalFraction = 4

// Decimal is an exact decimal number, Mantissa * 10**Exponent.
//
// The CBOR serializer decodes a CBOR decimal fraction (tag 4) to a Decimal,
// and encodes a Decimal as a decimal fraction, so that decimal values pass
// through the router unchanged. A decimal fraction whose exponent or mantissa
// does not fit in an int64, such as one with a bignum mantissa, cannot be
// decoded.
//
// The JSON and MessagePack serializers, which have no decimal type, encode a
// Decimal as the nearest float64.
type Decimal struct {
	Mantissa int64
	Exponent int64
}

// Float64 returns the float64 nearest to d.
func (d Decimal) Float64() float64 {
	f, _ := strconv.ParseFloat(d.eNotation(), 64)
	return f
}

// String returns d in decimal notation, such as "273.15", or in exponent
// notation, such as "27315e-30", when the exponent is large.
func (d Decimal) String() string {
	if d.Exponent == 0 {
		return strconv.FormatInt(d.Mantissa, 10)
	}
	if d.Exponent > 0 || d.Exponent < -18 {
		return d.eNotation()
	}
	digits := strconv.FormatInt(d.Mantissa, 10)
	sign := ""
	if d.Mantissa < 0 {
		sign, digits = "-", digits[1:]
	}
	n := int(-d.Exponent)
	if len(digits) <= n {
		digits = strings.Repeat("0", n-len(digits)+1) + digits
	}
	return sign + digits[:len(digits)-n] + "." + digits[len(digits)-n:]
}

func (d Decimal) eNotation() string {
	return strconv.FormatInt(d.Mantissa, 10) + "e" + strconv.FormatInt(d.Exponent, 10)
}

// CodecEncodeSelf encodes d as the nearest float64, for serializers that have
// no decimal type. The CBOR serializer encodes d as a decimal fraction
// instead.
func (d *Decimal) CodecEncodeSelf(e *codec.Encoder) {
	e.MustEncode(d.Float64())
}

// CodecDecodeSelf decodes a number into d.
func (d *Decimal) CodecDecodeSelf(dec *codec.Decoder) {
	var f float64
	dec.MustDecode(&f)
	// The shortest exponent notation, such as "2.7315e+02", has at most 17
	// significant digits, so its mantissa always fits in an int64.
	s := strconv.FormatFloat(f, 'e', -1, 64)
	mant, exp, _ := strings.Cut(s, "e")
	e, _ := strconv.ParseInt(exp, 10, 64)
	whole, frac, _ := strings.Cut(mant, ".")
	m, _ := strconv.ParseInt(whole+frac, 10, 64)
	*d = Decimal{Mantissa: m, Exponent: e - int64(len(frac))}
}

// decimalExt is the CBOR extension that encodes a Decimal as a decimal
// fraction, [exponent, mantissa] with tag 4, and decodes one to a Decimal.
type decimalExt struct{}

var errDecimalRange = errors.New("CBOR decimal fraction exponent or mantissa does not fit in an int64")

func (decimalExt) ConvertExt(v any) any {
	d := v.(*Decimal)
	return []any{d.Exponent, d.Mantissa}
}

func (decimalExt) UpdateExt(dst, src any) {
	parts, ok := src.([]any)
	if !ok || len(parts) != 2 {
		panic(errors.New("CBOR decimal fraction is not an array of 2 numbers"))
	}
	exp, ok := decimalInt(parts[0])
	if !ok {
		panic(errDecimalRange)
	}
	mant, ok := decimalInt(parts[1])
	if !ok {
		panic(errDecimalRange)
	}
	*dst.(*Decimal) = Decimal{Mantissa: mant, Exponent: exp}
}

// decimalInt returns v as an int64, if it is an integer that fits in one.
func decimalInt(v any) (int64, bool) {
	switch v := v.(type) {
	case int64:
		return v, true
	case uint64:
		if v > math.MaxInt64 {
			return 0, false
		}
		return int64(v), true
	}
	return 0, false
}
