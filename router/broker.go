package router

import (
	"fmt"

	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/wamp"
)

const (
	rolePub = "publisher"
	roleSub = "subscriber"

	featurePatternSub           = "pattern_based_subscription"
	featurePubExclusion         = "publisher_exclusion"
	featurePubIdent             = "publisher_identification"
	featureSubBlackWhiteListing = "subscriber_blackwhite_listing"
	featureSubMetaAPI           = "subscription_meta_api"

	detailTopic = "topic"
)

// Role information for this broker.
var brokerRole = wamp.Dict{
	"features": wamp.Dict{
		featurePatternSub:           true,
		featurePubExclusion:         true,
		featurePubIdent:             true,
		featureSessionMetaAPI:       true,
		featureSubBlackWhiteListing: true,
		featureSubMetaAPI:           true,
	},
}

// subscription manages all the subscribers to a particular topic.
type subscription struct {
	id          wamp.ID  // subscription ID
	topic       wamp.URI // topic URI
	match       string   // match policy
	created     string   // when subscription was created
	subscribers map[*wamp.Session]struct{}
}

type broker struct {
	// topic -> subscription
	topicSubscription    map[wamp.URI]*subscription
	pfxTopicSubscription map[wamp.URI]*subscription
	wcTopicSubscription  map[wamp.URI]*subscription

	// subscription ID -> subscription
	subscriptions map[wamp.ID]*subscription

	// Session -> subscription ID set
	sessionSubIDSet map[*wamp.Session]map[wamp.ID]struct{}

	actionChan chan func()

	// Generate subscription IDs.
	idGen *wamp.IDGen

	strictURI     bool
	allowDisclose bool

	log           stdlog.StdLog
	debug         bool
	filterFactory FilterFactory
}

// newBroker returns a new default broker implementation instance.
func newBroker(logger stdlog.StdLog, strictURI, allowDisclose, debug bool, publishFilter FilterFactory) *broker {
	if logger == nil {
		panic("logger is nil")
	}
	if publishFilter == nil {
		publishFilter = NewSimplePublishFilter
	}
	b := &broker{
		topicSubscription:    map[wamp.URI]*subscription{},
		pfxTopicSubscription: map[wamp.URI]*subscription{},
		wcTopicSubscription:  map[wamp.URI]*subscription{},

		subscriptions:   map[wamp.ID]*subscription{},
		sessionSubIDSet: map[*wamp.Session]map[wamp.ID]struct{}{},

		// The action handler should be nearly always runable, since it is the
		// critical section that does the only routing.  So, and unbuffered
		// channel is appropriate.
		actionChan: make(chan func()),

		idGen: new(wamp.IDGen),

		strictURI:     strictURI,
		allowDisclose: allowDisclose,

		log:           logger,
		debug:         debug,
		filterFactory: publishFilter,
	}
	go b.run()
	return b
}

// role returns the role information for the "broker" role.  The data returned
// is suitable for use as broker role info in a WELCOME message.
func (b *broker) role() wamp.Dict {
	return brokerRole
}

// publish finds all subscriptions for the topic being published to, including
// those matching the topic by pattern, and sends an event to the subscribers
// of that topic.
//
// When a single event matches more than one of a Subscriber's subscriptions,
// the event will be delivered for each subscription.
//
// The Subscriber can detect the delivery of that same event on multiple
// subscriptions via EVENT.PUBLISHED.Publication, which will be identical.
func (b *broker) publish(pub *wamp.Session, msg *wamp.Publish) {
	if pub == nil || msg == nil {
		panic("broker.Publish with nil session or message")
	}
	// Send a publish error only when pubAck is set.
	pubAck, _ := msg.Options[wamp.OptAcknowledge].(bool)

	// Validate URI.  For PUBLISH, must be valid URI (either strict or loose),
	// and all URI components must be non-empty.

	if !msg.Topic.ValidURI(b.strictURI, "") {
		if !pubAck {
			return
		}
		errMsg := fmt.Sprintf(
			"publish with invalid topic URI %v (URI strict checking %v)",
			msg.Topic, b.strictURI)
		b.trySend(pub, &wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Error:     wamp.ErrInvalidURI,
			Arguments: wamp.List{errMsg},
		})
		return
	}

	excludePub := true
	if exclude, ok := msg.Options[wamp.OptExcludeMe].(bool); ok {
		excludePub = exclude
	}

	// A Broker may also (automatically) disclose the identity of a
	// publisher even without the publisher having explicitly requested to
	// do so when the Broker configuration (for the publication topic) is
	// set up to do so.  TODO: Currently no broker config for this.
	var disclose bool
	if opt, _ := msg.Options[wamp.OptDiscloseMe].(bool); opt {
		// Broker MAY deny a publisher's request to disclose its identity.
		if !b.allowDisclose {
			if pubAck {
				b.trySend(pub, &wamp.Error{
					Type:    msg.MessageType(),
					Request: msg.Request,
					Details: wamp.Dict{},
					Error:   wamp.ErrOptionDisallowedDiscloseMe,
				})
			}
			// When the publisher requested disclosure, but it isn't
			// allowed, don't continue to publish the message.
			return
		}
		disclose = true
	}
	pubID := wamp.GlobalID()

	// Get blacklists and whitelists, if any, from publish message.
	filter := b.filterFactory(msg)

	b.actionChan <- func() {
		b.syncPublish(pub, msg, pubID, excludePub, disclose, filter)
	}

	// Send PUBLISHED message if acknowledge is present and true.
	if pubAck {
		b.trySend(pub, &wamp.Published{Request: msg.Request, Publication: pubID})
	}
}

