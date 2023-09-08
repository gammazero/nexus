package router

import (
	"context"
	"testing"
	"time"

	"github.com/gammazero/nexus/v3/wamp"
	"github.com/stretchr/testify/require"
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

func newTestBroker(t *testing.T, eventCfgs []*TopicEventHistoryConfig) *broker {
	b, err := newBroker(logger, false, true, debug, nil, eventCfgs)
	require.NoError(t, err, "Can not initialize broker")
	t.Cleanup(func() {
		b.close()
	})
	return b
}

func TestBasicSubscribe(t *testing.T) {
	// Test subscribing to a topic.
	broker := newTestBroker(t, nil)
	subscriber := newTestPeer()
	sess := wamp.NewSession(subscriber, 0, nil, nil)
	testTopic := wamp.URI("nexus.test.topic")
	broker.subscribe(sess, &wamp.Subscribe{Request: 123, Topic: testTopic})

	// Test that subscriber received SUBSCRIBED message
	rsp := <-sess.Recv()
	subMsg, ok := rsp.(*wamp.Subscribed)
	require.True(t, ok, "expected SUBSCRIBED")
	subID := subMsg.Subscription
	require.NotZero(t, subID, "invalid suvscription ID")

	// Check that broker created subscription.
	sub, ok := broker.subscriptions[subID]
	require.True(t, ok, "broker missing subscription")
	require.Equal(t, testTopic, sub.topic, "subscription to wrong topic")
	sub2, ok := broker.topicSubscription[testTopic]
	require.True(t, ok, "broker missing subscribers for topic")
	require.Equal(t, sub2.id, sub.id, "topic->subscription has wrong subscription")
	subIDSet, ok := broker.sessionSubIDSet[sess]
	require.True(t, ok, "broker missing subscriber ID for session")
	_, ok = subIDSet[sub.id]
	require.True(t, ok, "broker session's subscriber ID set missing subscription ID")

	// Test subscribing to same topic again.
	broker.subscribe(sess, &wamp.Subscribe{Request: 123, Topic: testTopic})
	// Test that subscriber received SUBSCRIBED message
	rsp = <-sess.Recv()
	subMsg, ok = rsp.(*wamp.Subscribed)
	require.True(t, ok, "expected SUBSCRIBED")
	// Should get same subscription ID.
	subID2 := subMsg.Subscription
	require.Equal(t, subID, subID2, "invalid suvscription ID")
	require.Equal(t, 1, len(broker.subscriptions), "broker has too many subscriptions")
	sub, ok = broker.topicSubscription[testTopic]
	require.True(t, ok, "broker missing topic->subscription")
	require.Equalf(t, 1, len(sub.subscribers), "too many subscribers to %s", testTopic)
	_, ok = sub.subscribers[sess]
	require.True(t, ok, "broker missing subscriber on subscription")
	require.Equal(t, 1, len(broker.sessionSubIDSet[sess]), "too many subscriptions for session")

	// Test subscribing to different topic.
	testTopic2 := wamp.URI("nexus.test.topic2")
	broker.subscribe(sess, &wamp.Subscribe{Request: 123, Topic: testTopic2})
	// Test that subscriber received SUBSCRIBED message
	rsp = <-sess.Recv()
	subMsg, ok = rsp.(*wamp.Subscribed)
	require.True(t, ok, "expected SUBSCRIBED")
	subID2 = subMsg.Subscription
	require.NotEqual(t, subID, subID2, "wrong subscription ID")
	require.Equal(t, 2, len(broker.subscriptions), "wrong number of subscriptions")

	sub, ok = broker.topicSubscription[testTopic]
	require.True(t, ok, "broker missing topic->subscription for topic")
	require.Equal(t, 1, len(sub.subscribers), "too many subscribers")

	sub, ok = broker.topicSubscription[testTopic2]
	require.True(t, ok, "broker missing topic->subscription for topic")
	require.Equalf(t, 1, len(sub.subscribers), "too many subscribers to %s", testTopic2)
	require.Equal(t, 2, len(broker.sessionSubIDSet[sess]), "wrong number of subscriptions for session")
}

func TestUnsubscribe(t *testing.T) {
	broker := newTestBroker(t, nil)
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
	require.Equal(t, subID2, subID, "subscribe to same topic resulted in different subscriptions")

	// Check that subscription is present and has 2 subscribers.
	sub, ok := broker.subscriptions[subID]
	require.True(t, ok, "broker missing subscription")
	require.Equal(t, 2, len(sub.subscribers), "subscription should have 2 subecribers")
	subIDSet, ok := broker.sessionSubIDSet[sess1]
	require.True(t, ok, "session 1 subscription ID set missing")
	require.Equal(t, 1, len(subIDSet), "wrong number of subscription ID for session 1")
	subIDSet, ok = broker.sessionSubIDSet[sess2]
	require.True(t, ok, "session 2 subscription ID set missing")
	require.Equal(t, 1, len(subIDSet), "wrong number of subscription ID for session 2")

	// Test unsubscribing session1 from topic.
	broker.unsubscribe(sess1, &wamp.Unsubscribe{Request: 124, Subscription: subID})
	// Check that session received UNSUBSCRIBED message.
	rsp = <-sess1.Recv()
	unsub, ok := rsp.(*wamp.Unsubscribed)
	require.True(t, ok, "expected UNSUBSCRIBED")
	unsubID := unsub.Request
	require.NotZero(t, unsubID, "invalid unsibscribe ID")

	// Check that subscription is still present and now has 1 subscriber.
	sub, ok = broker.subscriptions[subID]
	require.True(t, ok, "broker missing subscription")
	require.Equal(t, 1, len(sub.subscribers), "subscription should have 1 subecribers")
	// Check that subscriber 1 is gone and subscriber 2 remains.
	_, ok = sub.subscribers[sess1]
	require.False(t, ok, "subscription should not have unsubscribed session 1")
	_, ok = sub.subscribers[sess2]
	require.True(t, ok, "subscription missing session 2")
	// Check that topic->subscription remains
	_, ok = broker.topicSubscription[testTopic]
	require.True(t, ok, "topic subscription was deleted but subscription exists")
	_, ok = broker.sessionSubIDSet[sess1]
	require.False(t, ok, "session 1 subscription ID set still exists")
	_, ok = broker.sessionSubIDSet[sess2]
	require.True(t, ok, "session 2 subscription ID set missing")

	// Test unsubscribing session2 from invalid subscription ID.
	broker.unsubscribe(sess2, &wamp.Unsubscribe{Request: 124, Subscription: 747})
	// Check that session received ERROR message.
	rsp = <-sess2.Recv()
	_, ok = rsp.(*wamp.Error)
	require.True(t, ok, "expected ERROR")

	// Test unsubscribing session2 from topic.
	broker.unsubscribe(sess2, &wamp.Unsubscribe{Request: 124, Subscription: subID})
	// Check that session received UNSUBSCRIBED message.
	rsp = <-sess2.Recv()
	_, ok = rsp.(*wamp.Unsubscribed)
	require.True(t, ok, "expected UNSUBSCRIBED")

	// Check the broker removed subscription.
	_, ok = broker.subscriptions[subID]
	require.False(t, ok, "subscription still exists")
	_, ok = broker.topicSubscription[testTopic]
	require.False(t, ok, "topic subscription still exists")
	_, ok = broker.sessionSubIDSet[sess1]
	require.False(t, ok, "session 1 subscription ID set still exists")
	_, ok = broker.sessionSubIDSet[sess2]
	require.False(t, ok, "session 2 subscription ID set still exists")
}

func TestRemove(t *testing.T) {
	// Subscribe to topic
	broker := newTestBroker(t, nil)
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
	require.False(t, ok, "subscription still exists")
	_, ok = broker.topicSubscription[testTopic]
	require.False(t, ok, "topic subscriber still exists")
	_, ok = broker.subscriptions[subID2]
	require.False(t, ok, "subscription still exists")
	_, ok = broker.topicSubscription[testTopic2]
	require.False(t, ok, "topic subscriber still exists")
	_, ok = broker.sessionSubIDSet[sess]
	require.False(t, ok, "session subscription ID set still exists")
}

func TestBasicPubSub(t *testing.T) {
	broker := newTestBroker(t, nil)
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
	require.True(t, ok, "expected SUBSCRIBED")

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, 0, nil, nil)
	broker.publish(pubSess, &wamp.Publish{Request: 124, Topic: testTopic,
		Arguments: wamp.List{"hello world"}})
	rsp = <-sess.Recv()
	evt, ok := rsp.(*wamp.Event)
	require.True(t, ok, "expected EVENT")
	require.NotZero(t, len(evt.Arguments), "missing event payload")
	arg, _ := wamp.AsString(evt.Arguments[0])
	require.Equal(t, "hello world", arg, "wrong argument value in payload")
}

