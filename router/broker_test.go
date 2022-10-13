package router

import (
	"context"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/wamp"
)

type testPeer struct {
	in chan wamp.Message
}

func newTestPeer() *testPeer {
	return &testPeer{
		in: make(chan wamp.Message, 1),
	}
}

func (p *testPeer) TrySend(msg wamp.Message) error {
	return wamp.TrySend(p.in, msg)
}

func (p *testPeer) Send(msg wamp.Message) error {
	p.in <- msg
	return nil
}

func (p *testPeer) SendCtx(ctx context.Context, msg wamp.Message) error {
	return wamp.SendCtx(ctx, p.in, msg)
}

func (p *testPeer) Recv() <-chan wamp.Message { return p.in }
func (p *testPeer) Close()                    {}

func (p *testPeer) IsLocal() bool { return true }

func TestBasicSubscribe(t *testing.T) {
	// Test subscribing to a topic.
	broker := newBroker(logger, false, true, debug, nil)
	subscriber := newTestPeer()
	sess := wamp.NewSession(subscriber, 0, nil, nil)
	testTopic := wamp.URI("nexus.test.topic")
	broker.subscribe(sess, &wamp.Subscribe{Request: 123, Topic: testTopic})

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	subMsg, ok := rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}
	subID := subMsg.Subscription
	if subID == 0 {
		t.Fatal("invalid suvscription ID")
	}

	// Check that broker created subscription.
	sub, ok := broker.subscriptions[subID]
	if !ok {
		t.Fatal("broker missing subscription")
	}
	if sub.topic != testTopic {
		t.Fatal("subscription to wrong topic")
	}
	sub2, ok := broker.topicSubscription[testTopic]
	if !ok {
		t.Fatal("broker missing subscribers for topic")
	}
	if sub.id != sub2.id {
		t.Fatal("topic->subscription has wrong subscription")
	}
	subIDSet, ok := broker.sessionSubIDSet[sess]
	if !ok {
		t.Fatal("broker missing subscriber ID for session")
	}
	if _, ok = subIDSet[sub.id]; !ok {
		t.Fatal("broker session's subscriber ID set missing subscription ID")
	}

	// Test subscribing to same topic again.
	broker.subscribe(sess, &wamp.Subscribe{Request: 123, Topic: testTopic})
	// Test that subscriber received SUBSCRIBED message
	rsp = <-sess.Recv()
	subMsg, ok = rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}
	// Should get same subscription ID.
	subID2 := subMsg.Subscription
	if subID2 != subID {
		t.Fatal("invalid suvscription ID")
	}
	if len(broker.subscriptions) != 1 {
		t.Fatal("broker has too many subscriptions")
	}
	if sub, ok = broker.topicSubscription[testTopic]; !ok {
		t.Fatal("broker missing topic->subscription")
	}
	if len(sub.subscribers) != 1 {
		t.Fatal("too many subscribers to", testTopic)
	}
	if _, ok = sub.subscribers[sess]; !ok {
		t.Fatal("broker missing subscriber on subscription")
	}
	if len(broker.sessionSubIDSet[sess]) != 1 {
		t.Fatal("too many subscriptions for session")
	}

	// Test subscribing to different topic.
	testTopic2 := wamp.URI("nexus.test.topic2")
	broker.subscribe(sess, &wamp.Subscribe{Request: 123, Topic: testTopic2})
	// Test that subscriber received SUBSCRIBED message
	rsp = <-sess.Recv()
	subMsg, ok = rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}
	subID2 = subMsg.Subscription
	if subID2 == subID {
		t.Fatal("wrong suvscription ID")
	}
	if len(broker.subscriptions) != 2 {
		t.Fatal("wrong number of subscriptions")
	}

	if sub, ok = broker.topicSubscription[testTopic]; !ok {
		t.Fatal("broker missing topic->subscription for topic", testTopic)
	}
	if len(sub.subscribers) != 1 {
		t.Fatal("too many subscribers to", testTopic)
	}

	if sub, ok = broker.topicSubscription[testTopic2]; !ok {
		t.Fatal("broker missing topic->subscription for topic", testTopic2)
	}
	if len(sub.subscribers) != 1 {
		t.Fatal("too many subscribers to", testTopic2)
	}

	if len(broker.sessionSubIDSet[sess]) != 2 {
		t.Fatal("wrong number of subscriptions for session")
	}
}

