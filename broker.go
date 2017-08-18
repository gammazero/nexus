package nexus

import (
	"fmt"
	"strings"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/wamp"
)

// TODO: Implement the following:
// - publisher trust levels
// - event history
// - testament_meta_api

// Features supported by this broker.
var brokerFeatures = wamp.Dict{
	"features": wamp.Dict{
		"subscriber_blackwhite_listing": true,
		"pattern_based_subscription":    true,
		"publisher_exclusion":           true,
		"publisher_identification":      true,
		"subscription_meta_api":         true,
	},
}

// Broker is the interface implemented by an object that handles routing EVENTS
// from Publishers to Subscribers.
type Broker interface {
	// Publish finds all subscriptions for the topic being published to,
	// including those matching the topic by pattern, and sends an event to the
	// subscribers of that topic.
	//
	// When a single event matches more than one of a Subscriber's
	// subscriptions, the event will be delivered for each subscription.
	//
	// The Subscriber can detect the delivery of that same event on multiple
	// subscriptions via EVENT.PUBLISHED.Publication, which will be identical.
	Publish(*Session, *wamp.Publish)

	// Subscribe subscribes the client to the given topic.
	//
	// In case of receiving a SUBSCRIBE message from the same Subscriber and to
	// already subscribed topic, Broker should answer with SUBSCRIBED message,
	// containing the existing Subscription|id.
	//
	// By default, Subscribers subscribe to topics with exact matching
	// policy. A Subscriber might want to subscribe to topics based on a
	// pattern.  If the Broker and the Subscriber support pattern-based
	// subscriptions, this matching can happen by prefix-matching policy or
	// wildcard-matching policy.
	Subscribe(*Session, *wamp.Subscribe)

	// Unsubscribe removes the requested subscription.
	Unsubscribe(*Session, *wamp.Unsubscribe)

	// RemoveSession removes all subscriptions of the subscriber.
	RemoveSession(*Session)

	// Close shuts down the broker.
	Close()

	// Features returns the features supported by this broker.
	//
	// The data returned is suitable for use as the "features" section of the
	// broker role in a WELCOME message.
	Features() wamp.Dict
}

type broker struct {
	// topic URI -> {subscription ID -> subscribed Session}
	topicSubscribers    map[wamp.URI]map[wamp.ID]*Session
	pfxTopicSubscribers map[wamp.URI]map[wamp.ID]*Session
	wcTopicSubscribers  map[wamp.URI]map[wamp.ID]*Session

	// subscription ID -> topic URI
	subscriptions    map[wamp.ID]wamp.URI
	pfxSubscriptions map[wamp.ID]wamp.URI
	wcSubscriptions  map[wamp.ID]wamp.URI

	// Session -> subscription ID set
	sessionSubIDSet map[*Session]map[wamp.ID]struct{}

	actionChan chan func()

	// Generate subscription IDs.
	idGen *wamp.IDGen

	strictURI     bool
	allowDisclose bool

	log   stdlog.StdLog
	debug bool
}

// NewBroker returns a new default broker implementation instance.
func NewBroker(logger stdlog.StdLog, strictURI, allowDisclose, debug bool) Broker {
	b := &broker{
		topicSubscribers:    map[wamp.URI]map[wamp.ID]*Session{},
		pfxTopicSubscribers: map[wamp.URI]map[wamp.ID]*Session{},
		wcTopicSubscribers:  map[wamp.URI]map[wamp.ID]*Session{},

		subscriptions:    map[wamp.ID]wamp.URI{},
		pfxSubscriptions: map[wamp.ID]wamp.URI{},
		wcSubscriptions:  map[wamp.ID]wamp.URI{},

		sessionSubIDSet: map[*Session]map[wamp.ID]struct{}{},

		// The action handler should be nearly always runable, since it is the
		// critical section that does the only routing.  So, and unbuffered
		// channel is appropriate.
		actionChan: make(chan func()),

		idGen: wamp.NewIDGen(),

		strictURI:     strictURI,
		allowDisclose: allowDisclose,

		log:   logger,
		debug: debug,
	}
	go b.run()
	return b
}

// Features returns the features supported by this broker.
func (b *broker) Features() wamp.Dict {
	return brokerFeatures
}

