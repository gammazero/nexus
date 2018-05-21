package router

import (
	"fmt"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/wamp"
)

const (
	roleSub = "subscriber"
	rolePub = "publisher"

	featureSubBlackWhiteListing = "subscriber_blackwhite_listing"
	featurePatternSub           = "pattern_based_subscription"
	featurePubExclusion         = "publisher_exclusion"
	featurePubIdent             = "publisher_identification"
	featureSubMetaAPI           = "subscription_meta_api"

	detailTopic = "topic"
)

// Role information for this broker.
var brokerRole = wamp.Dict{
	"features": wamp.Dict{
		featureSubBlackWhiteListing: true,
		featurePatternSub:           true,
		featurePubExclusion:         true,
		featurePubIdent:             true,
		featureSubMetaAPI:           true,
	},
}

type Broker struct {
	// topic URI -> {subscription ID -> subscribed Session}
	topicSubscribers    map[wamp.URI]map[wamp.ID]*wamp.Session
	pfxTopicSubscribers map[wamp.URI]map[wamp.ID]*wamp.Session
	wcTopicSubscribers  map[wamp.URI]map[wamp.ID]*wamp.Session

	// subscription ID -> topic URI
	subscriptions    map[wamp.ID]wamp.URI
	pfxSubscriptions map[wamp.ID]wamp.URI
	wcSubscriptions  map[wamp.ID]wamp.URI

	// Session -> subscription ID set
	sessionSubIDSet map[*wamp.Session]map[wamp.ID]struct{}

	actionChan chan func()

	// Generate subscription IDs.
	idGen *wamp.IDGen

	strictURI     bool
	allowDisclose bool

	log   stdlog.StdLog
	debug bool
}

// NewBroker returns a new default broker implementation instance.
func NewBroker(logger stdlog.StdLog, strictURI, allowDisclose, debug bool) *Broker {
	if logger == nil {
		panic("logger is nil")
	}
	b := &Broker{
		topicSubscribers:    map[wamp.URI]map[wamp.ID]*wamp.Session{},
		pfxTopicSubscribers: map[wamp.URI]map[wamp.ID]*wamp.Session{},
		wcTopicSubscribers:  map[wamp.URI]map[wamp.ID]*wamp.Session{},

		subscriptions:    map[wamp.ID]wamp.URI{},
		pfxSubscriptions: map[wamp.ID]wamp.URI{},
		wcSubscriptions:  map[wamp.ID]wamp.URI{},

		sessionSubIDSet: map[*wamp.Session]map[wamp.ID]struct{}{},

		// The action handler should be nearly always runable, since it is the
		// critical section that does the only routing.  So, and unbuffered
		// channel is appropriate.
		actionChan: make(chan func()),

		idGen: new(wamp.IDGen),

		strictURI:     strictURI,
		allowDisclose: allowDisclose,

		log:   logger,
		debug: debug,
	}
	go b.run()
	return b
}

// Role returns the role information for the "broker" role.  The data returned
// is suitable for use as broker role info in a WELCOME message.
func (b *Broker) Role() wamp.Dict {
	return brokerRole
}

// Publish finds all subscriptions for the topic being published to, including
// those matching the topic by pattern, and sends an event to the subscribers
// of that topic.
//
// When a single event matches more than one of a Subscriber's subscriptions,
// the event will be delivered for each subscription.
//
// The Subscriber can detect the delivery of that same event on multiple
// subscriptions via EVENT.PUBLISHED.Publication, which will be identical.
func (b *Broker) Publish(pub *wamp.Session, msg *wamp.Publish) {
	if pub == nil || msg == nil {
		panic("broker.Publish with nil session or message")
	}
	// Validate URI.  For PUBLISH, must be valid URI (either strict or loose),
	// and all URI components must be non-empty.
	if !msg.Topic.ValidURI(b.strictURI, "") {
		if pubAck, _ := msg.Options[wamp.OptAcknowledge].(bool); !pubAck {
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
	if wamp.OptionFlag(msg.Options, wamp.OptDiscloseMe) {
		// Broker MAY deny a publisher's request to disclose its identity.
		if !b.allowDisclose {
			b.trySend(pub, &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrOptionDisallowedDiscloseMe,
			})
		}
		disclose = true
	}
	pubID := wamp.GlobalID()

	// Get blacklists and whitelists, if any, from publish message.
	filter := newPublishFilter(msg)

	b.actionChan <- func() {
		b.publish(pub, msg, pubID, excludePub, disclose, filter)
	}

	// Send Published message if acknowledge is present and true.
	if pubAck, _ := msg.Options[wamp.OptAcknowledge].(bool); pubAck {
		b.trySend(pub, &wamp.Published{Request: msg.Request, Publication: pubID})
	}
}

// Subscribe subscribes the client to the given topic.
//
// In case of receiving a SUBSCRIBE message from the same Subscriber and to
// already subscribed topic, Broker should answer with SUBSCRIBED message,
// containing the existing Subscription|id.
//
// By default, Subscribers subscribe to topics with exact matching policy. A
// Subscriber might want to subscribe to topics based on a pattern.  If the
// Broker and the Subscriber support pattern-based subscriptions, this matching
// can happen by prefix-matching policy or wildcard-matching policy.
func (b *Broker) Subscribe(sub *wamp.Session, msg *wamp.Subscribe) {
	if sub == nil || msg == nil {
		panic("broker.Subscribe with nil session or message")
	}
	// Validate topic URI.  For SUBSCRIBE, must be valid URI (either strict or
	// loose), and all URI components must be non-empty for normal
	// subscriptions, may be empty for wildcard subscriptions and must be
	// non-empty for all but the last component for prefix subscriptions.
	match := wamp.OptionString(msg.Options, wamp.OptMatch)
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
		b.subscribe(sub, msg, match)
	}
}

