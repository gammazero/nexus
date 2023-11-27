package aat_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/dtegapp/nexus/v3/client"
	"github.com/dtegapp/nexus/v3/wamp"
)

const benchMsgCount = 5

func BenchmarkPubSub(b *testing.B) {
	for i := 2; i <= 512; i <<= 1 {
		b.Run(fmt.Sprintf("1Pub%03dSub", i), func(b *testing.B) {
			benchPubSub(i, b)
		})
	}
}

func benchPubSub(subCount int, b *testing.B) {
	var allDone sync.WaitGroup

	eventHandler := func(ev *wamp.Event) {
		allDone.Done()
	}

	subs := make([]*client.Client, subCount)
	for i := range subs {
		subs[i] = connectSubscriber(eventHandler)
	}

	// Connect publisher session.
	cfg := client.Config{
		Realm:           testRealm,
		ResponseTimeout: clientResponseTimeout,
	}
	publisher, err := connectClientCfgErr(cfg)
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
		subs[i].Close()
	}
}

func connectSubscriber(fn client.EventHandler) *client.Client {
	// Connect subscriber session.
	cfg := client.Config{
		Realm:           testRealm,
		ResponseTimeout: clientResponseTimeout,
	}
	subscriber, err := connectClientCfgErr(cfg)
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