// Publish publishes an event to subscribers.
func (b *broker) Publish(pub *Session, msg *wamp.Publish) {
	if pub == nil || msg == nil {
		panic("broker.Publish with nil session or message")
	}
	// Validate URI.  For PUBLISH, must be valid URI (either strict or loose),
	// and all URI components must be non-empty.
	if !msg.Topic.ValidURI(b.strictURI, "") {
		opt, ok := msg.Options["acknowledge"]
		if !ok {
			return
		}
		if ack, ok := opt.(bool); ok && ack {
			errMsg := fmt.Sprintf(
				"publish with invalid topic URI %v (URI strict checking %v)",
				msg.Topic, b.strictURI)
			pub.Send(&wamp.Error{
				Type:      msg.MessageType(),
				Request:   msg.Request,
				Error:     wamp.ErrInvalidURI,
				Arguments: wamp.List{errMsg},
			})
		}
		return
	}

	excludePub := true
	if exclude, ok := msg.Options["exclude_me"].(bool); ok {
		excludePub = exclude
	}

	// A Broker may also (automatically) disclose the identity of a
	// Publisher even without the Publisher having explicitly requested to
	// do so when the Broker configuration (for the publication topic) is
	// set up to do so.  TODO: Currently no broker config for this.
	var disclose bool
	if wamp.OptionFlag(msg.Options, "disclose_me") {
		// Broker MAY deny a publisher's request to disclose its identity.
		if !b.allowDisclose {
			pub.Send(&wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrOptionDisallowedDiscloseMe,
			})
		}
		disclose = true
	}
	pubID := wamp.GlobalID()
	b.actionChan <- func() {
		b.publish(pub, msg, pubID, excludePub, disclose)
	}

	// Send Published message if acknowledge is present and true.
	if pubAck, _ := msg.Options["acknowledge"].(bool); pubAck {
		pub.Send(&wamp.Published{Request: msg.Request, Publication: pubID})
	}
}