// subscribe subscribes the client to the given topic.
//
// In case of receiving a SUBSCRIBE message from the same Subscriber and to
// already subscribed topic, Broker should answer with SUBSCRIBED message,
// containing the existing Subscription|id.
//
// By default, Subscribers subscribe to topics with exact matching policy. A
// Subscriber might want to subscribe to topics based on a pattern.  If the
// Broker and the Subscriber support pattern-based subscriptions, this matching
// can happen by prefix-matching policy or wildcard-matching policy.
func (b *broker) subscribe(sub *wamp.Session, msg *wamp.Subscribe) {
	if sub == nil || msg == nil {
		panic("broker.Subscribe with nil session or message")
	}
	// Validate topic URI.  For SUBSCRIBE, must be valid URI (either strict or
	// loose), and all URI components must be non-empty for normal
	// subscriptions, may be empty for wildcard subscriptions and must be
	// non-empty for all but the last component for prefix subscriptions.
	match, _ := wamp.AsString(msg.Options[wamp.OptMatch])
	if !msg.Topic.ValidURI(b.strictURI, match) {
		errMsg := fmt.Sprintf(
			"subscribe for invalid topic URI %v (URI strict checking %v)",
			msg.Topic, b.strictURI)
		b.trySend(sub, &wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Error:     wamp.ErrInvalidURI,
			Arguments: wamp.List{errMsg},
		})
		return
	}

	b.actionChan <- func() {
		b.syncSubscribe(sub, msg, match)
	}
}

// unsubscribe removes the requested subscription.
func (b *broker) unsubscribe(sub *wamp.Session, msg *wamp.Unsubscribe) {
	if sub == nil || msg == nil {
		panic("broker.Unsubscribe with nil session or message")
	}
	b.actionChan <- func() {
		b.syncUnsubscribe(sub, msg)
	}
}

// removeSession removes all subscriptions of the subscriber.  This is called
// when a client leaves the realm by sending a GOODBYE message or by
// disconnecting from the router.  If there are any subscriptions for this
// session a wamp.subscription.on_delete meta event is published for each.
func (b *broker) removeSession(sess *wamp.Session) {
	if sess == nil {
		return
	}
	b.actionChan <- func() {
		b.syncRemoveSession(sess)
	}
}

// Close stops the broker, letting already queued actions finish.
func (b *broker) close() {
	close(b.actionChan)
}

func (b *broker) run() {
	for action := range b.actionChan {
		action()
	}
	if b.debug {
		b.log.Print("Broker stopped")
	}
}

func (b *broker) syncPublish(pub *wamp.Session, msg *wamp.Publish, pubID wamp.ID, excludePub, disclose bool, filter PublishFilter) {
	// Publish to subscribers with exact match.
	if sub, ok := b.topicSubscription[msg.Topic]; ok {
		b.syncPubEvent(pub, msg, pubID, sub, excludePub, false, disclose, filter)
	}

	// Publish to subscribers with prefix match.
	for pfxTopic, sub := range b.pfxTopicSubscription {
		if msg.Topic.PrefixMatch(pfxTopic) {
			b.syncPubEvent(pub, msg, pubID, sub, excludePub, true, disclose, filter)
		}
	}

	// Publish to subscribers with wildcard match.
	for wcTopic, sub := range b.wcTopicSubscription {
		if msg.Topic.WildcardMatch(wcTopic) {
			b.syncPubEvent(pub, msg, pubID, sub, excludePub, true, disclose, filter)
		}
	}
}