func TestUnsubscribe(t *testing.T) {
	broker := newBroker(logger, false, true, debug, nil)
	testTopic := wamp.URI("nexus.test.topic")

	// Subscribe session1 to topic
	subscriber := newTestPeer()
	sess1 := wamp.NewSession(subscriber, 0, nil, nil)
	broker.subscribe(sess1, &wamp.Subscribe{Request: 123, Topic: testTopic})
	rsp := <-sess1.Recv()
	subID := rsp.(*wamp.Subscribed).Subscription

	// Subscribe session2 to topic
	subscriber2 := newTestPeer()
	sess2 := wamp.NewSession(subscriber2, 0, nil, nil)
	broker.subscribe(sess2, &wamp.Subscribe{Request: 567, Topic: testTopic})
	rsp = <-sess2.Recv()
	subID2 := rsp.(*wamp.Subscribed).Subscription

	if subID != subID2 {
		t.Fatal("subscribe to same topic resulted in different subscriptions")
	}

	// Check that subscription is present and has 2 subscribers.
	sub, ok := broker.subscriptions[subID]
	if !ok {
		t.Fatal("broker missing subscription")
	}
	if len(sub.subscribers) != 2 {
		t.Fatal("subscription should have 2 subecribers")
	}
	subIDSet, ok := broker.sessionSubIDSet[sess1]
	if !ok {
		t.Fatal("session 1 subscription ID set missing")
	}
	if len(subIDSet) != 1 {
		t.Error("wrong number of subscription ID for session 1")
	}
	if subIDSet, ok = broker.sessionSubIDSet[sess2]; !ok {
		t.Fatal("session 2 subscription ID set missing")
	}
	if len(subIDSet) != 1 {
		t.Error("wrong number of subscription ID for session 2")
	}

	// Test unsubscribing session1 from topic.
	broker.unsubscribe(sess1, &wamp.Unsubscribe{Request: 124, Subscription: subID})
	// Check that session received UNSUBSCRIBED message.
	rsp = <-sess1.Recv()
	unsub, ok := rsp.(*wamp.Unsubscribed)
	if !ok {
		t.Fatal("expected", wamp.UNSUBSCRIBED, "got:", rsp.MessageType())
	}
	unsubID := unsub.Request
	if unsubID == 0 {
		t.Fatal("invalid unsibscribe ID")
	}

	// Check that subscription is still present and now has 1 subscriber.
	sub, ok = broker.subscriptions[subID]
	if !ok {
		t.Fatal("broker missing subscription")
	}
	if len(sub.subscribers) != 1 {
		t.Fatal("subscription should have 1 subecribers")
	}
	// Check that subscriber 1 is gone and subscriber 2 remains.
	if _, ok = sub.subscribers[sess1]; ok {
		t.Fatal("subscription should not have unsubscribed session 1")
	}
	if _, ok = sub.subscribers[sess2]; !ok {
		t.Fatal("subscription missing session 2")
	}
	// Check that topic->subscription remains
	if _, ok = broker.topicSubscription[testTopic]; !ok {
		t.Fatal("topic subscription was deleted but subscription exists")
	}
	if _, ok = broker.sessionSubIDSet[sess1]; ok {
		t.Fatal("session 1 subscription ID set still exists")
	}
	if _, ok = broker.sessionSubIDSet[sess2]; !ok {
		t.Fatal("session 2 subscription ID set missing")
	}

	// Test unsubscribing session2 from invalid subscription ID.
	broker.unsubscribe(sess2, &wamp.Unsubscribe{Request: 124, Subscription: 747})
	// Check that session received ERROR message.
	rsp = <-sess2.Recv()
	_, ok = rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected", wamp.ERROR, "got:", rsp.MessageType())
	}

	// Test unsubscribing session2 from topic.
	broker.unsubscribe(sess2, &wamp.Unsubscribe{Request: 124, Subscription: subID})
	// Check that session received UNSUBSCRIBED message.
	rsp = <-sess2.Recv()
	_, ok = rsp.(*wamp.Unsubscribed)
	if !ok {
		t.Fatal("expected", wamp.UNSUBSCRIBED, "got:", rsp.MessageType())
	}

	// Check the broker removed subscription.
	if _, ok = broker.subscriptions[subID]; ok {
		t.Fatal("subscription still exists")
	}
	if _, ok = broker.topicSubscription[testTopic]; ok {
		t.Fatal("topic subscription still exists")
	}
	if _, ok = broker.sessionSubIDSet[sess1]; ok {
		t.Fatal("session 1 subscription ID set still exists")
	}
	if _, ok = broker.sessionSubIDSet[sess2]; ok {
		t.Fatal("session 2 subscription ID set still exists")
	}
}