// Subscribe subscribes the client to the given topic.
func (b *broker) Subscribe(sub *Session, msg *wamp.Subscribe) {
	if sub == nil || msg == nil {
		panic("broker.Subscribe with nil session or message")
	}
	// Validate topic URI.  For SUBSCRIBE, must be valid URI (either strict or
	// loose), and all URI components must be non-empty for normal
	// subscriptions, may be empty for wildcard subscriptions and must be
	// non-empty for all but the last component for prefix subscriptions.
	match := wamp.OptionString(msg.Options, "match")
	if !msg.Topic.ValidURI(b.strictURI, match) {
		errMsg := fmt.Sprintf(
			"subscribe for invalid topic URI %v (URI strict checking %v)",
			msg.Topic, b.strictURI)
		sub.Send(&wamp.Error{
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
func (b *broker) Unsubscribe(sub *Session, msg *wamp.Unsubscribe) {
	if sub == nil || msg == nil {
		panic("broker.Unsubscribe with nil session or message")
	}
	b.actionChan <- func() {
		b.unsubscribe(sub, msg)
	}
}

func (b *broker) RemoveSession(sess *Session) {
	if sess == nil {
		return
	}
	b.actionChan <- func() {
		b.removeSession(sess)
	}
}

// Close stops the broker and waits message processing to stop.
func (b *broker) Close() {
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

func (b *broker) publish(pub *Session, msg *wamp.Publish, pubID wamp.ID, excludePub, disclose bool) {
	// Publish to subscribers with exact match.
	subs := b.topicSubscribers[msg.Topic]
	b.pubEvent(pub, msg, pubID, subs, excludePub, false, disclose)

	// Publish to subscribers with prefix match.
	for pfxTopic, subs := range b.pfxTopicSubscribers {
		if msg.Topic.PrefixMatch(pfxTopic) {
			b.pubEvent(pub, msg, pubID, subs, excludePub, true, disclose)
		}
	}

	// Publish to subscribers with wildcard match.
	for wcTopic, subs := range b.wcTopicSubscribers {
		if msg.Topic.WildcardMatch(wcTopic) {
			b.pubEvent(pub, msg, pubID, subs, excludePub, true, disclose)
		}
	}
}

func (b *broker) subscribe(sub *Session, msg *wamp.Subscribe, match string) {
	var idSub map[wamp.ID]*Session
	var subscriptions map[wamp.ID]wamp.URI
	var ok bool
	switch match {
	case "prefix":
		// Subscribe to any topic that matches by the given prefix URI
		idSub, ok = b.pfxTopicSubscribers[msg.Topic]
		if !ok {
			idSub = map[wamp.ID]*Session{}
			b.pfxTopicSubscribers[msg.Topic] = idSub
		}
		subscriptions = b.pfxSubscriptions
	case "wildcard":
		// Subscribe to any topic that matches by the given wildcard URI.
		idSub, ok = b.wcTopicSubscribers[msg.Topic]
		if !ok {
			idSub = map[wamp.ID]*Session{}
			b.wcTopicSubscribers[msg.Topic] = idSub
		}
		subscriptions = b.wcSubscriptions
	default:
		// Subscribe to the topic that exactly matches the given URI.
		idSub, ok = b.topicSubscribers[msg.Topic]
		if !ok {
			idSub = map[wamp.ID]*Session{}
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
				sub.Send(&wamp.Subscribed{
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
	sub.Send(&wamp.Subscribed{Request: msg.Request, Subscription: id})

	if newSub {
		b.pubSubCreateMeta(msg.Topic, sub.ID, id, match)
	}

	// Publish WAMP on_subscribe meta event.
	b.pubSubMeta(wamp.MetaEventSubOnSubscribe, sub.ID, id)
}

func (b *broker) unsubscribe(sub *Session, msg *wamp.Unsubscribe) {
	var delLastSub bool
	var topicSubscribers map[wamp.URI]map[wamp.ID]*Session
	topic, ok := b.subscriptions[msg.Subscription]
	if !ok {
		if topic, ok = b.pfxSubscriptions[msg.Subscription]; !ok {
			if topic, ok = b.wcSubscriptions[msg.Subscription]; !ok {
				err := &wamp.Error{
					Type:    msg.MessageType(),
					Request: msg.Request,
					Error:   wamp.ErrNoSuchSubscription,
				}
				sub.Send(err)
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
	sub.Send(&wamp.Unsubscribed{Request: msg.Request})

	// Publish WAMP unsubscribe meta event.
	b.pubSubMeta(wamp.MetaEventSubOnUnsubscribe, sub.ID, msg.Subscription)
	if delLastSub {
		// Fired when a subscription is deleted after the last session attached
		// to it has been removed.
		b.pubSubMeta(wamp.MetaEventSubOnDelete, sub.ID, msg.Subscription)
	}
}

func (b *broker) removeSession(sub *Session) {
	var topicSubscribers map[wamp.URI]map[wamp.ID]*Session
	for id, _ := range b.sessionSubIDSet[sub] {
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
func (b *broker) pubEvent(pub *Session, msg *wamp.Publish, pubID wamp.ID, subs map[wamp.ID]*Session, excludePublisher, sendTopic, disclose bool) {
	// Get blacklists and whitelists, if any, from publish message.
	blIDs, wlIDs, blMap, wlMap := msgBlackWhiteLists(msg)
	// Check if any filtering is needed.
	var filter bool
	if len(blIDs) != 0 || len(wlIDs) != 0 || len(blMap) != 0 || len(wlMap) != 0 {
		filter = true
	}

	for id, sub := range subs {
		// Do not send event to publisher.
		if sub == pub && excludePublisher {
			continue
		}

		// Check if receiver is restricted.
		if filter && !publishAllowed(sub, blIDs, wlIDs, blMap, wlMap) {
			continue
		}

		details := wamp.Dict{}

		// If a subscription was established with a pattern-based matching
		// policy, a Broker MUST supply the original PUBLISH.Topic as provided
		// by the Publisher in EVENT.Details.topic|uri.
		if sendTopic {
			details["topic"] = msg.Topic
		}

		if disclose && sub.HasFeature("subscriber", "publisher_identification") {
			details["publisher"] = pub.ID
		}

		// TODO: Handle publication trust levels

		sub.Send(&wamp.Event{
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
func (b *broker) pubMeta(metaTopic wamp.URI, sendMeta func(subs map[wamp.ID]*Session, sendTopic bool)) {
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
func (b *broker) pubSubMeta(metaTopic wamp.URI, subSessID, subID wamp.ID) {
	pubID := wamp.GlobalID()
	sendMeta := func(subs map[wamp.ID]*Session, sendTopic bool) {
		for id, sub := range subs {
			// Do not send the meta event to the session that is causing the
			// meta event to be generated.  This prevents useless events that
			// could lead to race conditions on the client.
			if sub.ID == subSessID {
				continue
			}
			details := wamp.Dict{}
			if sendTopic {
				details["topic"] = metaTopic
			}
			sub.Send(&wamp.Event{
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
func (b *broker) pubSubCreateMeta(subTopic wamp.URI, subSessID, subID wamp.ID, match string) {
	created := wamp.NowISO8601()
	pubID := wamp.GlobalID()
	sendMeta := func(subs map[wamp.ID]*Session, sendTopic bool) {
		for id, sub := range subs {
			// Do not send the meta event to the session that is causing the
			// meta event to be generated.  This prevents useless events that
			// could lead to race conditions on the client.
			if sub.ID == subSessID {
				continue
			}
			details := wamp.Dict{}
			if sendTopic {
				details["topic"] = wamp.MetaEventSubOnCreate
			}
			subDetails := wamp.Dict{
				"id":      subID,
				"created": created,
				"uri":     subTopic,
				"match":   match,
			}
			sub.Send(&wamp.Event{
				Publication:  pubID,
				Subscription: id,
				Details:      details,
				Arguments:    wamp.List{subSessID, subDetails},
			})
		}
	}
	b.pubMeta(wamp.MetaEventSubOnCreate, sendMeta)
}

// msgBWLists gets any blacklists and whitelists included in a PUBLISH message.
func msgBlackWhiteLists(msg *wamp.Publish) ([]wamp.ID, []wamp.ID, map[string][]string, map[string][]string) {
	if len(msg.Options) == 0 {
		return nil, nil, nil, nil
	}

	var blIDs []wamp.ID
	if blacklist, ok := msg.Options["exclude"]; ok {
		if blacklist, ok := wamp.AsList(blacklist); ok {
			for i := range blacklist {
				if blVal, ok := wamp.AsID(blacklist[i]); ok {
					blIDs = append(blIDs, blVal)
				}
			}
		}
	}

	var wlIDs []wamp.ID
	if whitelist, ok := msg.Options["eligible"]; ok {
		if whitelist, ok := wamp.AsList(whitelist); ok {
			for i := range whitelist {
				if wlID, ok := wamp.AsID(whitelist[i]); ok {
					wlIDs = append(wlIDs, wlID)
				}
			}
		}
	}

	getAttrMap := func(prefix string) map[string][]string {
		var attrMap map[string][]string
		for k, vals := range msg.Options {
			if !strings.HasPrefix(k, prefix) {
				continue
			}
			if vals, ok := wamp.AsList(vals); ok {
				vallist := make([]string, 0, len(vals))
				for i := range vals {
					if val, ok := vals[i].(string); ok && val != "" {
						vallist = append(vallist, val)
					}
				}
				if len(vallist) != 0 {
					attrName := k[len(prefix):]
					if attrMap == nil {
						attrMap = map[string][]string{}
					}
					attrMap[attrName] = vallist
				}
			}
		}
		return attrMap
	}

	blMap := getAttrMap("exclude_")
	wlMap := getAttrMap("eligible_")

	return blIDs, wlIDs, blMap, wlMap
}

// publishAllowed determines if a message is allowed to be published to a
// subscriber, by looking at any blacklists and whitelists provided with the
// publish message.
func publishAllowed(sub *Session, blIDs, wlIDs []wamp.ID, blMap, wlMap map[string][]string) bool {
	// Check each blacklisted ID to see if session ID is blacklisted.
	for i := range blIDs {
		if blIDs[i] == sub.ID {
			return false
		}
	}
	// Check blacklists to see if session has a value in any blacklist.
	if len(blMap) != 0 {
		for attr, vals := range blMap {
			// Get the session attribute value to compare with blacklist.
			sessAttr := wamp.OptionString(sub.Details, attr)
			if sessAttr == "" {
				continue
			}
			// Check each blacklisted value to see if session attribute is one.
			for i := range vals {
				if vals[i] == sessAttr {
					// Session has blacklisted attribute value.
					return false
				}
			}
		}
	}

	var eligible bool
	// If session ID whitelist given, make sure session ID is in whitelist.
	if len(wlIDs) != 0 {
		eligible = false
		for i := range wlIDs {
			if wlIDs[i] == sub.ID {
				eligible = true
				break
			}
		}
		if !eligible {
			return false
		}
	}
	// Check whitelists to make sure session has value in each whitelist.
	if len(wlMap) != 0 {
		for attr, vals := range wlMap {
			// Get the session attribute value to compare with whitelist.
			sessAttr := wamp.OptionString(sub.Details, attr)
			if sessAttr == "" {
				// Session does not have whitelisted value, so deny.
				return false
			}
			eligible = false
			// Check all whitelisted values to see is session attribute is one.
			for i := range vals {
				if vals[i] == sessAttr {
					// Session has whitelisted attribute value.
					eligible = true
					break
				}
			}
			// If session attribute value no found in whitelist, then deny.
			if !eligible {
				return false
			}
		}
	}
	return true
}
