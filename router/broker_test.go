package router

import (
	"testing"

	"github.com/gammazero/nexus/wamp"
)

type testPeer struct {
	in chan wamp.Message
}

func newTestPeer() *testPeer {
	return &testPeer{
		in: make(chan wamp.Message, 1),
	}
}

func (p *testPeer) Send(msg wamp.Message)     { p.in <- msg }
func (p *testPeer) Recv() <-chan wamp.Message { return p.in }
func (p *testPeer) Close()                    { return }

func TestBasicSubscribe(t *testing.T) {
	// Test subscribing to a topic.
	broker := NewBroker(false, true).(*broker)
	subscriber := newTestPeer()
	sess := &Session{Peer: subscriber}
	testTopic := wamp.URI("nexus.test.topic")
	msg := &wamp.Subscribe{Request: 123, Topic: testTopic}
	broker.Subscribe(sess, msg)

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	sub, ok := rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected ", wamp.SUBSCRIBED, " got: ", rsp.MessageType())
	}
	subID := sub.Subscription
	if subID == 0 {
		t.Fatal("invalid suvscription ID")
	}

	// Check that broker created subscription.
	topic, ok := broker.subscriptions[subID]
	if !ok {
		t.Fatal("broker missing subscription")
	}
	if topic != testTopic {
		t.Fatal("subscription to wrong topic")
	}
	_, ok = broker.topicSubscribers[testTopic]
	if !ok {
		t.Fatal("broker missing subscribers for topic")
	}
	_, ok = broker.sessionSubIDSet[sess]
	if !ok {
		t.Fatal("broker missing subscriber ID for session")
	}

	// Test subscribing to same topic again.
	msg = &wamp.Subscribe{Request: 123, Topic: testTopic}
	broker.Subscribe(sess, msg)
	// Test that subscriber received SUBSCRIBED message
	rsp = <-sess.Recv()
	sub, ok = rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected ", wamp.SUBSCRIBED, " got: ", rsp.MessageType())
	}
	// Should get same subscription ID.
	subID2 := sub.Subscription
	if subID2 != subID {
		t.Fatal("invalid suvscription ID")
	}
	if len(broker.subscriptions) != 1 {
		t.Fatal("broker has too many subscriptions")
	}
	if len(broker.topicSubscribers[testTopic]) != 1 {
		t.Fatal("too many subscribers to ", testTopic)
	}
	if len(broker.sessionSubIDSet[sess]) != 1 {
		t.Fatal("too many subscriptions for session")
	}

	// Test subscribing to different topic.
	testTopic2 := wamp.URI("nexus.test.topic2")
	msg = &wamp.Subscribe{Request: 123, Topic: testTopic2}
	broker.Subscribe(sess, msg)
	// Test that subscriber received SUBSCRIBED message
	rsp = <-sess.Recv()
	sub, ok = rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected ", wamp.SUBSCRIBED, " got: ", rsp.MessageType())
	}
	subID2 = sub.Subscription
	if subID2 == subID {
		t.Fatal("wrong suvscription ID")
	}
	if len(broker.subscriptions) != 2 {
		t.Fatal("wrong number of subscriptions")
	}
	if len(broker.topicSubscribers[testTopic]) != 1 {
		t.Fatal("too many subscribers to ", testTopic)
	}
	if len(broker.topicSubscribers[testTopic2]) != 1 {
		t.Fatal("too many subscribers to ", testTopic2)
	}
	if len(broker.sessionSubIDSet[sess]) != 2 {
		t.Fatal("wrong number of subscriptions for session")
	}
}

func TestUnsubscribe(t *testing.T) {
	// Subscribe to topic
	broker := NewBroker(false, true).(*broker)
	subscriber := newTestPeer()
	sess := &Session{Peer: subscriber}
	testTopic := wamp.URI("nexus.test.topic")
	msg := &wamp.Subscribe{Request: 123, Topic: testTopic}
	broker.Subscribe(sess, msg)
	rsp := <-sess.Recv()
	subID := rsp.(*wamp.Subscribed).Subscription

	// Test unsubscribing from topic.
	umsg := &wamp.Unsubscribe{Request: 124, Subscription: subID}
	broker.Unsubscribe(sess, umsg)
	// Check that session received UNSUBSCRIBED message.
	rsp = <-sess.Recv()
	unsub, ok := rsp.(*wamp.Unsubscribed)
	if !ok {
		t.Fatal("expected ", wamp.UNSUBSCRIBED, " got: ", rsp.MessageType())
	}
	unsubID := unsub.Request
	if unsubID == 0 {
		t.Fatal("invalid unsibscribe ID")
	}
	// Check the broker removed subscription.
	if _, ok = broker.subscriptions[subID]; ok {
		t.Fatal("subscription still exists")
	}
	if _, ok = broker.topicSubscribers[testTopic]; ok {
		t.Fatal("topic subscriber still exists")
	}
	if _, ok = broker.sessionSubIDSet[sess]; ok {
		t.Fatal("session subscription ID set still exists")
	}
}

