package router

import (
	"fmt"
	"time"

	"github.com/gammazero/nexus/v3/stdlog"
	"github.com/gammazero/nexus/v3/wamp"
)

const (
	detailTopic = "topic"
)

// Role information for this broker.
var brokerRole = wamp.Dict{
	"features": wamp.Dict{
		wamp.FeaturePatternSub:           true,
		wamp.FeaturePubExclusion:         true,
		wamp.FeaturePubIdent:             true,
		wamp.FeatureSessionMetaAPI:       true,
		wamp.FeatureSubBlackWhiteListing: true,
		wamp.FeatureSubMetaAPI:           true,
		wamp.FeaturePayloadPassthruMode:  true,
		wamp.FeatureEventHistory:         true,
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

// storedEvent is a wrapper around wamp event message with timestamp
// to be used in event history
type storedEvent struct {
	timestamp    time.Time
	Subscription wamp.ID
	Publication  wamp.ID
	Details      wamp.Dict
	Arguments    wamp.List `wamp:"omitempty"`
	ArgumentsKw  wamp.Dict `wamp:"omitempty"`
}

type historyEntry struct {
	publication *wamp.Publish
	event       storedEvent
}

type historyStore struct {
	entries        []historyEntry
	matchPolicy    string
	limit          int
	isLimitReached bool
}

type broker struct {
	// topic -> subscription
	topicSubscription    map[wamp.URI]*subscription
	pfxTopicSubscription map[wamp.URI]*subscription
	wcTopicSubscription  map[wamp.URI]*subscription

	// event history in-memory store
	eventHistoryStore map[*subscription]*historyStore

	// subscription ID -> subscription
	subscriptions map[wamp.ID]*subscription

	// Session -> subscription ID set
	sessionSubIDSet map[*wamp.Session]map[wamp.ID]struct{}

	actionChan chan func()
	stopped    chan struct{}

	// Generate subscription IDs.
	idGen *wamp.IDGen

	strictURI     bool
	allowDisclose bool

	log           stdlog.StdLog
	debug         bool
	filterFactory FilterFactory
}

// newBroker returns a new default broker implementation instance.
func newBroker(logger stdlog.StdLog, strictURI, allowDisclose, debug bool, publishFilter FilterFactory, evntCfgs []*TopicEventHistoryConfig) (*broker, error) { //nolint:lll
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
		eventHistoryStore:    map[*subscription]*historyStore{},

		subscriptions:   map[wamp.ID]*subscription{},
		sessionSubIDSet: map[*wamp.Session]map[wamp.ID]struct{}{},

		// The action handler should be nearly always runable, since it is the
		// critical section that does the only routing.  So, and unbuffered
		// channel is appropriate.
		actionChan: make(chan func()),
		stopped:    make(chan struct{}),

		idGen: new(wamp.IDGen),

		strictURI:     strictURI,
		allowDisclose: allowDisclose,

		log:           logger,
		debug:         debug,
		filterFactory: publishFilter,
	}
	err := b.PreInitEventHistoryTopics(evntCfgs)
	// if broker fails initialize event history store we just log it,
	// the broker itself will continue to operate but without event history store
	if err != nil {
		return nil, configError{Err: err}
	}
	go b.run()
	return b, nil
}

// role returns the role information for the "broker" role.  The data returned
// is suitable for use as broker role info in a WELCOME message.
func (b *broker) role() wamp.Dict {
	return brokerRole
}

// PreInitEventHistoryTopics initializes storage for event history enabled topics.
func (b *broker) PreInitEventHistoryTopics(evntCfgs []*TopicEventHistoryConfig) error {
	for _, topicCfg := range evntCfgs {
		if !topicCfg.Topic.ValidURI(b.strictURI, topicCfg.MatchPolicy) {
			return fmt.Errorf("invalid topic/match configuration found: %s %s", topicCfg.Topic, topicCfg.MatchPolicy)
		}

		if topicCfg.Limit <= 0 {
			return fmt.Errorf("invalid limit for topic configuration found: %d %s", topicCfg.Limit, topicCfg.Topic)
		}

		sub, _ := b.syncInitSubscription(topicCfg.Topic, topicCfg.MatchPolicy, nil)

		b.eventHistoryStore[sub] = &historyStore{
			entries:        []historyEntry{},
			matchPolicy:    topicCfg.MatchPolicy,
			limit:          topicCfg.Limit,
			isLimitReached: false,
		}

	}
	return nil
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
			Details:   wamp.Dict{},
		})
		return
	}

	details := wamp.Dict{}

	// Check and handle Payload PassThru Mode
	// @see https://wamp-proto.org/wamp_latest_ietf.html#name-payload-passthru-mode
	if pptScheme, _ := msg.Options[wamp.OptPPTScheme].(string); pptScheme != "" {

		// Let's check: was ppt feature announced by publisher?
		if !pub.HasFeature(wamp.RolePublisher, wamp.FeaturePayloadPassthruMode) {
			// It's protocol violation, so we need to abort connection
			abortMsg := wamp.Abort{Reason: wamp.ErrProtocolViolation}
			abortMsg.Details = wamp.Dict{}
			abortMsg.Details[wamp.OptMessage] = ErrPPTNotSupportedByPeer.Error()
			b.trySend(pub, &abortMsg)
			pub.Close()

			return
		}

		// Every side supports PPT feature
		// Let's fill PPT options for callee
		pptOptionsToDetails(msg.Options, details)
	}

	excludePub := true
	if exclude, ok := msg.Options[wamp.OptExcludeMe].(bool); ok {
		if !pub.HasFeature(wamp.RolePublisher, wamp.FeaturePubExclusion) {
			b.log.Println(wamp.RolePublisher, "included", wamp.OptExcludeMe,
				"option but did not announce support for",
				wamp.FeaturePubExclusion)
			// Do not return error here, even though published did not announce
			// for publisher_exclusion.  Assume that publisher supports it
			// since specify option that requires it.
		}
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
		b.syncPublish(pub, msg, pubID, excludePub, disclose, filter, details)
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
			Details:   wamp.Dict{},
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
	<-b.stopped
	if b.debug {
		b.log.Print("Broker stopped")
	}
}