// ----- WAMP v.2 Testing -----

func TestAdvancedPubSub(t *testing.T) {
	broker := newTestBroker(t, nil)
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
	require.True(t, ok, "expected ERROR")

	msg = &wamp.Subscribe{
		Request: 123,
		Topic:   testTopic,
		Options: wamp.Dict{},
	}
	broker.subscribe(sess, msg)

	// Test that subscriber received SUBSCRIBED message
	rsp = <-sess.Recv()
	_, ok = rsp.(*wamp.Subscribed)
	require.True(t, ok, "expected SUBSCRIBED")

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, 0, nil, nil)

	options := wamp.Dict{
		wamp.OptAcknowledge: true,
	}
	broker.publish(pubSess, &wamp.Publish{Request: 125, Topic: "invalid!@#@#$topicuri", Options: options})
	rsp = <-pubSess.Recv()
	_, ok = rsp.(*wamp.Error)
	require.True(t, ok, "expected ERROR")
}

func TestPPTPubSub(t *testing.T) {
	broker := newTestBroker(t, nil)
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
	require.True(t, ok, "expected SUBSCRIBED")
	subID := subMsg.Subscription
	require.NotZero(t, subID, "invalid suvscription ID")

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, 0, nil, nil)

	options := wamp.Dict{
		wamp.OptAcknowledge: true,
	}
	broker.publish(pubSess, &wamp.Publish{Request: 125, Topic: "invalid!@#@#$topicuri", Options: options})
	rsp = <-pubSess.Recv()
	_, ok = rsp.(*wamp.Error)
	require.True(t, ok, "expected ERROR")

	options = wamp.Dict{
		wamp.OptPPTScheme:     "x_custom",
		wamp.OptPPTSerializer: "native",
	}
	broker.publish(pubSess, &wamp.Publish{Request: 126, Topic: testTopic, Options: options})
	rsp = <-pubSess.Recv()
	_, ok = rsp.(*wamp.Abort)
	require.True(t, ok, "expected ABORT")

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
	require.True(t, ok, "expected EVENT")
}

