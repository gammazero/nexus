package aat

import (
	"sync"
	"testing"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
)

const benchMsgCount = 5

func BenchmarkPub2Sub(b *testing.B) {
	benchPubSub(2, b)
}

func BenchmarkPub4Sub(b *testing.B) {
	benchPubSub(4, b)
}

func BenchmarkPub8Sub(b *testing.B) {
	benchPubSub(4, b)
}

func BenchmarkPub16Sub(b *testing.B) {
	benchPubSub(16, b)
}

func BenchmarkPub32Sub(b *testing.B) {
	benchPubSub(32, b)
}

func BenchmarkPub64Sub(b *testing.B) {
	benchPubSub(64, b)
}

func BenchmarkPub128Sub(b *testing.B) {
	benchPubSub(128, b)
}

func BenchmarkPub256Sub(b *testing.B) {
	benchPubSub(256, b)
}

func BenchmarkPub512Sub(b *testing.B) {
	benchPubSub(256, b)
}

func benchPubSub(subCount int, b *testing.B) {
	var allDone sync.WaitGroup

	evtHandler := func(args wamp.List, kwargs wamp.Dict, details wamp.Dict) {
		allDone.Done()
	}

	subs := make([]*client.Client, subCount)
	for i := range subs {
		subs[i] = connectSubscriber(evtHandler)
	}

	// Connect publisher session.
	publisher, err := connectClient()
	if err != nil {
		panic("Failed to connect client: " + err.Error())
	}

	args := wamp.List{"hello world"}
	recvCount := subCount * benchMsgCount

	b.ResetTimer()

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < b.N; i++ {
		allDone.Add(recvCount)
		for j := 0; j < benchMsgCount; j++ {
			// Publish an event to topic.
			err = publisher.Publish(testTopic, nil, args, nil)
			if err != nil {
				panic("Error waiting for published response: " + err.Error())
			}
		}
		// Wait until all subscribers got the message.
		allDone.Wait()
	}

	publisher.Close()
	for i := range subs {
		subs[i].Unsubscribe(testTopic)
		subs[i].Close()
	}
}

func connectSubscriber(fn client.EventHandler) *client.Client {
	// Connect subscriber session.
	subscriber, err := connectClient()
	if err != nil {
		panic("Failed to connect client: " + err.Error())
	}

	// Subscribe to event.
	err = subscriber.Subscribe(testTopic, fn, nil)
	if err != nil {
		panic("subscribe error: " + err.Error())
	}
	return subscriber
}