func newSubscription(id wamp.ID, subscriber *wamp.Session, topic wamp.URI, match string) *subscription {
	return &subscription{
		id:          id,
		topic:       topic,
		match:       match,
		created:     wamp.NowISO8601(),
		subscribers: map[*wamp.Session]struct{}{subscriber: struct{}{}},
	}
}

func (b *broker) syncSubscribe(subscriber *wamp.Session, msg *wamp.Subscribe, match string) {
	var sub *subscription
	var existingSub bool

	switch match {
	case wamp.MatchPrefix:
		// Subscribe to any topic that matches by the given prefix URI
		sub, existingSub = b.pfxTopicSubscription[msg.Topic]
		if !existingSub {
			// Create a new prefix subscription.
			sub = newSubscription(b.idGen.Next(), subscriber, msg.Topic, match)
			b.pfxTopicSubscription[msg.Topic] = sub
		}
	case wamp.MatchWildcard:
		// Subscribe to any topic that matches by the given wildcard URI.
		sub, existingSub = b.wcTopicSubscription[msg.Topic]
		if !existingSub {
			// Create a new wildcard subscription.
			sub = newSubscription(b.idGen.Next(), subscriber, msg.Topic, match)
			b.wcTopicSubscription[msg.Topic] = sub
		}
	default:
		// Subscribe to the topic that exactly matches the given URI.
		sub, existingSub = b.topicSubscription[msg.Topic]
		if !existingSub {
			// Create a new subscription.
			sub = newSubscription(b.idGen.Next(), subscriber, msg.Topic, match)
			b.topicSubscription[msg.Topic] = sub
		}
	}
	b.subscriptions[sub.id] = sub

	// If the topic already has subscribers, then see if the session requesting
	// a subscription is already subscribed to the topic.
	if existingSub {
		if _, already := sub.subscribers[subscriber]; already {
			// Already subscribed; send existing subscription ID.
			b.trySend(subscriber, &wamp.Subscribed{
				Request:      msg.Request,
				Subscription: sub.id,
			})
			return
		}
		// Add subscriber to existing subscription.
		sub.subscribers[subscriber] = struct{}{}
	}

	// Add the subscription ID to the set of subscriptions for the subscriber.
	subIdSet, ok := b.sessionSubIDSet[subscriber]
	if !ok {
		// This subscriber does not have any other subscriptions, so new set.
		subIdSet = map[wamp.ID]struct{}{}
		b.sessionSubIDSet[subscriber] = subIdSet
	}
	subIdSet[sub.id] = struct{}{}

	// Tell sender the new subscription ID.
	b.trySend(subscriber, &wamp.Subscribed{Request: msg.Request, Subscription: sub.id})

	if !existingSub {
		b.syncPubSubCreateMeta(msg.Topic, subscriber.ID, sub)
	}

	// Publish WAMP on_subscribe meta event.
	b.syncPubSubMeta(wamp.MetaEventSubOnSubscribe, subscriber.ID, sub.id)
}

// syncDeleteSubscription removes the the ID->subscription mapping and removes
// the topic->subscription mapping.
func (b *broker) syncDelSubscription(sub *subscription) {
	// Remove ID -> subscription.
	delete(b.subscriptions, sub.id)

	// Delete topic -> subscription
	switch sub.match {
	case wamp.MatchPrefix:
		delete(b.pfxTopicSubscription, sub.topic)
	case wamp.MatchWildcard:
		delete(b.wcTopicSubscription, sub.topic)
	default:
		delete(b.topicSubscription, sub.topic)
	}
}