// Unsubscribe removes the requested subscription.
func (b *Broker) Unsubscribe(sub *wamp.Session, msg *wamp.Unsubscribe) {
	if sub == nil || msg == nil {
		panic("broker.Unsubscribe with nil session or message")
	}
	b.actionChan <- func() {
		b.unsubscribe(sub, msg)
	}
}

// RemoveSession removes all subscriptions of the subscriber.  This is called
// when a client leaves the realm by sending a GOODBYE message or by
// disconnecting from the router.  If there are any subscriptions for this
// session a wamp.subscription.on_delete meta event is published for each.
func (b *Broker) RemoveSession(sess *wamp.Session) {
	if sess == nil {
		return
	}
	b.actionChan <- func() {
		b.removeSession(sess)
	}
}

// Close stops the broker, letting already queued actions finish.
func (b *Broker) Close() {
	close(b.actionChan)
}

func (b *Broker) run() {
	for action := range b.actionChan {
		action()
	}
	if b.debug {
		b.log.Print("Broker stopped")
	}
}

func (b *Broker) publish(pub *wamp.Session, msg *wamp.Publish, pubID wamp.ID, excludePub, disclose bool, filter *publishFilter) {
	// Publish to subscribers with exact match.
	subs := b.topicSubscribers[msg.Topic]
	b.pubEvent(pub, msg, pubID, subs, excludePub, false, disclose, filter)

	// Publish to subscribers with prefix match.
	for pfxTopic, subs := range b.pfxTopicSubscribers {
		if msg.Topic.PrefixMatch(pfxTopic) {
			b.pubEvent(pub, msg, pubID, subs, excludePub, true, disclose, filter)
		}
	}

	// Publish to subscribers with wildcard match.
	for wcTopic, subs := range b.wcTopicSubscribers {
		if msg.Topic.WildcardMatch(wcTopic) {
			b.pubEvent(pub, msg, pubID, subs, excludePub, true, disclose, filter)
		}
	}
}

func (b *Broker) subscribe(sub *wamp.Session, msg *wamp.Subscribe, match string) {
	var idSub map[wamp.ID]*wamp.Session
	var subscriptions map[wamp.ID]wamp.URI
	var ok bool
	switch match {
	case wamp.MatchPrefix:
		// Subscribe to any topic that matches by the given prefix URI
		idSub, ok = b.pfxTopicSubscribers[msg.Topic]
		if !ok {
			idSub = map[wamp.ID]*wamp.Session{}
			b.pfxTopicSubscribers[msg.Topic] = idSub
		}
		subscriptions = b.pfxSubscriptions
	case wamp.MatchWildcard:
		// Subscribe to any topic that matches by the given wildcard URI.
		idSub, ok = b.wcTopicSubscribers[msg.Topic]
		if !ok {
			idSub = map[wamp.ID]*wamp.Session{}
			b.wcTopicSubscribers[msg.Topic] = idSub
		}
		subscriptions = b.wcSubscriptions
	default:
		// Subscribe to the topic that exactly matches the given URI.
		idSub, ok = b.topicSubscribers[msg.Topic]
		if !ok {
			idSub = map[wamp.ID]*wamp.Session{}
			b.topicSubscribers[msg.Topic] = idSub
		}
		subscriptions = b.subscriptions
	}

	// If the topic already has subscribers, then see if the session requesting
	// a subscription is already subscribed to the topic.
	newSub := true
	if ok {
		newSub = false
		for alreadyID, alreadySub := range idSub {
			if alreadySub == sub {
				// Already subscribed, send existing subscription ID.
				b.trySend(sub, &wamp.Subscribed{
					Request:      msg.Request,
					Subscription: alreadyID,
				})
				return
			}
		}
	}

	// Create a new subscription.
	id := b.idGen.Next()
	subscriptions[id] = msg.Topic
	idSub[id] = sub

	idSet, ok := b.sessionSubIDSet[sub]
	if !ok {
		idSet = map[wamp.ID]struct{}{}
		b.sessionSubIDSet[sub] = idSet
	}
	idSet[id] = struct{}{}

	// Tell sender the new subscription ID.
	b.trySend(sub, &wamp.Subscribed{Request: msg.Request, Subscription: id})

	if newSub {
		b.pubSubCreateMeta(msg.Topic, sub.ID, id, match)
	}

	// Publish WAMP on_subscribe meta event.
	b.pubSubMeta(wamp.MetaEventSubOnSubscribe, sub.ID, id)
}