func TestPrefixPatternBasedSubscription(t *testing.T) {
	// Test match=prefix
	broker := newTestBroker(t, nil)
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
	require.True(t, ok, "expected SUBSCRIBED")
	subID := subMsg.Subscription
	require.NotZero(t, subID, "invalid suvscription ID")

	// Check that broker created subscription.
	sub, ok := broker.subscriptions[subID]
	require.True(t, ok, "broker missing subscription")
	_, ok = sub.subscribers[sess]
	require.True(t, ok, "broker missing subscriber on subscription")
	require.Equal(t, testTopicPfx, sub.topic, "subscription to wrong topic")
	_, ok = broker.pfxTopicSubscription[testTopicPfx]
	require.True(t, ok, "broker missing subscribers for topic")
	_, ok = broker.sessionSubIDSet[sess]
	require.True(t, ok, "broker missing subscriber ID for session")

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, 0, nil, nil)
	broker.publish(pubSess, &wamp.Publish{Request: 124, Topic: testTopic})
	rsp = <-sess.Recv()
	evt, ok := rsp.(*wamp.Event)
	require.True(t, ok, "expected EVENT")
	_topic, ok := evt.Details["topic"]
	require.True(t, ok, "event missing topic")
	topic := _topic.(wamp.URI)
	require.Equal(t, testTopic, topic, "wrong topic received")
}