// syncUnsibsubscribe removes the subscriber from the specified subscription.
func (b *broker) syncUnsubscribe(subscriber *wamp.Session, msg *wamp.Unsubscribe) {
	subID := msg.Subscription
	sub, ok := b.subscriptions[subID]
	if !ok {
		b.trySend(subscriber, &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Error:   wamp.ErrNoSuchSubscription,
		})
		b.log.Println("Error unsubscribing: no such subscription", subID)
		return
	}

	// Remove subscribed session from subscription.
	delete(sub.subscribers, subscriber)

	// If no more subscribers on this subscription, delete subscription and
	// send on_delete meta event.
	var delLastSub bool
	if len(sub.subscribers) == 0 {
		b.syncDelSubscription(sub)
		delLastSub = true
	}

	// Clean up subscriber's subscription ID set.
	if subIDSet, ok := b.sessionSubIDSet[subscriber]; !ok {
		b.log.Print("Error unsubscribing: no subscriptions for sender")
	} else if _, ok := subIDSet[subID]; !ok {
		b.log.Println("Error unsubscribing: no such subscription for sender:",
			subID)
	} else {
		delete(subIDSet, subID)
		// If subscriber has no remaining subscriptions.
		if len(subIDSet) == 0 {
			// Remove subscribers subscription ID set.
			delete(b.sessionSubIDSet, subscriber)
		}
	}

	// Tell sender they are unsubscribed.
	b.trySend(subscriber, &wamp.Unsubscribed{Request: msg.Request})

	// Publish WAMP unsubscribe meta event.
	b.syncPubSubMeta(wamp.MetaEventSubOnUnsubscribe, subscriber.ID, subID)
	if delLastSub {
		// Fired when a subscription is deleted after the last session attached
		// to it has been removed.
		b.syncPubSubMeta(wamp.MetaEventSubOnDelete, subscriber.ID, subID)
	}
}

// syncRemoveSession removed all subscriptions for the session.
func (b *broker) syncRemoveSession(subscriber *wamp.Session) {
	subIDSet, ok := b.sessionSubIDSet[subscriber]
	if !ok {
		return
	}
	delete(b.sessionSubIDSet, subscriber)

	// For each subscription ID, lookup the subscription and remove the
	// subscriber from the subscription.  If there are no more subscribers on
	// the subscription, then delete the subscription.
	var sub *subscription
	for subID := range subIDSet {
		sub, ok = b.subscriptions[subID]
		if !ok {
			continue
		}
		// Remove subscribed session from subscription.
		delete(sub.subscribers, subscriber)

		// If no more subscribers on this subscription.
		if len(sub.subscribers) == 0 {
			b.syncDelSubscription(sub)
			// Fired when a subscription is deleted after the last
			// session attached to it has been removed.
			b.syncPubSubMeta(wamp.MetaEventSubOnDelete, subscriber.ID, subID)
		}
	}
}

// syncPubEvent sends an event to all subscribers that are not excluded from
// receiving the event.
func (b *broker) syncPubEvent(pub *wamp.Session, msg *wamp.Publish, pubID wamp.ID, sub *subscription, excludePublisher, sendTopic, disclose bool, filter PublishFilter) {
	for subscriber, _ := range sub.subscribers {
		// Do not send event to publisher.
		if subscriber == pub && excludePublisher {
			continue
		}

		// Check if receiver is restricted.
		if filter != nil {
			// Create a safe session to prevent access to the session.Peer.
			safeSession := wamp.Session{
				ID:      subscriber.ID,
				Details: subscriber.Details,
			}
			subscriber.Lock()
			ok := filter.Allowed(&safeSession)
			subscriber.Unlock()
			if !ok {
				continue
			}
		}

		details := wamp.Dict{}

		// If a subscription was established with a pattern-based matching
		// policy, a Broker MUST supply the original PUBLISH.Topic as provided
		// by the Publisher in EVENT.Details.topic|uri.
		if sendTopic {
			details[detailTopic] = msg.Topic
		}

		if disclose && subscriber.HasFeature(roleSub, featurePubIdent) {
			disclosePublisher(pub, details)
		}

		// TODO: Handle publication trust levels

		b.trySend(subscriber, &wamp.Event{
			Publication:  pubID,
			Subscription: sub.id,
			Arguments:    msg.Arguments,
			ArgumentsKw:  msg.ArgumentsKw,
			Details:      details,
		})
	}
}