func (b *Broker) unsubscribe(sub *wamp.Session, msg *wamp.Unsubscribe) {
	var delLastSub bool
	var topicSubscribers map[wamp.URI]map[wamp.ID]*wamp.Session
	topic, ok := b.subscriptions[msg.Subscription]
	if !ok {
		if topic, ok = b.pfxSubscriptions[msg.Subscription]; !ok {
			if topic, ok = b.wcSubscriptions[msg.Subscription]; !ok {
				b.trySend(sub, &wamp.Error{
					Type:    msg.MessageType(),
					Request: msg.Request,
					Error:   wamp.ErrNoSuchSubscription,
				})
				b.log.Println("Error unsubscribing: no such subscription",
					msg.Subscription)
				return
			}
			delete(b.wcSubscriptions, msg.Subscription)
			topicSubscribers = b.wcTopicSubscribers
		} else {
			delete(b.pfxSubscriptions, msg.Subscription)
			topicSubscribers = b.pfxTopicSubscribers
		}
	} else {
		delete(b.subscriptions, msg.Subscription)
		topicSubscribers = b.topicSubscribers
	}

	// clean up topic -> subscribed session
	if subs, ok := topicSubscribers[topic]; !ok {
		b.log.Println("Error unsubscribing: unable to find subscribers for",
			topic, "topic")
	} else if _, ok := subs[msg.Subscription]; !ok {
		b.log.Println("Error unsubscribing: topic", topic,
			"does not have subscription", msg.Subscription)
	} else {
		delete(subs, msg.Subscription)
		if len(subs) == 0 {
			delete(b.topicSubscribers, topic)
			delLastSub = true
		}
	}

	// clean up sender's subscription
	if s, ok := b.sessionSubIDSet[sub]; !ok {
		b.log.Print("Error unsubscribing: no subscriptions for sender")
	} else if _, ok := s[msg.Subscription]; !ok {
		b.log.Println("Error unsubscribing: cannot find subscription",
			msg.Subscription, "for sender")
	} else {
		delete(s, msg.Subscription)
		if len(s) == 0 {
			delete(b.sessionSubIDSet, sub)
		}
	}

	// Tell sender they are unsubscribed.
	b.trySend(sub, &wamp.Unsubscribed{Request: msg.Request})

	// Publish WAMP unsubscribe meta event.
	b.pubSubMeta(wamp.MetaEventSubOnUnsubscribe, sub.ID, msg.Subscription)
	if delLastSub {
		// Fired when a subscription is deleted after the last session attached
		// to it has been removed.
		b.pubSubMeta(wamp.MetaEventSubOnDelete, sub.ID, msg.Subscription)
	}
}

func (b *Broker) removeSession(sub *wamp.Session) {
	var topicSubscribers map[wamp.URI]map[wamp.ID]*wamp.Session
	for id := range b.sessionSubIDSet[sub] {
		// For each subscription ID, delete the subscription: topic map entry.
		topic, ok := b.subscriptions[id]
		if !ok {
			if topic, ok = b.pfxSubscriptions[id]; !ok {
				if topic, ok = b.wcSubscriptions[id]; !ok {
					continue
				}
				delete(b.wcSubscriptions, id)
				topicSubscribers = b.wcTopicSubscribers
			} else {
				delete(b.pfxSubscriptions, id)
				topicSubscribers = b.pfxTopicSubscribers
			}
		} else {
			delete(b.subscriptions, id)
			topicSubscribers = b.topicSubscribers
		}

		// clean up topic -> subscriber session
		if subs, ok := topicSubscribers[topic]; ok {
			if _, ok := subs[id]; ok {
				delete(subs, id)
				if len(subs) == 0 {
					delete(b.topicSubscribers, topic)
					// Fired when a subscription is deleted after the last
					// session attached to it has been removed.
					b.pubSubMeta(wamp.MetaEventSubOnDelete, sub.ID, id)
				}
			}
		}
	}
	delete(b.sessionSubIDSet, sub)
}

