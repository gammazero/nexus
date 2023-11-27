package aat_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/dtegapp/nexus/v3/client"
	"github.com/dtegapp/nexus/v3/wamp"
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
	benchmarkRpc(b, identity, wamp.List{shortString}, func(result *wamp.Result) {
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
	benchmarkRpc(b, identity, wamp.List{largeString}, func(result *wamp.Result) {
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

	benchmarkRpc(b, identity, args, func(result *wamp.Result) {
		v, ok := wamp.AsDict(result.Arguments[0])
		if !ok {
			panic("Can not typecast result to int64")
		}
		if len(v) != 8 {
			panic(fmt.Sprintf("Wrong result!: %d", len(v)))
		}

	})
}

func BenchmarkRPCProgress(b *testing.B) {
	cfg := client.Config{
		Realm:           testRealm,
		ResponseTimeout: clientResponseTimeout,
	}
	server, err := connectClientCfgErr(cfg)
	if err != nil {
		panic("Failed to connect client: " + err.Error())
	}

	const chunkSize = 64

	// b.N will be the number of chunks to send.
	dataLen := b.N * chunkSize

	// Make a chunk of data to send as a progressive result.
	sendBytes := make([]byte, chunkSize)
	for i := 0; i < chunkSize; i++ {
		sendBytes[i] = byte((i % 26) + int('a'))
	}
	sendChunk := string(sendBytes)

	// Define invocation handler.
	handler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
		// Read and send chunks of data until the buffer is empty.
		for i := 0; i < b.N; i++ {
			// Send a chunk of data.
			e := server.SendProgress(ctx, wamp.List{sendChunk}, nil)
			if e != nil {
				panic(e)
			}
		}
		// Send total length as final result.
		return client.InvokeResult{Args: wamp.List{dataLen}}
	}

	// Register procedure.
	if err = server.Register(chunkProc, handler, nil); err != nil {
		panic(err)
	}

	client, err := connectClientCfgErr(cfg)
	if err != nil {
		panic("Failed to connect client: " + err.Error())
	}

	// The progress handler accumulates the chunks of data as they arrive.
	var recvLen int
	progHandler := func(result *wamp.Result) {
		chunk := result.Arguments[0].(string)
		recvLen += len(chunk)
	}

	b.ResetTimer()

	result, err := client.Call(
		context.Background(), chunkProc, nil, nil, nil, progHandler)
	if err != nil {
		panic(err)
	}

	b.StopTimer()

	// As a final result, the callee returns the total length the data.
	totalLen, _ := wamp.AsInt64(result.Arguments[0])
	// Panic if benchmark not valid
	if int(totalLen) != dataLen {
		panic("received wrong about of data in final result")
	}
	if recvLen != dataLen {
		panic("received wrong about of data in progressive results")
	}

	client.Close()
	server.Close()
}

func benchmarkRpc(b *testing.B, action client.InvocationHandler, callArgs wamp.List, verify func(*wamp.Result)) {
	cfg := client.Config{
		Realm:           testRealm,
		ResponseTimeout: clientResponseTimeout,
	}
	server, err := connectClientCfgErr(cfg)
	if err != nil {
		panic("Failed to connect client: " + err.Error())
	}

	if err = server.Register("action", action, nil); err != nil {
		panic("Failed to register procedure: " + err.Error())
	}

	client, err := connectClientCfgErr(cfg)
	if err != nil {
		panic("Failed to connect client: " + err.Error())
	}

	ctx := context.Background()
	var result *wamp.Result

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		result, err = client.Call(ctx, "action", nil, callArgs, nil, nil)
		if err != nil {
			panic(err)
		}
	}

	b.StopTimer()
	verify(result)

	client.Close()
	server.Close()
}

func sum(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	var sum int64
	for _, arg := range inv.Arguments {
		n, ok := wamp.AsInt64(arg)
		if ok {
			sum += n
		}
	}
	return client.InvokeResult{Args: wamp.List{sum}}
}

func identity(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
	return client.InvokeResult{Args: inv.Arguments}
}

func randomString(n int) string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	b := make([]rune, n)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}