func TestRemove(t *testing.T) {
	// Subscribe to topic
	broker := newBroker(logger, false, true, debug, nil)
	subscriber := newTestPeer()
	sess := wamp.NewSession(subscriber, 0, nil, nil)
	testTopic := wamp.URI("nexus.test.topic")
	broker.subscribe(sess, &wamp.Subscribe{Request: 123, Topic: testTopic})
	rsp := <-sess.Recv()
	subID := rsp.(*wamp.Subscribed).Subscription

	testTopic2 := wamp.URI("nexus.test.topic2")
	broker.subscribe(sess, &wamp.Subscribe{Request: 456, Topic: testTopic2})
	rsp = <-sess.Recv()
	subID2 := rsp.(*wamp.Subscribed).Subscription

	broker.removeSession(sess)

	// Wait for another subscriber as a way to wait for the RemoveSession to
	// complete.
	sess2 := wamp.NewSession(subscriber, 0, nil, nil)
	broker.subscribe(sess2,
		&wamp.Subscribe{Request: 789, Topic: wamp.URI("nexus.test.sync")})
	<-sess2.Recv()

	// Check the broker removed subscription.
	_, ok := broker.subscriptions[subID]
	if ok {
		t.Fatal("subscription still exists")
	}
	if _, ok = broker.topicSubscription[testTopic]; ok {
		t.Fatal("topic subscriber still exists")
	}
	if _, ok = broker.subscriptions[subID2]; ok {
		t.Fatal("subscription still exists")
	}
	if _, ok = broker.topicSubscription[testTopic2]; ok {
		t.Fatal("topic subscriber still exists")
	}
	if _, ok = broker.sessionSubIDSet[sess]; ok {
		t.Fatal("session subscription ID set still exists")
	}
}

func TestBasicPubSub(t *testing.T) {
	broker := newBroker(logger, false, true, debug, nil)
	subscriber := newTestPeer()
	sess := wamp.NewSession(subscriber, 0, nil, nil)
	testTopic := wamp.URI("nexus.test.topic")
	msg := &wamp.Subscribe{
		Request: 123,
		Topic:   testTopic,
	}
	broker.subscribe(sess, msg)

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	_, ok := rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, 0, nil, nil)
	broker.publish(pubSess, &wamp.Publish{Request: 124, Topic: testTopic,
		Arguments: wamp.List{"hello world"}})
	rsp = <-sess.Recv()
	evt, ok := rsp.(*wamp.Event)
	if !ok {
		t.Fatal("expected", wamp.EVENT, "got:", rsp.MessageType())
	}
	if len(evt.Arguments) == 0 {
		t.Fatal("missing event payload")
	}
	arg, _ := wamp.AsString(evt.Arguments[0])
	if arg != "hello world" {
		t.Fatal("wrong argument value in payload:", arg)
	}
}

// ----- WAMP v.2 Testing -----

func TestAdvancedPubSub(t *testing.T) {
	broker := newBroker(logger, false, true, debug, nil)
	subscriber := newTestPeer()
	sess := wamp.NewSession(subscriber, 0, nil, nil)
	testTopic := wamp.URI("nexus.test.topic")

	msg := &wamp.Subscribe{
		Request: 123,
		Topic:   "invalid!@#@#$topicuri",
		Options: wamp.Dict{},
	}
	broker.subscribe(sess, msg)

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	_, ok := rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected", wamp.ERROR, "got:", rsp.MessageType())
	}

	msg = &wamp.Subscribe{
		Request: 123,
		Topic:   testTopic,
		Options: wamp.Dict{},
	}
	broker.subscribe(sess, msg)

	// Test that subscriber received SUBSCRIBED message
	rsp = <-sess.Recv()
	_, ok = rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, 0, nil, nil)

	options := wamp.Dict{
		wamp.OptAcknowledge: true,
	}
	broker.publish(pubSess, &wamp.Publish{Request: 125, Topic: "invalid!@#@#$topicuri", Options: options})
	rsp = <-pubSess.Recv()
	_, ok = rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected", wamp.ERROR, "got:", rsp.MessageType())
	}

}