// syncPubMeta publishes the subscription meta event, using the supplied
// function, to the matching subscribers.
func (b *broker) syncPubMeta(metaTopic wamp.URI, sendMeta func(metaSub *subscription, sendTopic bool)) {
	// Publish to subscribers with exact match.
	if metaSub, ok := b.topicSubscription[metaTopic]; ok {
		sendMeta(metaSub, false)
	}
	// Publish to subscribers with prefix match.
	for pfxTopic, metaSub := range b.pfxTopicSubscription {
		if metaTopic.PrefixMatch(pfxTopic) {
			sendMeta(metaSub, true)
		}
	}
	// Publish to subscribers with wildcard match.
	for wcTopic, metaSub := range b.wcTopicSubscription {
		if metaTopic.WildcardMatch(wcTopic) {
			sendMeta(metaSub, true)
		}
	}
}

// syncPubSubMeta publishes a subscription meta event when a subscription is
// added, removed, or deleted.
func (b *broker) syncPubSubMeta(metaTopic wamp.URI, subSessID, subID wamp.ID) {
	pubID := wamp.GlobalID() // create here so that it is same for all events
	b.syncPubMeta(metaTopic, func(metaSub *subscription, sendTopic bool) {
		if len(metaSub.subscribers) == 0 {
			return
		}
		details := wamp.Dict{}
		if sendTopic {
			details[detailTopic] = metaTopic
		}
		for subscriber := range metaSub.subscribers {
			// Do not send the meta event to the session that is causing the
			// meta event to be generated.  This prevents useless events that
			// could lead to race conditions on the client.
			if subscriber.ID == subSessID {
				continue
			}
			b.trySend(subscriber, &wamp.Event{
				Publication:  pubID,
				Subscription: metaSub.id,
				Details:      details,
				Arguments:    wamp.List{subSessID, subID},
			})
		}
	})
}

// syncPubSubCreateMeta publishes a meta event on subscription creation.
//
// Fired when a subscription is created through a subscription request for a
// topic which was previously without subscribers.
func (b *broker) syncPubSubCreateMeta(topic wamp.URI, subSessID wamp.ID, sub *subscription) {
	pubID := wamp.GlobalID() // create here so that it is same for all events
	b.syncPubMeta(wamp.MetaEventSubOnCreate, func(metaSub *subscription, sendTopic bool) {
		if len(metaSub.subscribers) == 0 {
			return
		}
		details := wamp.Dict{}
		if sendTopic {
			details[detailTopic] = wamp.MetaEventSubOnCreate
		}
		subDetails := wamp.Dict{
			"id":          sub.id,
			"created":     sub.created,
			"uri":         sub.topic,
			wamp.OptMatch: sub.match,
		}

		for subscriber := range metaSub.subscribers {
			// Do not send the meta event to the session that is causing the
			// meta event to be generated.  This prevents useless events that
			// could lead to race conditions on the client.
			if subscriber.ID == subSessID {
				continue
			}
			b.trySend(subscriber, &wamp.Event{
				Publication:  pubID,
				Subscription: metaSub.id,
				Details:      details,
				Arguments:    wamp.List{subSessID, subDetails},
			})
		}
	})
}

func (b *broker) trySend(sess *wamp.Session, msg wamp.Message) bool {
	if err := sess.TrySend(msg); err != nil {
		b.log.Printf("!!! Dropped %s to session %s: %s", msg.MessageType(), sess, err)
		return false
	}
	return true
}

// disclosePublisher adds publisher identity information to EVENT.Details.
func disclosePublisher(pub *wamp.Session, details wamp.Dict) {
	details[rolePub] = pub.ID
	// These values are not required by the specification, but are here for
	// compatibility with Crossbar.
	pub.Lock()
	for _, f := range []string{"authid", "authrole"} {
		if val, ok := pub.Details[f]; ok {
			details[fmt.Sprintf("%s_%s", rolePub, f)] = val
		}
	}
	pub.Unlock()
}

// ----- Subscription Meta Procedure Handlers -----

// subList retrieves subscription IDs listed according to match policies.
func (b *broker) subList(msg *wamp.Invocation) wamp.Message {
	var exactSubs, pfxSubs, wcSubs []wamp.ID
	sync := make(chan struct{})
	b.actionChan <- func() {
		for subID, sub := range b.subscriptions {
			switch sub.match {
			case wamp.MatchPrefix:
				pfxSubs = append(pfxSubs, subID)
			case wamp.MatchWildcard:
				wcSubs = append(wcSubs, subID)
			default:
				exactSubs = append(exactSubs, subID)
			}
		}
		close(sync)
	}
	<-sync
	dict := wamp.Dict{
		wamp.MatchExact:    exactSubs,
		wamp.MatchPrefix:   pfxSubs,
		wamp.MatchWildcard: wcSubs,
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{dict},
	}
}