func TestWildcardPatternBasedSubscription(t *testing.T) {
	// Test match=prefix
	broker := newTestBroker(t, nil)
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
	require.True(t, ok, "expected SUBSCRIBED")
	subID := subMsg.Subscription
	require.NotZero(t, subID, "invalid suvscription ID")

	// Check that broker created subscription.
	sub, ok := broker.subscriptions[subID]
	require.True(t, ok, "broker missing subscription")
	require.Equal(t, subID, sub.id, "broker subscriptions has subscription with wrong ID")
	_, ok = sub.subscribers[sess]
	require.True(t, ok, "broker missing subscriber on subscription")
	require.Equal(t, testTopicWc, sub.topic, "subscription to wrong topic")
	require.Equal(t, "wildcard", sub.match, "subscription has wrong match policy")
	sub2, ok := broker.wcTopicSubscription[testTopicWc]
	require.True(t, ok, "broker missing subscription for topic")
	require.Equal(t, subID, sub2.id, "topic->session has wrong session")
	_, ok = broker.sessionSubIDSet[sess]
	require.True(t, ok, "broker missing subscriber ID for session")

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, 0, nil, nil)
	broker.publish(pubSess, &wamp.Publish{Request: 124, Topic: testTopic})
	rsp = <-sess.Recv()
	evt, ok := rsp.(*wamp.Event)
	require.True(t, ok, "expected EVENT")
	_topic, ok := evt.Details["topic"]
	require.True(t, ok, "event missing topic")
	topic := _topic.(wamp.URI)
	require.Equal(t, testTopic, topic, "wrong topic received")
}

func TestSubscriberBlackWhiteListing(t *testing.T) {
	broker := newTestBroker(t, nil)
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
	require.True(t, ok, "expected SUBSCRIBED")

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
	_, err := wamp.RecvTimeout(sess, time.Second)
	require.NoError(t, err, "not allowed by whitelist")

	// Test whitelist authrole
	broker.publish(pubSess, &wamp.Publish{
		Request: 125,
		Topic:   testTopic,
		Options: wamp.Dict{"eligible_authrole": wamp.List{"admin"}},
	})
	_, err = wamp.RecvTimeout(sess, time.Second)
	require.NoError(t, err, "not allowed by authrole whitelist")

	// Test whitelist authid
	broker.publish(pubSess, &wamp.Publish{
		Request: 126,
		Topic:   testTopic,
		Options: wamp.Dict{"eligible_authid": wamp.List{"jdoe"}},
	})
	_, err = wamp.RecvTimeout(sess, time.Second)
	require.NoError(t, err, "not allowed by authid whitelist")

	// Test blacklist.
	broker.publish(pubSess, &wamp.Publish{
		Request: 127,
		Topic:   testTopic,
		Options: wamp.Dict{"exclude": wamp.List{sess.ID}},
	})
	_, err = wamp.RecvTimeout(sess, time.Second)
	require.Error(t, err, "not excluded by blacklist")

	// Test blacklist authrole
	broker.publish(pubSess, &wamp.Publish{
		Request: 128,
		Topic:   testTopic,
		Options: wamp.Dict{"exclude_authrole": wamp.List{"admin"}},
	})
	_, err = wamp.RecvTimeout(sess, time.Second)
	require.Error(t, err, "not excluded by authrole blacklist")

	// Test blacklist authid
	broker.publish(pubSess, &wamp.Publish{
		Request: 129,
		Topic:   testTopic,
		Options: wamp.Dict{"exclude_authid": wamp.List{"jdoe"}},
	})
	_, err = wamp.RecvTimeout(sess, time.Second)
	require.Error(t, err, "not excluded by authid blacklist")

	// Test that blacklist takes precedence over whitelist.
	broker.publish(pubSess, &wamp.Publish{
		Request: 126,
		Topic:   testTopic,
		Options: wamp.Dict{"eligible_authid": []string{"jdoe"},
			"exclude_authid": []string{"jdoe"}},
	})
	_, err = wamp.RecvTimeout(sess, time.Second)
	require.Error(t, err, "should have been excluded by blacklist")
}