func TestPPTPubSub(t *testing.T) {
	broker := newBroker(logger, false, true, debug, nil)
	subscriber := newTestPeer()
	sess := wamp.NewSession(subscriber, 0, nil, nil)
	testTopic := wamp.URI("nexus.test.topic")

	msg := &wamp.Subscribe{
		Request: 123,
		Topic:   testTopic,
		Options: wamp.Dict{},
	}
	broker.subscribe(sess, msg)

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	subMsg, ok := rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}
	subID := subMsg.Subscription
	if subID == 0 {
		t.Fatal("invalid suvscription ID")
	}

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, 0, nil, nil)

	options := wamp.Dict{
		wamp.OptAcknowledge: true,
	}
	broker.publish(pubSess, &wamp.Publish{Request: 125, Topic: "invalid!@#@#$topicuri", Options: options})
	rsp = <-pubSess.Recv()
	_, ok = rsp.(*wamp.Error)
	if !ok {
		t.Fatal("expected", wamp.ERROR, "got:", rsp.MessageType())
	}

	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "native",
	}
	broker.publish(pubSess, &wamp.Publish{Request: 126, Topic: testTopic, Options: options})
	rsp = <-pubSess.Recv()
	_, ok = rsp.(*wamp.Abort)
	if !ok {
		t.Fatal("expected", wamp.ABORT, "got:", rsp.MessageType())
	}

	greetDetails := wamp.Dict{
		"roles": wamp.Dict{
			"publisher": wamp.Dict{
				"features": wamp.Dict{
					wamp.FeaturePayloadPassthruMode: true,
				},
			},
		},
	}
	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "native",
		wamp.OptPPTCipher:     "ppt_cipher",
		wamp.OptPPTKeyId:      "ppt_keyid",
	}
	publisher = newTestPeer()
	pubSess = wamp.NewSession(publisher, 0, nil, greetDetails)
	broker.publish(pubSess, &wamp.Publish{Request: 124, Topic: testTopic, Options: options})
	rsp = <-sess.Recv()
	_, ok = rsp.(*wamp.Event)
	if !ok {
		t.Fatal("expected", wamp.EVENT, "got:", rsp.MessageType())
	}

}

func TestPrefxPatternBasedSubscription(t *testing.T) {
	// Test match=prefix
	broker := newBroker(logger, false, true, debug, nil)
	subscriber := newTestPeer()
	sess := wamp.NewSession(subscriber, 0, nil, nil)
	testTopic := wamp.URI("nexus.test.topic")
	testTopicPfx := wamp.URI("nexus.test.")
	msg := &wamp.Subscribe{
		Request: 123,
		Topic:   testTopicPfx,
		Options: wamp.Dict{"match": "prefix"},
	}
	broker.subscribe(sess, msg)

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	subMsg, ok := rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}
	subID := subMsg.Subscription
	if subID == 0 {
		t.Fatal("invalid suvscription ID")
	}

	// Check that broker created subscription.
	sub, ok := broker.subscriptions[subID]
	if !ok {
		t.Fatal("broker missing subscription")
	}
	if _, ok = sub.subscribers[sess]; !ok {
		t.Fatal("broker missing subscriber on subscription")
	}
	if sub.topic != testTopicPfx {
		t.Fatal("subscription to wrong topic")
	}
	_, ok = broker.pfxTopicSubscription[testTopicPfx]
	if !ok {
		t.Fatal("broker missing subscribers for topic")
	}
	_, ok = broker.sessionSubIDSet[sess]
	if !ok {
		t.Fatal("broker missing subscriber ID for session")
	}

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, 0, nil, nil)
	broker.publish(pubSess, &wamp.Publish{Request: 124, Topic: testTopic})
	rsp = <-sess.Recv()
	evt, ok := rsp.(*wamp.Event)
	if !ok {
		t.Fatal("expected", wamp.EVENT, "got:", rsp.MessageType())
	}
	_topic, ok := evt.Details["topic"]
	if !ok {
		t.Fatalf("event missing topic")
	}
	topic := _topic.(wamp.URI)
	if topic != testTopic {
		t.Fatal("wrong topic received")
	}
}

