package serialize

import (
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBytesExtWrapperWriteExt(t *testing.T) {
	var got reflect.Value
	x := bytesExtWrapper{
		encFn: func(v reflect.Value) ([]byte, error) {
			got = v
			return []byte("encoded"), nil
		},
	}
	require.Equal(t, []byte("encoded"), x.WriteExt(BinaryData("data")))
	require.Equal(t, BinaryData("data"), got.Interface(),
		"encode not given the value")

	// An error from encode is a panic, which the codec package recovers and
	// returns as an error.
	errEncode := errors.New("cannot encode")
	x.encFn = func(reflect.Value) ([]byte, error) { return nil, errEncode }
	require.PanicsWithError(t, errEncode.Error(), func() {
		x.WriteExt(BinaryData("data"))
	})
}

func TestBytesExtWrapperReadExt(t *testing.T) {
	x := bytesExtWrapper{
		decFn: func(v reflect.Value, data []byte) error {
			v.Elem().SetBytes(data)
			return nil
		},
	}
	var b BinaryData
	x.ReadExt(&b, []byte("data"))
	require.Equal(t, BinaryData("data"), b)

	// An error from decode is a panic, which the codec package recovers and
	// returns as an error.
	errDecode := errors.New("cannot decode")
	x.decFn = func(reflect.Value, []byte) error { return errDecode }
	require.PanicsWithError(t, errDecode.Error(), func() {
		x.ReadExt(&b, []byte("data"))
	})
}