func (b *broker) run() {
	for action := range b.actionChan {
		action()
	}
	close(b.stopped)
}

func (b *broker) syncPublish(pub *wamp.Session, msg *wamp.Publish, pubID wamp.ID, excludePub, disclose bool, filter PublishFilter, eventDetails wamp.Dict) { //nolint:lll
	// Publish to subscribers with exact match.
	if sub, ok := b.topicSubscription[msg.Topic]; ok {
		b.syncPubEvent(pub, msg, pubID, sub, excludePub, false, disclose, filter, eventDetails)
	}

	// Publish to subscribers with prefix match.
	for pfxTopic, sub := range b.pfxTopicSubscription {
		if msg.Topic.PrefixMatch(pfxTopic) {
			b.syncPubEvent(pub, msg, pubID, sub, excludePub, true, disclose, filter, eventDetails)
		}
	}

	// Publish to subscribers with wildcard match.
	for wcTopic, sub := range b.wcTopicSubscription {
		if msg.Topic.WildcardMatch(wcTopic) {
			b.syncPubEvent(pub, msg, pubID, sub, excludePub, true, disclose, filter, eventDetails)
		}
	}
}

func newSubscription(id wamp.ID, subscriber *wamp.Session, topic wamp.URI, match string) *subscription {
	subscribers := map[*wamp.Session]struct{}{}
	if subscriber != nil {
		subscribers[subscriber] = struct{}{}
	}

	return &subscription{
		id:          id,
		topic:       topic,
		match:       match,
		created:     wamp.NowISO8601(),
		subscribers: subscribers,
	}
}

func (b *broker) syncSaveEvent(eventStore *historyStore, pub *wamp.Publish, event *wamp.Event) {

	if eventStore.isLimitReached {
		eventStore.entries = eventStore.entries[1:]
	} else if len(eventStore.entries) >= eventStore.limit {
		eventStore.isLimitReached = true
		eventStore.entries = eventStore.entries[1:]
	}

	item := historyEntry{
		publication: pub,
		event: storedEvent{
			timestamp:    time.Now(),
			Subscription: event.Subscription,
			Publication:  event.Publication,
			Details:      event.Details,
			Arguments:    event.Arguments,
			ArgumentsKw:  event.ArgumentsKw,
		},
	}

	eventStore.entries = append(eventStore.entries, item)
}