func TestPublisherExclusion(t *testing.T) {
	broker := newTestBroker(t, nil)
	subscriber := newTestPeer()
	sess := wamp.NewSession(subscriber, 0, nil, nil)
	testTopic := wamp.URI("nexus.test.topic")

	broker.subscribe(sess, &wamp.Subscribe{Request: 123, Topic: testTopic})

	// Test that subscriber received SUBSCRIBED message
	rsp, err := wamp.RecvTimeout(sess, time.Second)
	require.NoError(t, err, "subscribe session did not get response to SUBSCRIBE")
	_, ok := rsp.(*wamp.Subscribed)
	require.True(t, ok, "expected SUBSCRIBED")

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
	require.NoError(t, err, "publish session did not get response to SUBSCRIBE")
	_, ok = rsp.(*wamp.Subscribed)
	require.True(t, ok, "expected SUBSCRIBED")

	// Publish message with exclud_me = false.
	broker.publish(pubSess, &wamp.Publish{
		Request: 124,
		Topic:   testTopic,
		Options: wamp.Dict{"exclude_me": false},
	})
	_, err = wamp.RecvTimeout(sess, time.Second)
	require.NoError(t, err, "subscriber did not receive event")
	_, err = wamp.RecvTimeout(pubSess, time.Second)
	require.NoError(t, err, "pub session should have received event")

	// Publish message with exclud_me = true.
	broker.publish(pubSess, &wamp.Publish{
		Request: 124,
		Topic:   testTopic,
		Options: wamp.Dict{"exclude_me": true},
	})
	_, err = wamp.RecvTimeout(sess, time.Second)
	require.NoError(t, err, "subscriber did not receive event")
	_, err = wamp.RecvTimeout(pubSess, time.Second)
	require.Error(t, err, "pub session should NOT have received event")
}

func TestPublisherIdentification(t *testing.T) {
	broker := newTestBroker(t, nil)
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
	require.True(t, ok, "expected SUBSCRIBED")

	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, wamp.GlobalID(), nil, nil)
	broker.publish(pubSess, &wamp.Publish{
		Request: 124,
		Topic:   testTopic,
		Options: wamp.Dict{"disclose_me": true},
	})
	rsp = <-sess.Recv()
	evt, ok := rsp.(*wamp.Event)
	require.True(t, ok, "expected EVENT")
	pub, ok := evt.Details["publisher"]
	require.True(t, ok, "missing publisher ID")
	require.Equal(t, pubSess.ID, pub.(wamp.ID), "incorrect publisher ID disclosed")
}