// subLookup obtains the subscription (if any) managing a topic, according
// to some match policy.
func (b *broker) subLookup(msg *wamp.Invocation) wamp.Message {
	var subID wamp.ID
	if len(msg.Arguments) != 0 {
		if topic, ok := wamp.AsURI(msg.Arguments[0]); ok {
			var match string
			if len(msg.Arguments) > 1 {
				if opts, ok := wamp.AsDict(msg.Arguments[1]); ok {
					match, _ = wamp.AsString(opts[wamp.OptMatch])
				}
			}
			sync := make(chan struct{})
			b.actionChan <- func() {
				var sub *subscription
				var ok bool
				switch match {
				default:
					sub, ok = b.topicSubscription[topic]
				case wamp.MatchPrefix:
					sub, ok = b.pfxTopicSubscription[topic]
				case wamp.MatchWildcard:
					sub, ok = b.wcTopicSubscription[topic]
				}
				if ok {
					subID = sub.id
				}
				close(sync)
			}
			<-sync
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{subID},
	}
}

// subMatch retrieves a list of IDs of subscriptions matching a topic URI,
// irrespective of match policy.
func (b *broker) subMatch(msg *wamp.Invocation) wamp.Message {
	var subIDs []wamp.ID
	if len(msg.Arguments) != 0 {
		if topic, ok := wamp.AsURI(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			b.actionChan <- func() {
				if sub, ok := b.topicSubscription[topic]; ok {
					for subscriber := range sub.subscribers {
						subIDs = append(subIDs, subscriber.ID)
					}
				}
				for pfxTopic, sub := range b.pfxTopicSubscription {
					if topic.PrefixMatch(pfxTopic) {
						for subscriber := range sub.subscribers {
							subIDs = append(subIDs, subscriber.ID)
						}
					}
				}
				for wcTopic, sub := range b.wcTopicSubscription {
					if topic.WildcardMatch(wcTopic) {
						for subscriber := range sub.subscribers {
							subIDs = append(subIDs, subscriber.ID)
						}
					}
				}
				close(sync)
			}
			<-sync
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{subIDs},
	}
}

// subGet retrieves information on a particular subscription.
func (b *broker) subGet(msg *wamp.Invocation) wamp.Message {
	var dict wamp.Dict
	if len(msg.Arguments) != 0 {
		if subID, ok := wamp.AsID(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			b.actionChan <- func() {
				if sub, ok := b.subscriptions[subID]; ok {
					dict = wamp.Dict{
						"id":          subID,
						"created":     sub.created,
						"uri":         sub.topic,
						wamp.OptMatch: sub.match,
					}
				}
				close(sync)
			}
			<-sync
		}
	}
	if dict == nil {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchSubscription,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{dict},
	}
}

// subListSubscribers retrieves a list of session IDs for sessions currently
// attached to the subscription.
func (b *broker) subListSubscribers(msg *wamp.Invocation) wamp.Message {
	var subscriberIDs []wamp.ID
	if len(msg.Arguments) != 0 {
		if subID, ok := wamp.AsID(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			b.actionChan <- func() {
				if sub, ok := b.subscriptions[subID]; ok {
					subscriberIDs = make([]wamp.ID, len(sub.subscribers))
					var i int
					for subscriber := range sub.subscribers {
						subscriberIDs[i] = subscriber.ID
						i++
					}
				}
				close(sync)
			}
			<-sync
		}
	}
	if len(subscriberIDs) == 0 {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchSubscription,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{subscriberIDs},
	}
}

// subCountSubscribers obtains the number of sessions currently attached to the
// subscription.
func (b *broker) subCountSubscribers(msg *wamp.Invocation) wamp.Message {
	var count int
	var ok bool
	if len(msg.Arguments) != 0 {
		var subID wamp.ID
		if subID, ok = wamp.AsID(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			b.actionChan <- func() {
				if sub, found := b.subscriptions[subID]; found {
					count = len(sub.subscribers)
				} else {
					ok = false
				}
				close(sync)
			}
			<-sync
		}
	}
	if !ok {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchSession,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{count},
	}
}