func TestRemove(t *testing.T) {
	// Subscribe to topic
	broker := NewBroker(false, true).(*broker)
	subscriber := newTestPeer()
	sess := &Session{Peer: subscriber}
	testTopic := wamp.URI("nexus.test.topic")
	msg := &wamp.Subscribe{Request: 123, Topic: testTopic}
	broker.Subscribe(sess, msg)
	rsp := <-sess.Recv()
	subID := rsp.(*wamp.Subscribed).Subscription

	testTopic2 := wamp.URI("nexus.test.topic2")
	msg2 := &wamp.Subscribe{Request: 456, Topic: testTopic2}
	broker.Subscribe(sess, msg2)
	rsp = <-sess.Recv()
	subID2 := rsp.(*wamp.Subscribed).Subscription

	broker.RemoveSession(sess)
	broker.sync()

	// Check the broker removed subscription.
	_, ok := broker.subscriptions[subID]
	if ok {
		t.Fatal("subscription still exists")
	}
	if _, ok = broker.topicSubscribers[testTopic]; ok {
		t.Fatal("topic subscriber still exists")
	}
	if _, ok = broker.subscriptions[subID2]; ok {
		t.Fatal("subscription still exists")
	}
	if _, ok = broker.topicSubscribers[testTopic2]; ok {
		t.Fatal("topic subscriber still exists")
	}
	if _, ok = broker.sessionSubIDSet[sess]; ok {
		t.Fatal("session subscription ID set still exists")
	}
}

// ----- WAMP v.2 Testing -----

func TestPrefxPatternBasedSubscription(t *testing.T) {
	// Test match=prefix
	broker := NewBroker(false, true).(*broker)
	subscriber := newTestPeer()
	sess := &Session{Peer: subscriber}
	testTopic := wamp.URI("nexus.test.topic")
	testTopicPfx := wamp.URI("nexus.test.")
	msg := &wamp.Subscribe{
		Request: 123,
		Topic:   testTopicPfx,
		Options: map[string]interface{}{"match": "prefix"},
	}
	broker.Subscribe(sess, msg)

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	sub, ok := rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected ", wamp.SUBSCRIBED, " got: ", rsp.MessageType())
	}
	subID := sub.Subscription
	if subID == 0 {
		t.Fatal("invalid suvscription ID")
	}

	// Check that broker created subscription.
	topic, ok := broker.pfxSubscriptions[subID]
	if !ok {
		t.Fatal("broker missing subscription")
	}
	if topic != testTopicPfx {
		t.Fatal("subscription to wrong topic")
	}
	_, ok = broker.pfxTopicSubscribers[testTopicPfx]
	if !ok {
		t.Fatal("broker missing subscribers for topic")
	}
	_, ok = broker.sessionSubIDSet[sess]
	if !ok {
		t.Fatal("broker missing subscriber ID for session")
	}

	publisher := newTestPeer()
	pubSess := &Session{Peer: publisher}
	pubMsg := &wamp.Publish{
		Request: 124,
		Topic:   testTopic,
	}
	broker.Publish(pubSess, pubMsg)
	rsp = <-sess.Recv()
	evt, ok := rsp.(*wamp.Event)
	if !ok {
		t.Fatal("expected", wamp.EVENT, "got:", rsp.MessageType)
	}
	_topic, ok := evt.Details["topic"]
	if !ok {
		t.Fatalf("event missing topic")
	}
	topic = _topic.(wamp.URI)
	if topic != testTopic {
		t.Fatal("wrong topic received")
	}
}

func TestWildcardPatternBasedSubscription(t *testing.T) {
	// Test match=prefix
	broker := NewBroker(false, true).(*broker)
	subscriber := newTestPeer()
	sess := &Session{Peer: subscriber}
	testTopic := wamp.URI("nexus.test.topic")
	testTopicWc := wamp.URI("nexus..topic")
	msg := &wamp.Subscribe{
		Request: 123,
		Topic:   testTopicWc,
		Options: map[string]interface{}{"match": "wildcard"},
	}
	broker.Subscribe(sess, msg)

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	sub, ok := rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected ", wamp.SUBSCRIBED, " got: ", rsp.MessageType())
	}
	subID := sub.Subscription
	if subID == 0 {
		t.Fatal("invalid suvscription ID")
	}

	// Check that broker created subscription.
	topic, ok := broker.wcSubscriptions[subID]
	if !ok {
		t.Fatal("broker missing subscription")
	}
	if topic != testTopicWc {
		t.Fatal("subscription to wrong topic")
	}
	_, ok = broker.wcTopicSubscribers[testTopicWc]
	if !ok {
		t.Fatal("broker missing subscribers for topic")
	}
	_, ok = broker.sessionSubIDSet[sess]
	if !ok {
		t.Fatal("broker missing subscriber ID for session")
	}

	publisher := newTestPeer()
	pubSess := &Session{Peer: publisher}
	pubMsg := &wamp.Publish{
		Request: 124,
		Topic:   testTopic,
	}
	broker.Publish(pubSess, pubMsg)
	rsp = <-sess.Recv()
	evt, ok := rsp.(*wamp.Event)
	if !ok {
		t.Fatal("expected", wamp.EVENT, "got:", rsp.MessageType)
	}
	_topic, ok := evt.Details["topic"]
	if !ok {
		t.Fatalf("event missing topic")
	}
	topic = _topic.(wamp.URI)
	if topic != testTopic {
		t.Fatal("wrong topic received")
	}
}

func TestSubscriberBlackwhiteListing(t *testing.T) {
	// TODO: code here
}

func TestPublisherExclusion(t *testing.T) {
	// TODO: code here
}

func TestPublisherIdentification(t *testing.T) {
	// TODO: code here
}
