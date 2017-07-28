package router

import (
	"fmt"

	"github.com/gammazero/nexus/wamp"
)

// TODO: Implement the following:
// - publisher trust levels
// - event history
// - testament_meta_api
// - session_meta_api

// Features supported by this broker.
var brokerFeatures = map[string]interface{}{
	"features": map[string]interface{}{
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
	// Submit dispatches a Publish, Subscribe, or Unsubscribe message to the
	// broker.
	Submit(sess *Session, msg wamp.Message)

	// RemoveSession removes all subscriptions of the subscriber.
	RemoveSession(*Session)

	// Close shuts down the broker.
	Close()

	// Features returns the features supported by this broker.
	//
	// The data returned is suitable for use as the "features" section of the
	// broker role in a WELCOME message.
	Features() map[string]interface{}
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

	reqChan chan routerReq

	// Generate subscription IDs.
	idGen *wamp.IDGen

	strictURI     bool
	allowDisclose bool
}

// NewBroker returns a new default broker implementation instance.
func NewBroker(strictURI, allowDisclose bool) Broker {
	b := &broker{
		topicSubscribers:    map[wamp.URI]map[wamp.ID]*Session{},
		pfxTopicSubscribers: map[wamp.URI]map[wamp.ID]*Session{},
		wcTopicSubscribers:  map[wamp.URI]map[wamp.ID]*Session{},

		subscriptions:    map[wamp.ID]wamp.URI{},
		pfxSubscriptions: map[wamp.ID]wamp.URI{},
		wcSubscriptions:  map[wamp.ID]wamp.URI{},

		sessionSubIDSet: map[*Session]map[wamp.ID]struct{}{},

		// The request handler channel does not need to be more than size one,
		// since the incoming messages will be processed at the same rate
		// whether the messages sit in the recv channel of peers, or they sit
		// in the reqChan.
		reqChan: make(chan routerReq, 1),

		idGen: wamp.NewIDGen(),

		strictURI:     strictURI,
		allowDisclose: allowDisclose,
	}
	go b.reqHandler()
	return b
}

// Features returns the features supported by this broker.
func (b *broker) Features() map[string]interface{} {
	return brokerFeatures
}

func (b *broker) Submit(sess *Session, msg wamp.Message) {
	if msg == nil || sess == nil {
		panic("nil session or message")
	}
	b.reqChan <- routerReq{session: sess, msg: msg}
}

func (b *broker) RemoveSession(sub *Session) {
	if sub == nil {
		panic("nil subscriber")
	}
	b.reqChan <- routerReq{session: sub}
}

// Close stops the broker and waits message processing to stop.
func (b *broker) Close() {
	close(b.reqChan)
}

// reqHandler is broker's main processing function that is run by a single
// goroutine.  All functions that access broker data structures run on this
// routine.
func (b *broker) reqHandler() {
	for req := range b.reqChan {
		if req.msg == nil {
			b.removeSession(req.session)
			continue
		}
		sess := req.session
		switch msg := req.msg.(type) {
		case *wamp.Publish:
			b.publish(sess, msg)
		case *wamp.Subscribe:
			b.subscribe(sess, msg)
		case *wamp.Unsubscribe:
			b.unsubscribe(sess, msg)
		default:
			panic(fmt.Sprint("broker received message type: ",
				req.msg.MessageType()))
		}
	}
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
func (b *broker) publish(pub *Session, msg *wamp.Publish) {
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
				Arguments: []interface{}{errMsg},
			})
		}
		return
	}

	pubID := wamp.GlobalID()

	excludePublisher := true
	if exclude, ok := msg.Options["exclude_me"].(bool); ok {
		excludePublisher = exclude
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
				Details: map[string]interface{}{},
				Error:   wamp.ErrOptionDisallowedDiscloseMe,
			})
		}
		disclose = true
	}

	// Publish to subscribers with exact match.
	subs := b.topicSubscribers[msg.Topic]
	b.pubEvent(pub, msg, pubID, subs, excludePublisher, false, disclose)

	// Publish to subscribers with prefix match.
	for pfxTopic, subs := range b.pfxTopicSubscribers {
		if msg.Topic.PrefixMatch(pfxTopic) {
			b.pubEvent(pub, msg, pubID, subs, excludePublisher, true, disclose)
		}
	}

	// Publish to subscribers with wildcard match.
	for wcTopic, subs := range b.wcTopicSubscribers {
		if msg.Topic.WildcardMatch(wcTopic) {
			b.pubEvent(pub, msg, pubID, subs, excludePublisher, true, disclose)
		}
	}

	// Send Published message if acknowledge is present and true.
	if pubAck, _ := msg.Options["acknowledge"].(bool); pubAck {
		pub.Send(&wamp.Published{Request: msg.Request, Publication: pubID})
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
func (b *broker) subscribe(sub *Session, msg *wamp.Subscribe) {
	// Validate topic URI.  For SUBSCRIBE, must be valid URI (either strict or
	// loose), and all URI components must be non-empty for normal
	// subscriptions, may be empty for wildcard subscriptions and must be
	// non-empty for all but the last component for prefix subscriptions.
	match := wamp.OptionString(msg.Options, "match")
	if !msg.Topic.ValidURI(b.strictURI, match) {
		errMsg := fmt.Sprintf("subscribe for invalid topic URI %v",
			msg.Topic, b.strictURI)
		sub.Send(&wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Error:     wamp.ErrInvalidURI,
			Arguments: []interface{}{errMsg},
		})
		return
	}

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

// unsubscribe removes the requested subscription.
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
				log.Println("Error unsubscribing: no such subscription",
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
		log.Println("Error unsubscribing: unable to find subscribers for",
			topic, "topic")
	} else if _, ok := subs[msg.Subscription]; !ok {
		log.Println("Error unsubscribing: topic", topic,
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
		log.Println("Error unsubscribing: no subscriptions for sender")
	} else if _, ok := s[msg.Subscription]; !ok {
		log.Println("Error unsubscribing: cannot find subscription",
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
	for id, sub := range subs {
		// Do not send event to publisher.
		if sub == pub && excludePublisher {
			continue
		}

		// Check if receiver is restricted.
		if !publishAllowed(msg, sub) {
			continue
		}

		details := map[string]interface{}{}

		// If a subscription was established with a pattern-based matching
		// policy, a Broker MUST supply the original PUBLISH.Topic as provided
		// by the Publisher in EVENT.Details.topic|uri.
		if sendTopic {
			details["topic"] = msg.Topic
		}

		if disclose {
			// TODO: Check for feature support, or let recipient ignore?
			if sub.HasFeature("subscriber", "publisher_identification") {
				details["publisher"] = pub.ID
			}
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
			details := map[string]interface{}{}
			if sendTopic {
				details["topic"] = metaTopic
			}
			sub.Send(&wamp.Event{
				Publication:  pubID,
				Subscription: id,
				Details:      details,
				Arguments:    []interface{}{subSessID, subID},
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
			details := map[string]interface{}{}
			if sendTopic {
				details["topic"] = wamp.MetaEventSubOnCreate
			}
			subDetails := map[string]interface{}{
				"id":      subID,
				"created": created,
				"uri":     subTopic,
				"match":   match,
			}
			sub.Send(&wamp.Event{
				Publication:  pubID,
				Subscription: id,
				Details:      details,
				Arguments:    []interface{}{subSessID, subDetails},
			})
		}
	}
	b.pubMeta(wamp.MetaEventSubOnCreate, sendMeta)
}

// publishAllowed determines if a message is allowed to be published to a
// subscriber, by looking at any blacklists and whitelists provided with the
// publish message.
func publishAllowed(msg *wamp.Publish, sub *Session) bool {
	if blacklist, ok := msg.Options["exclude"]; ok {
		if blacklist, ok := blacklist.([]string); ok {
			for i := range blacklist {
				if blacklist[i] == string(sub.ID) {
					return false
				}
			}
		}
	}
	if blacklist, ok := msg.Options["exclude_authid"]; ok {
		if blacklist, ok := blacklist.([]string); ok {
			authid := wamp.OptionString(sub.Details, "authid")
			for i := range blacklist {
				if blacklist[i] == authid {
					return false
				}
			}
		}
	}
	if blacklist, ok := msg.Options["exclude_authrole"]; ok {
		if blacklist, ok := blacklist.([]string); ok {
			authrole := wamp.OptionString(sub.Details, "authrole")
			for i := range blacklist {
				if blacklist[i] == authrole {
					return false
				}
			}
		}
	}
	if whitelist, ok := msg.Options["eligible"]; ok {
		eligible := false
		if whitelist, ok := whitelist.([]string); ok {
			for i := range whitelist {
				if whitelist[i] == string(sub.ID) {
					eligible = true
					break
				}
			}
		}
		if !eligible {
			return false
		}
	}
	if whitelist, ok := msg.Options["eligible_authid"]; ok {
		eligible := false
		if whitelist, ok := whitelist.([]string); ok {
			authid := wamp.OptionString(sub.Details, "authid")
			for i := range whitelist {
				if whitelist[i] == authid {
					eligible = true
					break
				}
			}
		}
		if !eligible {
			return false
		}
	}
	if whitelist, ok := msg.Options["eligible_authrole"]; ok {
		eligible := false
		if whitelist, ok := whitelist.([]string); ok {
			authrole := wamp.OptionString(sub.Details, "authrole")
			for i := range whitelist {
				if whitelist[i] == authrole {
					eligible = true
					break
				}
			}
		}
		if !eligible {
			return false
		}
	}
	return true
}