func TestEventHistory(t *testing.T) {
	eventHistoryConfig := []*TopicEventHistoryConfig{
		{
			Topic:       wamp.URI("nexus.test.exact.topic"),
			MatchPolicy: "exact",
			Limit:       3,
		},
		{
			Topic:       wamp.URI("nexus.test"),
			MatchPolicy: "prefix",
			Limit:       4,
		},
		{
			Topic:       wamp.URI("nexus.test..topic"),
			MatchPolicy: "wildcard",
			Limit:       4,
		},
		{
			Topic:       wamp.URI("nexus"),
			MatchPolicy: "prefix",
			Limit:       1000,
		},
	}

	broker := newTestBroker(t, eventHistoryConfig)
	publisher := newTestPeer()
	pubSess := wamp.NewSession(publisher, 0, nil, nil)

	topics := []wamp.URI{"nexus.test.exact.topic", "nexus.test.prefix.catch", "nexus.test.wildcard.topic", "nexus.test.wildcard.miss"}
	reqId := 25501

	// Let's publish all payloads to all topics to have a data to check
	for i := 1; i <= 5; i++ {
		for _, topic := range topics {

			publication := wamp.Publish{
				Request:     wamp.ID(reqId),
				Topic:       topic,
				Arguments:   wamp.List{reqId},
				ArgumentsKw: wamp.Dict{"topic": string(topic)},
			}
			broker.publish(pubSess, &publication)
			reqId++
		}
	}

	// and let's publish some events that should not be saved in event store
	for _, topic := range topics {

		publication := wamp.Publish{
			Request:     wamp.ID(reqId),
			Topic:       topic,
			Options:     wamp.Dict{"exclude": 12345},
			Arguments:   wamp.List{reqId},
			ArgumentsKw: wamp.Dict{"topic": string(topic)},
		}
		broker.publish(pubSess, &publication)
		reqId++
	}

	// Now let's examine what is stored in the Event Store
	topic := wamp.URI("nexus.test.exact.topic")
	subscription := broker.topicSubscription[topic]
	subEvents := broker.eventHistoryStore[subscription].entries
	require.Equalf(t, 3, len(subEvents), "Store for topic %s should hold 3 records", topic)
	require.Truef(t, broker.eventHistoryStore[subscription].isLimitReached, "Limit for the store for topic %s should be reached", topic)
	require.Equalf(t, "nexus.test.exact.topic", subEvents[0].event.ArgumentsKw["topic"], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, 25509, subEvents[0].event.Arguments[0], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, "nexus.test.exact.topic", subEvents[1].event.ArgumentsKw["topic"], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, 25513, subEvents[1].event.Arguments[0], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, "nexus.test.exact.topic", subEvents[2].event.ArgumentsKw["topic"], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, 25517, subEvents[2].event.Arguments[0], "Event store for topic %s holds invalid event", topic)

	topic = wamp.URI("nexus.test")
	subscription = broker.pfxTopicSubscription[topic]
	subEvents = broker.eventHistoryStore[subscription].entries
	require.Equalf(t, 4, len(subEvents), "Store for topic %s should hold 3 records", topic)
	require.Truef(t, broker.eventHistoryStore[subscription].isLimitReached, "Limit for the store for topic %s should be reached", topic)
	require.Equalf(t, "nexus.test.exact.topic", subEvents[0].event.ArgumentsKw["topic"], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, 25517, subEvents[0].event.Arguments[0], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, "nexus.test.prefix.catch", subEvents[1].event.ArgumentsKw["topic"], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, 25518, subEvents[1].event.Arguments[0], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, "nexus.test.wildcard.topic", subEvents[2].event.ArgumentsKw["topic"], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, 25519, subEvents[2].event.Arguments[0], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, "nexus.test.wildcard.miss", subEvents[3].event.ArgumentsKw["topic"], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, 25520, subEvents[3].event.Arguments[0], "Event store for topic %s holds invalid event", topic)

	topic = wamp.URI("nexus.test..topic")
	subscription = broker.wcTopicSubscription[topic]
	subEvents = broker.eventHistoryStore[subscription].entries
	require.Equalf(t, 4, len(subEvents), "Store for topic %s should hold 3 records", topic)
	require.Truef(t, broker.eventHistoryStore[subscription].isLimitReached, "Limit for the store for topic %s should be reached", topic)
	require.Equalf(t, "nexus.test.exact.topic", subEvents[0].event.ArgumentsKw["topic"], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, 25513, subEvents[0].event.Arguments[0], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, "nexus.test.wildcard.topic", subEvents[1].event.ArgumentsKw["topic"], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, 25515, subEvents[1].event.Arguments[0], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, "nexus.test.exact.topic", subEvents[2].event.ArgumentsKw["topic"], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, 25517, subEvents[2].event.Arguments[0], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, "nexus.test.wildcard.topic", subEvents[3].event.ArgumentsKw["topic"], "Event store for topic %s holds invalid event", topic)
	require.Equalf(t, 25519, subEvents[3].event.Arguments[0], "Event store for topic %s holds invalid event", topic)

	topic = wamp.URI("nexus")
	subscription = broker.pfxTopicSubscription[topic]
	subEvents = broker.eventHistoryStore[subscription].entries
	require.Equalf(t, 20, len(subEvents), "Store for topic %s should hold 20 records", topic)
	require.Falsef(t, broker.eventHistoryStore[subscription].isLimitReached, "Limit for the store for topic %s should not be reached", topic)

	//Now let's test Event History MetaRPCs
	topic = wamp.URI("nexus.test.exact.topic")
	subId := broker.topicSubscription[topic].id
	inv := wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"limit": 10},
	}
	reqId++

	msg := broker.subEventHistory(&inv)
	yield, ok := msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC eventHistoryLast for topic %s should return Yield message", topic)
	require.Equalf(t, 3, len(yield.Arguments), "MetaRPC eventHistoryLast for topic %s should return 3 records", topic)
	limitReachedVal := yield.ArgumentsKw["is_limit_reached"]
	limitReached, _ := wamp.AsBool(limitReachedVal)
	require.Truef(t, limitReached, "is_limit_reached for topic %s should be true", topic)

	topic = wamp.URI("nexus")
	subId = broker.pfxTopicSubscription[topic].id
	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"limit": 10},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC eventHistoryLast for topic %s should return Yield message", topic)
	require.Equalf(t, 10, len(yield.Arguments), "MetaRPC eventHistoryLast for topic %s should return 10 records", topic)
	limitReachedVal = yield.ArgumentsKw["is_limit_reached"]
	limitReached, _ = wamp.AsBool(limitReachedVal)
	require.Falsef(t, limitReached, "is_limit_reached for topic %s should be false", topic)

	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"limit": 1000},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC eventHistoryLast for topic %s should return Yield message", topic)
	require.Equalf(t, 20, len(yield.Arguments), "MetaRPC eventHistoryLast for topic %s should return 20 records", topic)

	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"after_time": (time.Now().Add(-1 * time.Hour)).Format(time.RFC3339)},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Yield message", topic)
	require.Equalf(t, 20, len(yield.Arguments), "MetaRPC subEventHistory for topic %s should return 20 records", topic)

	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"from_time": (time.Now().Add(-1 * time.Hour)).Format(time.RFC3339)},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Yield message", topic)
	require.Equalf(t, 20, len(yield.Arguments), "MetaRPC subEventHistory for topic %s should return 20 records", topic)

	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"before_time": (time.Now().Add(1 * time.Hour)).Format(time.RFC3339)},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Yield message", topic)
	require.Equalf(t, 20, len(yield.Arguments), "MetaRPC subEventHistory for topic %s should return 20 records", topic)

	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"until_time": (time.Now().Add(1 * time.Hour)).Format(time.RFC3339)},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Yield message", topic)
	require.Equalf(t, 20, len(yield.Arguments), "MetaRPC subEventHistory for topic %s should return 20 records", topic)

	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw: wamp.Dict{
			"after_time":  (time.Now().Add(-1 * time.Hour)).Format(time.RFC3339),
			"before_time": (time.Now().Add(1 * time.Hour)).Format(time.RFC3339),
		},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Yield message", topic)
	require.Equalf(t, 20, len(yield.Arguments), "MetaRPC subEventHistory for topic %s should return 20 records", topic)

	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"topic": "nexus.test.exact.topic"},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Yield message", topic)
	require.Equalf(t, 5, len(yield.Arguments), "MetaRPC subEventHistory for topic %s should return 5 records", topic)

	// Let's test filtering based on publication ID
	topic = wamp.URI("nexus")
	subscription = broker.pfxTopicSubscription[topic]
	pubId := broker.eventHistoryStore[subscription].entries[4].event.Publication
	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"from_publication": pubId},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Yield message", topic)
	require.Equalf(t, 16, len(yield.Arguments), "MetaRPC subEventHistory for topic %s should return 16 records", topic)

	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"after_publication": pubId},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Yield message", topic)
	require.Equalf(t, 15, len(yield.Arguments), "MetaRPC subEventHistory for topic %s should return 15 records", topic)

	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"before_publication": pubId},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Yield message", topic)
	require.Equalf(t, 4, len(yield.Arguments), "MetaRPC subEventHistory for topic %s should return 4 records", topic)

	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"until_publication": pubId},
	}
	reqId++

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Yield message", topic)
	require.Equalf(t, 5, len(yield.Arguments), "MetaRPC subEventHistory for topic %s should return 5 records", topic)

	// Let's test reverse order
	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{subId},
		ArgumentsKw:  wamp.Dict{"until_publication": pubId, "reverse": true, "limit": 3},
	}

	msg = broker.subEventHistory(&inv)
	yield, ok = msg.(*wamp.Yield)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Yield message", topic)
	require.Equalf(t, 3, len(yield.Arguments), "MetaRPC subEventHistory for topic %s should return 5 records", topic)
	ev1, ok := yield.Arguments[0].(storedEvent)
	require.True(t, ok, "0 item in Arguments should be of type storedEvent")
	ev2, ok := yield.Arguments[2].(storedEvent)
	require.True(t, ok, "2 item in Arguments should be of type storedEvent")
	require.GreaterOrEqual(t, ev1.Arguments[0].(int), ev2.Arguments[0].(int), "MetaRPC subEventHistory for topic should return records in reverse order")

	// Let's test with zero args
	inv = wamp.Invocation{
		Request:      wamp.ID(reqId),
		Registration: 0,
		Details:      wamp.Dict{},
		Arguments:    wamp.List{},
		ArgumentsKw:  wamp.Dict{},
	}

	msg = broker.subEventHistory(&inv)
	wErr, ok := msg.(*wamp.Error)
	require.Truef(t, ok, "MetaRPC subEventHistory for topic %s should return Error message", topic)
	require.Equal(t, wamp.ErrInvalidArgument, wErr.Error, "Error URI must be ErrInvalidArgument when MetaRPC subEventHistory called without arguments")
}