func TestWildcardPatternBasedSubscription(t *testing.T) {
	// Test match=prefix
	broker := newBroker(logger, false, true, debug, nil)
	subscriber := newTestPeer()
	sess := wamp.NewSession(subscriber, 0, nil, nil)
	testTopic := wamp.URI("nexus.test.topic")
	testTopicWc := wamp.URI("nexus..topic")
	msg := &wamp.Subscribe{
		Request: 123,
		Topic:   testTopicWc,
		Options: wamp.Dict{"match": "wildcard"},
	}
	broker.subscribe(sess, msg)

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	subMsg, ok := rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}
	subID := subMsg.Subscription
	if subID == 0 {
		t.Fatal("invalid suvscription ID")
	}

	// Check that broker created subscription.
	sub, ok := broker.subscriptions[subID]
	if !ok {
		t.Fatal("broker missing subscription")
	}
	if sub.id != subID {
		t.Fatal("broker subscriptions has subscription with wrong ID")
	}
	if _, ok = sub.subscribers[sess]; !ok {
		t.Fatal("broker missing subscriber on subscription")
	}
	if sub.topic != testTopicWc {
		t.Fatal("subscription to wrong topic")
	}
	if sub.match != "wildcard" {
		t.Fatal("subscription has wrong match policy")
	}
	sub2, ok := broker.wcTopicSubscription[testTopicWc]
	if !ok {
		t.Fatal("broker missing subscription for topic")
	}
	if sub2.id != subID {
		t.Fatal("topic->session has wrong session")
	}
	_, ok = broker.sessionSubIDSet[sess]
	if !ok {
		t.Fatal("broker missing subscriber ID for session")
	}

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, 0, nil, nil)
	broker.publish(pubSess, &wamp.Publish{Request: 124, Topic: testTopic})
	rsp = <-sess.Recv()
	evt, ok := rsp.(*wamp.Event)
	if !ok {
		t.Fatal("expected", wamp.EVENT, "got:", rsp.MessageType())
	}
	_topic, ok := evt.Details["topic"]
	if !ok {
		t.Fatalf("event missing topic")
	}
	topic := _topic.(wamp.URI)
	if topic != testTopic {
		t.Fatal("wrong topic received")
	}
}

func TestSubscriberBlackwhiteListing(t *testing.T) {
	broker := newBroker(logger, false, true, debug, nil)
	subscriber := newTestPeer()
	details := wamp.Dict{
		"authid":   "jdoe",
		"authrole": "admin",
	}
	sess := wamp.NewSession(subscriber, wamp.GlobalID(), details, nil)
	testTopic := wamp.URI("nexus.test.topic")

	broker.subscribe(sess, &wamp.Subscribe{Request: 123, Topic: testTopic})

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	_, ok := rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}

	publisher := newTestPeer()

	details = wamp.Dict{
		"roles": wamp.Dict{
			"publisher": wamp.Dict{
				"features": wamp.Dict{
					"subscriber_blackwhite_listing": true,
				},
			},
		},
	}
	pubSess := wamp.NewSession(publisher, 0, details, nil)

	// Test whilelist
	broker.publish(pubSess, &wamp.Publish{
		Request: 124,
		Topic:   testTopic,
		Options: wamp.Dict{"eligible": wamp.List{sess.ID}},
	})
	rsp, err := wamp.RecvTimeout(sess, time.Second)
	if err != nil {
		t.Fatal("not allowed by whitelist")
	}
	// Test whitelist authrole
	broker.publish(pubSess, &wamp.Publish{
		Request: 125,
		Topic:   testTopic,
		Options: wamp.Dict{"eligible_authrole": wamp.List{"admin"}},
	})
	rsp, err = wamp.RecvTimeout(sess, time.Second)
	if err != nil {
		t.Fatal("not allowed by authrole whitelist")
	}
	// Test whitelist authid
	broker.publish(pubSess, &wamp.Publish{
		Request: 126,
		Topic:   testTopic,
		Options: wamp.Dict{"eligible_authid": wamp.List{"jdoe"}},
	})
	rsp, err = wamp.RecvTimeout(sess, time.Second)
	if err != nil {
		t.Fatal("not allowed by authid whitelist")
	}

	// Test blacklist.
	broker.publish(pubSess, &wamp.Publish{
		Request: 127,
		Topic:   testTopic,
		Options: wamp.Dict{"exclude": wamp.List{sess.ID}},
	})
	rsp, err = wamp.RecvTimeout(sess, time.Second)
	if err == nil {
		t.Fatal("not excluded by blacklist")
	}
	// Test blacklist authrole
	broker.publish(pubSess, &wamp.Publish{
		Request: 128,
		Topic:   testTopic,
		Options: wamp.Dict{"exclude_authrole": wamp.List{"admin"}},
	})
	rsp, err = wamp.RecvTimeout(sess, time.Second)
	if err == nil {
		t.Fatal("not excluded by authrole blacklist")
	}
	// Test blacklist authid
	broker.publish(pubSess, &wamp.Publish{
		Request: 129,
		Topic:   testTopic,
		Options: wamp.Dict{"exclude_authid": wamp.List{"jdoe"}},
	})
	rsp, err = wamp.RecvTimeout(sess, time.Second)
	if err == nil {
		t.Fatal("not excluded by authid blacklist")
	}

	// Test that blacklist takes precedence over whitelist.
	broker.publish(pubSess, &wamp.Publish{
		Request: 126,
		Topic:   testTopic,
		Options: wamp.Dict{"eligible_authid": []string{"jdoe"},
			"exclude_authid": []string{"jdoe"}},
	})
	rsp, err = wamp.RecvTimeout(sess, time.Second)
	if err == nil {
		t.Fatal("should have been excluded by blacklist")
	}
}

