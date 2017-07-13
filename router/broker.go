package router

import (
	"fmt"
	"log"

	"github.com/gammazero/nexus/wamp"
)

// TODO: Implement the following:
// - publisher trust levels
// - event history
// - testament_meta_api
// - session_meta_api

// Features supported by this broker.
var brokerFeatures = map[string]interface{}{
	"features": map[string]bool{
		"subscriber_blackwhite_listing": true,
		"pattern_based_subscription":    true,
		"publisher_exclusion":           true,
		"publisher_identification":      true,
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

	reqChan    chan routerReq
	closedChan chan struct{}
	syncChan   chan struct{}

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
		reqChan:    make(chan routerReq, 1),
		closedChan: make(chan struct{}),
		syncChan:   make(chan struct{}),

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
	b.reqChan <- routerReq{session: sess, msg: msg}
}

func (b *broker) RemoveSession(sub *Session) {
	b.reqChan <- routerReq{session: sub}
}

// Close stops the broker and waits message processing to stop.
func (b *broker) Close() {
	if b.Closed() {
		return
	}
	close(b.reqChan)
	<-b.closedChan
}

// Closed returns true if the broker has been closed.
func (b *broker) Closed() bool {
	select {
	case <-b.closedChan:
		return true
	default:
	}
	return false
}

// Wait until all previous requests have been processed.
func (b *broker) sync() {
	b.reqChan <- routerReq{}
	<-b.syncChan
}

// reqHandler is broker's main processing function that is run by a single
// goroutine.  All functions that access broker data structures run on this
// routine.
func (b *broker) reqHandler() {
	defer close(b.closedChan)
	for req := range b.reqChan {
		if req.session == nil {
			b.syncChan <- struct{}{}
			continue
		}
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
				"publish with invalid topic URI %s (URI strict checking %s)",
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

	// Publish to subscribers with exact match.
	subs := b.topicSubscribers[msg.Topic]
	b.pubEvent(pub, msg, pubID, subs, excludePublisher, false)

	// Publish to subscribers with prefix match.
	for pfxTopic, subs := range b.pfxTopicSubscribers {
		if msg.Topic.PrefixMatch(pfxTopic) {
			b.pubEvent(pub, msg, pubID, subs, excludePublisher, true)
		}
	}

	// Publish to subscribers with wildcard match.
	for wcTopic, subs := range b.wcTopicSubscribers {
		if msg.Topic.WildcardMatch(wcTopic) {
			b.pubEvent(pub, msg, pubID, subs, excludePublisher, true)
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
		errMsg := fmt.Sprintf("subscribe for invalid topic URI %s",
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
	if ok {
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

	// TODO: publish WAMP meta events
}

// unsubscribe removes the requested subscription.
func (b *broker) unsubscribe(sub *Session, msg *wamp.Unsubscribe) {
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

	// TODO: publish WAMP meta events
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

		// clean up topic: subscriber session
		if subs, ok := topicSubscribers[topic]; ok {
			if _, ok := subs[id]; ok {
				delete(subs, id)
				if len(subs) == 0 {
					delete(b.topicSubscribers, topic)
				}
			}
		}
	}
	delete(b.sessionSubIDSet, sub)

	// TODO: publish WAMP meta events
}

// pubEvent sends an event to all subscribers that are not excluded from
// receiving the event.
func (b *broker) pubEvent(pub *Session, msg *wamp.Publish, pubID wamp.ID, subs map[wamp.ID]*Session, excludePublisher, sendTopic bool) {
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

// publishAllowed determines if a message is allowed to be published to a
// subscriber, by looking at any blacklists and whitelists provided with the
// publish message.
func publishAllowed(msg *wamp.Publish, sub *Session) bool {
	if blacklist, ok := msg.Options["exclude"]; ok {
		blacklist := blacklist.([]string)
		for i := range blacklist {
			if blacklist[i] == string(sub.ID) {
				return false
			}
		}
	}
	if blacklist, ok := msg.Options["exclude_authid"]; ok {
		blacklist := blacklist.([]string)
		for i := range blacklist {
			if blacklist[i] == sub.AuthID {
				return false
			}
		}
	}
	if blacklist, ok := msg.Options["exclude_authrole"]; ok {
		blacklist := blacklist.([]string)
		for i := range blacklist {
			if blacklist[i] == sub.AuthRole {
				return false
			}
		}
	}
	if whitelist, ok := msg.Options["eligible"]; ok {
		eligible := false
		whitelist := whitelist.([]string)
		for i := range whitelist {
			if whitelist[i] == string(sub.ID) {
				eligible = true
				break
			}
		}
		if !eligible {
			return false
		}
	}
	if whitelist, ok := msg.Options["eligible_authid"]; ok {
		eligible := false
		whitelist := whitelist.([]string)
		for i := range whitelist {
			if whitelist[i] == sub.AuthID {
				eligible = true
				break
			}
		}
		if !eligible {
			return false
		}
	}
	if whitelist, ok := msg.Options["eligible_authrole"]; ok {
		eligible := false
		whitelist := whitelist.([]string)
		for i := range whitelist {
			if whitelist[i] == sub.AuthRole {
				eligible = true
				break
			}
		}
		if !eligible {
			return false
		}
	}
	return true
}