func (b *broker) syncInitSubscription(topic wamp.URI, match string, subscriber *wamp.Session) (sub *subscription, existingSub bool) {

	switch match {
	case wamp.MatchPrefix:
		// Subscribe to any topic that matches by the given prefix URI
		sub, existingSub = b.pfxTopicSubscription[topic]
		if !existingSub {
			// Create a new prefix subscription.
			sub = newSubscription(b.idGen.Next(), subscriber, topic, match)
			b.pfxTopicSubscription[topic] = sub
		}
	case wamp.MatchWildcard:
		// Subscribe to any topic that matches by the given wildcard URI.
		sub, existingSub = b.wcTopicSubscription[topic]
		if !existingSub {
			// Create a new wildcard subscription.
			sub = newSubscription(b.idGen.Next(), subscriber, topic, match)
			b.wcTopicSubscription[topic] = sub
		}
	default:
		// Subscribe to the topic that exactly matches the given URI.
		sub, existingSub = b.topicSubscription[topic]
		if !existingSub {
			// Create a new subscription.
			sub = newSubscription(b.idGen.Next(), subscriber, topic, match)
			b.topicSubscription[topic] = sub
		}
	}
	b.subscriptions[sub.id] = sub

	return sub, existingSub
}

func (b *broker) syncSubscribe(subscriber *wamp.Session, msg *wamp.Subscribe, match string) {
	sub, existingSub := b.syncInitSubscription(msg.Topic, match, subscriber)

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
			Details: wamp.Dict{},
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
func (b *broker) syncPubEvent(pub *wamp.Session, msg *wamp.Publish, pubID wamp.ID, sub *subscription, excludePublisher, sendTopic, disclose bool, filter PublishFilter, eventDetails wamp.Dict) { //nolint:lll
	for subscriber := range sub.subscribers {
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

		event := prepareEvent(pub, msg, pubID, sub, sendTopic, disclose, eventDetails, subscriber)
		b.trySend(subscriber, event)
	}

	// If event history store is enabled for subscription let's save event
	if eventStore, ok := b.eventHistoryStore[sub]; ok {
		// We should ignore publications that have exclude or eligible lists of session IDs
		// As session ID is a runtime thing, it is impossible to check later: is history asker allowed to get
		// this publication or not
		// @see Security Aspects paragraph of Event History section in WAMP Spec
		if _, ok := msg.Options["exclude"]; ok {
			return
		}
		if _, ok := msg.Options["eligible"]; ok {
			return
		}
		event := prepareEvent(pub, msg, pubID, sub, sendTopic, disclose, eventDetails, nil)
		b.syncSaveEvent(eventStore, msg, event)
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
		makeEvent := func() *wamp.Event {
			evt := &wamp.Event{
				Publication:  pubID,
				Subscription: metaSub.id,
				Details:      wamp.Dict{},
				Arguments:    wamp.List{subSessID, subID},
			}
			if sendTopic {
				evt.Details[detailTopic] = metaTopic
			}
			return evt
		}
		var event *wamp.Event

		for subscriber := range metaSub.subscribers {
			// Do not send the meta event to the session that is causing the
			// meta event to be generated.  This prevents useless events that
			// could lead to race conditions on the client.
			if subscriber.ID == subSessID {
				continue
			}

			// Need to send separate event message to each local subscriber,
			// since local clients could modify contents.
			if subscriber.Peer.IsLocal() {
				b.trySend(subscriber, makeEvent())
				continue
			}

			if event == nil {
				event = makeEvent()
			}
			b.trySend(subscriber, event)
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
		makeEvent := func() *wamp.Event {
			evt := &wamp.Event{
				Publication:  pubID,
				Subscription: metaSub.id,
				Details:      wamp.Dict{},
				Arguments: wamp.List{
					subSessID,
					wamp.Dict{
						"id":          sub.id,
						"created":     sub.created,
						"uri":         sub.topic,
						wamp.OptMatch: sub.match,
					},
				},
			}
			if sendTopic {
				evt.Details[detailTopic] = wamp.MetaEventSubOnCreate
			}
			return evt
		}
		var event *wamp.Event

		for subscriber := range metaSub.subscribers {
			// Do not send the meta event to the session that is causing the
			// meta event to be generated.  This prevents useless events that
			// could lead to race conditions on the client.
			if subscriber.ID == subSessID {
				continue
			}

			// Need to send separate event message to each locsl subscriber,
			// since local clients could modify contents.
			if subscriber.Peer.IsLocal() {
				b.trySend(subscriber, makeEvent())
				continue
			}

			if event == nil {
				event = makeEvent()
			}
			b.trySend(subscriber, event)
		}
	})
}

func (b *broker) trySend(sess *wamp.Session, msg wamp.Message) {
	select {
	case sess.Send() <- msg:
	default:
		b.log.Printf("!!! Dropped %s to session %s: blocked", msg.MessageType(), sess)
	}
}

func prepareEvent(pub *wamp.Session, msg *wamp.Publish, pubID wamp.ID, sub *subscription, sendTopic, disclose bool, eventDetails wamp.Dict, subscriber *wamp.Session) *wamp.Event { //nolint:lll
	details := eventDetails
	if details == nil {
		details = wamp.Dict{}
	}

	event := &wamp.Event{
		Publication:  pubID,
		Subscription: sub.id,
		Arguments:    msg.Arguments,
		ArgumentsKw:  msg.ArgumentsKw,
		Details:      details,
	}
	// If a subscription was established with a pattern-based matching
	// policy, a Broker MUST supply the original PUBLISH.Topic as provided
	// by the Publisher in EVENT.Details.topic|uri.
	if sendTopic {
		event.Details[detailTopic] = msg.Topic
	}
	if disclose && subscriber != nil && subscriber.HasFeature(wamp.RoleSubscriber, wamp.FeaturePubIdent) {
		disclosePublisher(pub, event.Details)
	}

	// TODO: Handle publication trust levels

	if subscriber != nil && subscriber.Peer.IsLocal() {
		// the local handler may be lousy and may try to modify
		// either of those fields, make sure that each event
		// carries a copy of the respective structure, even if
		// it's empty
		if event.Details != nil {
			options := make(map[string]interface{}, len(event.Details))
			for k, v := range event.Details {
				options[k] = v
			}
			event.Details = options
		}
		if msg.Arguments != nil {
			event.Arguments = make([]interface{}, len(msg.Arguments))
			copy(event.Arguments, msg.Arguments)
		}
		if msg.ArgumentsKw != nil {
			argsKw := make(map[string]interface{}, len(msg.ArgumentsKw))
			for k, v := range msg.ArgumentsKw {
				argsKw[k] = v
			}
			event.ArgumentsKw = argsKw
		}
	}
	return event
}

// disclosePublisher adds publisher identity information to EVENT.Details.
func disclosePublisher(pub *wamp.Session, details wamp.Dict) {
	details[wamp.RolePublisher] = pub.ID
	// These values are not required by the specification, but are here for
	// compatibility with Crossbar.
	pub.Lock()
	for _, f := range []string{"authid", "authrole"} {
		if val, ok := pub.Details[f]; ok {
			details[fmt.Sprintf("%s_%s", wamp.RolePublisher, f)] = val
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
					subIDs = append(subIDs, sub.id)
				}
				for pfxTopic, sub := range b.pfxTopicSubscription {
					if topic.PrefixMatch(pfxTopic) {
						subIDs = append(subIDs, sub.id)
					}
				}
				for wcTopic, sub := range b.wcTopicSubscription {
					if topic.WildcardMatch(wcTopic) {
						subIDs = append(subIDs, sub.id)
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

// eventHistoryLast retrieves events history for subscription applying provided filters
// TODO: Need to filter by authid and/or authrole of caller if stored events have any of them
// But that's not possible in current arch as meta peer doesn't know anything about caller, just invocation message
func (b *broker) subEventHistory(msg *wamp.Invocation) wamp.Message {
	var events wamp.List // []*storedEvent
	var isLimitReached bool
	var reverse bool
	var limit int
	var fromDate time.Time
	var afterDate time.Time
	var beforeDate time.Time
	var untilDate time.Time
	var dateStr string
	var topicStr string
	var topicUri wamp.URI
	var fromPub wamp.ID
	var afterPub wamp.ID
	var beforePub wamp.ID
	var untilPub wamp.ID
	var err error

	if len(msg.Arguments) == 0 {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrInvalidArgument,
		}
	}

	subId, ok := wamp.AsID(msg.Arguments[0])

	if !ok {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrInvalidArgument,
		}
	}

	limit, ok = msg.ArgumentsKw["limit"].(int)
	if ok && limit < 1 {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrInvalidArgument,
		}
	}

	reverseOp, ok := msg.ArgumentsKw["reverse"]
	if ok {
		reverse, ok = reverseOp.(bool)
		if !ok {
			return &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrInvalidArgument,
			}
		}
	}

	dateStr, ok = msg.ArgumentsKw["from_time"].(string)
	if ok {
		fromDate, err = time.Parse(time.RFC3339, dateStr)
		if err != nil {
			return &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrInvalidArgument,
			}
		}
	}

	dateStr, ok = msg.ArgumentsKw["after_time"].(string)
	if ok {
		afterDate, err = time.Parse(time.RFC3339, dateStr)
		if err != nil {
			return &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrInvalidArgument,
			}
		}
	}

	dateStr, ok = msg.ArgumentsKw["before_time"].(string)
	if ok {
		beforeDate, err = time.Parse(time.RFC3339, dateStr)
		if err != nil {
			return &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrInvalidArgument,
			}
		}
	}

	dateStr, ok = msg.ArgumentsKw["until_time"].(string)
	if ok {
		untilDate, err = time.Parse(time.RFC3339, dateStr)
		if err != nil {
			return &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrInvalidArgument,
			}
		}
	}

	topicStr, ok = msg.ArgumentsKw["topic"].(string)
	if ok {
		topicUri = wamp.URI(topicStr)
	}

	fromPubOp, ok := msg.ArgumentsKw["from_publication"]
	if ok {
		fromPub = fromPubOp.(wamp.ID)
		if !ok || fromPub < 1 {
			return &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrInvalidArgument,
			}

		}
	}

	afterPubOp, ok := msg.ArgumentsKw["after_publication"]
	if ok {
		afterPub = afterPubOp.(wamp.ID)
		if !ok || afterPub < 1 {
			return &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrInvalidArgument,
			}
		}
	}

	beforePubOp, ok := msg.ArgumentsKw["before_publication"]
	if ok {
		beforePub = beforePubOp.(wamp.ID)
		if !ok || beforePub < 1 {
			return &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrInvalidArgument,
			}
		}
	}

	untilPubOp, ok := msg.ArgumentsKw["until_publication"]
	if ok {
		untilPub = untilPubOp.(wamp.ID)
		if !ok || untilPub < 1 {
			return &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrInvalidArgument,
			}
		}
	}

	ch := make(chan struct{})
	b.actionChan <- func() {
		var filteredEvents []storedEvent

		if subscription, ok := b.subscriptions[subId]; ok {
			if storeItem, ok := b.eventHistoryStore[subscription]; ok {
				isLimitReached = storeItem.isLimitReached

				fromPubReached := false
				afterPubReached := false
				untilPubReached := false

				for _, entry := range storeItem.entries {
					if !fromDate.IsZero() && entry.event.timestamp.Before(fromDate) {
						continue
					}
					if !afterDate.IsZero() && !entry.event.timestamp.After(afterDate) {
						continue
					}
					if !beforeDate.IsZero() && !entry.event.timestamp.Before(beforeDate) {
						continue
					}
					if !untilDate.IsZero() && entry.event.timestamp.After(untilDate) {
						continue
					}
					if fromPub > 0 && !fromPubReached {
						if entry.event.Publication != fromPub {
							continue
						} else {
							fromPubReached = true
						}
					}
					if afterPub > 0 && !afterPubReached {
						if entry.event.Publication != afterPub {
							continue
						} else {
							afterPubReached = true
							continue
						}
					}
					if beforePub > 0 && entry.event.Publication == beforePub {
						break
					}
					if untilPub > 0 {
						// We need to include specified event, but also
						// we need to check it against remaining filters,
						// so we rise up untilPubReached flag and break the
						// cycle on next turn
						if untilPubReached {
							break
						}
						if entry.event.Publication == untilPub {
							untilPubReached = true
						}
					}

					eventTopic, ok := entry.event.Details["topic"]
					if len(topicUri) > 0 && (!ok || eventTopic != topicUri) {
						continue
					}

					filteredEvents = append(filteredEvents, entry.event)
				}
			}
		}

		if reverse {
			for i, j := 0, len(filteredEvents)-1; i < j; i, j = i+1, j-1 {
				filteredEvents[i], filteredEvents[j] = filteredEvents[j], filteredEvents[i]
			}
		}

		if limit > 0 {
			start := len(filteredEvents) - limit
			if start < 0 {
				start = 0
			}
			filteredEvents = filteredEvents[start:]
		}

		events, _ = wamp.AsList(filteredEvents)
		close(ch)
	}
	<-ch

	return &wamp.Yield{
		Request:     msg.Request,
		Arguments:   events,
		ArgumentsKw: wamp.Dict{"is_limit_reached": isLimitReached},
	}
}