// pubEvent sends an event to all subscribers that are not excluded from
// receiving the event.
func (b *Broker) pubEvent(pub *wamp.Session, msg *wamp.Publish, pubID wamp.ID, subs map[wamp.ID]*wamp.Session, excludePublisher, sendTopic, disclose bool, filter *publishFilter) {
	for id, sub := range subs {
		// Do not send event to publisher.
		if sub == pub && excludePublisher {
			continue
		}

		// Check if receiver is restricted.
		if filter != nil && !filter.publishAllowed(sub) {
			continue
		}

		details := wamp.Dict{}

		// If a subscription was established with a pattern-based matching
		// policy, a Broker MUST supply the original PUBLISH.Topic as provided
		// by the Publisher in EVENT.Details.topic|uri.
		if sendTopic {
			details[detailTopic] = msg.Topic
		}

		if disclose && sub.HasFeature(roleSub, featurePubIdent) {
			disclosePublisher(pub, details)
		}

		// TODO: Handle publication trust levels

		b.trySend(sub, &wamp.Event{
			Publication:  pubID,
			Subscription: id,
			Arguments:    msg.Arguments,
			ArgumentsKw:  msg.ArgumentsKw,
			Details:      details,
		})
	}
}

// pubMeta publishes the subscription meta event, using the supplied function,
// to the matching subscribers.
func (b *Broker) pubMeta(metaTopic wamp.URI, sendMeta func(subs map[wamp.ID]*wamp.Session, sendTopic bool)) {
	// Publish to subscribers with exact match.
	subs := b.topicSubscribers[metaTopic]
	sendMeta(subs, false)
	// Publish to subscribers with prefix match.
	for pfxTopic, subs := range b.pfxTopicSubscribers {
		if metaTopic.PrefixMatch(pfxTopic) {
			sendMeta(subs, true)
		}
	}
	// Publish to subscribers with wildcard match.
	for wcTopic, subs := range b.wcTopicSubscribers {
		if metaTopic.WildcardMatch(wcTopic) {
			sendMeta(subs, true)
		}
	}
}

// pubSubMeta publishes a subscription meta event when a subscription is added,
// removed, or deleted.
func (b *Broker) pubSubMeta(metaTopic wamp.URI, subSessID, subID wamp.ID) {
	pubID := wamp.GlobalID()
	sendMeta := func(subs map[wamp.ID]*wamp.Session, sendTopic bool) {
		for id, sub := range subs {
			// Do not send the meta event to the session that is causing the
			// meta event to be generated.  This prevents useless events that
			// could lead to race conditions on the client.
			if sub.ID == subSessID {
				continue
			}
			details := wamp.Dict{}
			if sendTopic {
				details[detailTopic] = metaTopic
			}
			b.trySend(sub, &wamp.Event{
				Publication:  pubID,
				Subscription: id,
				Details:      details,
				Arguments:    wamp.List{subSessID, subID},
			})
		}
	}
	b.pubMeta(metaTopic, sendMeta)
}

// pubSubCreateMeta publishes a meta event on subscription creation.
//
// Fired when a subscription is created through a subscription request for a
// topic which was previously without subscribers.
func (b *Broker) pubSubCreateMeta(subTopic wamp.URI, subSessID, subID wamp.ID, match string) {
	created := wamp.NowISO8601()
	pubID := wamp.GlobalID()
	sendMeta := func(subs map[wamp.ID]*wamp.Session, sendTopic bool) {
		for id, sub := range subs {
			// Do not send the meta event to the session that is causing the
			// meta event to be generated.  This prevents useless events that
			// could lead to race conditions on the client.
			if sub.ID == subSessID {
				continue
			}
			details := wamp.Dict{}
			if sendTopic {
				details[detailTopic] = wamp.MetaEventSubOnCreate
			}
			subDetails := wamp.Dict{
				"id":          subID,
				"created":     created,
				"uri":         subTopic,
				wamp.OptMatch: match,
			}
			b.trySend(sub, &wamp.Event{
				Publication:  pubID,
				Subscription: id,
				Details:      details,
				Arguments:    wamp.List{subSessID, subDetails},
			})
		}
	}
	b.pubMeta(wamp.MetaEventSubOnCreate, sendMeta)
}

func (b *Broker) trySend(sess *wamp.Session, msg wamp.Message) bool {
	if err := sess.TrySend(msg); err != nil {
		b.log.Println("!!! broker dropped", msg.MessageType(), "message:", err)
		return false
	}
	return true
}

func disclosePublisher(pub *wamp.Session, details wamp.Dict) {
	details[rolePub] = pub.ID
	features := []string{
		"authrole",
		"authid",
		"authmethod",
		"authextra",
	}
	for _, f := range features {
		val, ok := pub.Details[f]
		if ok {
			details[fmt.Sprintf("%s_%s", rolePub, f)] = val
		}
	}
}