func TestPublisherExclusion(t *testing.T) {
	broker := newBroker(logger, false, true, debug, nil)
	subscriber := newTestPeer()
	sess := wamp.NewSession(subscriber, 0, nil, nil)
	testTopic := wamp.URI("nexus.test.topic")

	broker.subscribe(sess, &wamp.Subscribe{Request: 123, Topic: testTopic})

	// Test that subscriber received SUBSCRIBED message
	rsp, err := wamp.RecvTimeout(sess, time.Second)
	if err != nil {
		t.Fatal("subscribe session did not get response to SUBSCRIBE")
	}
	_, ok := rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}

	publisher := newTestPeer()

	details := wamp.Dict{
		"roles": wamp.Dict{
			"publisher": wamp.Dict{
				"features": wamp.Dict{
					"publisher_exclusion": true,
				},
			},
		},
	}
	pubSess := wamp.NewSession(publisher, 0, nil, details)

	// Subscribe the publish session also.
	broker.subscribe(pubSess, &wamp.Subscribe{Request: 123, Topic: testTopic})
	// Test that pub session received SUBSCRIBED message
	rsp, err = wamp.RecvTimeout(pubSess, time.Second)
	if err != nil {
		t.Fatal("publish session did not get response to SUBSCRIBE")
	}
	_, ok = rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}

	// Publish message with exclud_me = false.
	broker.publish(pubSess, &wamp.Publish{
		Request: 124,
		Topic:   testTopic,
		Options: wamp.Dict{"exclude_me": false},
	})
	rsp, err = wamp.RecvTimeout(sess, time.Second)
	if err != nil {
		t.Fatal("subscriber did not receive event")
	}
	rsp, err = wamp.RecvTimeout(pubSess, time.Second)
	if err != nil {
		t.Fatal("pub session should have received event")
	}

	// Publish message with exclud_me = true.
	broker.publish(pubSess, &wamp.Publish{
		Request: 124,
		Topic:   testTopic,
		Options: wamp.Dict{"exclude_me": true},
	})
	rsp, err = wamp.RecvTimeout(sess, time.Second)
	if err != nil {
		t.Fatal("subscriber did not receive event")
	}
	rsp, err = wamp.RecvTimeout(pubSess, time.Second)
	if err == nil {
		t.Fatal("pub session should NOT have received event")
	}
}

func TestPublisherIdentification(t *testing.T) {
	broker := newBroker(logger, false, true, debug, nil)
	subscriber := newTestPeer()

	details := wamp.Dict{
		"roles": wamp.Dict{
			"subscriber": wamp.Dict{
				"features": wamp.Dict{
					"publisher_identification": true,
				},
			},
		},
	}
	sess := wamp.NewSession(subscriber, 0, nil, details)

	testTopic := wamp.URI("nexus.test.topic")

	broker.subscribe(sess, &wamp.Subscribe{Request: 123, Topic: testTopic})

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	_, ok := rsp.(*wamp.Subscribed)
	if !ok {
		t.Fatal("expected", wamp.SUBSCRIBED, "got:", rsp.MessageType())
	}

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, wamp.GlobalID(), nil, nil)
	broker.publish(pubSess, &wamp.Publish{
		Request: 124,
		Topic:   testTopic,
		Options: wamp.Dict{"disclose_me": true},
	})
	rsp = <-sess.Recv()
	evt, ok := rsp.(*wamp.Event)
	if !ok {
		t.Fatal("expected", wamp.EVENT, "got:", rsp.MessageType())
	}
	pub, ok := evt.Details["publisher"]
	if !ok {
		t.Fatal("missing publisher ID")
	}
	if pub.(wamp.ID) != pubSess.ID {
		t.Fatal("incorrect publisher ID disclosed")
	}
}
