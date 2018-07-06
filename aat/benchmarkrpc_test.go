package aat

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

func BenchmarkRpcIntegerList(b *testing.B) {
	args := wamp.List{make([]int, 128)}
	benchmarkRpc(b, sum, args, func(result *wamp.Result) {
		v, ok := wamp.AsInt64(result.Arguments[0])
		if !ok {
			panic("Can not typecast result to int64")
		}
		if v != 0 {
			panic(fmt.Sprintf("Wrong result!: %d", v))
		}
	})
}

func BenchmarkRpcShortString(b *testing.B) {
	shortString := randomString(128)
	benchmarkRpc(b, identify, wamp.List{shortString}, func(result *wamp.Result) {
		v, ok := wamp.AsString(result.Arguments[0])
		if !ok {
			panic("Can not typecast result to int64")
		}
		if v != shortString {
			panic(fmt.Sprintf("Wrong result!: %v", v))
		}
	})
}

func BenchmarkRpcLargeString(b *testing.B) {
	largeString := randomString(4096)
	benchmarkRpc(b, identify, wamp.List{largeString}, func(result *wamp.Result) {
		v, ok := wamp.AsString(result.Arguments[0])
		if !ok {
			panic("Can not typecast result to int64")
		}
		if v != largeString {
			panic(fmt.Sprintf("Wrong result!: %v", v))
		}
	})
}

func BenchmarkRpcDict(b *testing.B) {
	dict := wamp.Dict{}
	for i := 0; i < 8; i++ {
		dict[randomString(8)] = randomString(8)
	}
	args := wamp.List{dict}

	benchmarkRpc(b, identify, args, func(result *wamp.Result) {
		v, ok := wamp.AsDict(result.Arguments[0])
		if !ok {
			panic("Can not typecast result to int64")
		}
		if len(v) != 8 {
			panic(fmt.Sprintf("Wrong result!: %d", len(v)))
		}

	})
}

func benchmarkRpc(b *testing.B, action client.InvocationHandler, callArgs wamp.List, verify func(*wamp.Result)) {
	server, err := connectClient()
	if err != nil {
		panic("Failed to connect client: " + err.Error())
	}

	if err = server.Register("action", action, nil); err != nil {
		panic("Failed to register procedure: " + err.Error())
	}

	client, err := connectClient()
	if err != nil {
		panic("Failed to connect client: " + err.Error())
	}

	ctx := context.Background()
	var result *wamp.Result

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		result, err = client.Call(ctx, "action", nil, callArgs, nil, "")
		if err != nil {
			panic(err)
		}
	}

	b.StopTimer()
	verify(result)

	client.Close()
	server.Close()
}

func sum(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
	var sum int64
	for i := range args {
		n, ok := wamp.AsInt64(args[i])
		if ok {
			sum += n
		}
	}
	return &client.InvokeResult{Args: wamp.List{sum}}
}

func identify(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
	return &client.InvokeResult{Args: args}
}

func randomString(n int) string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	b := make([]rune, n)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}
