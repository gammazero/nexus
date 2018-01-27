/*
Package wamp defines all message types, data types, and reserved URI values
defined by the WAMP specification.

*/
package wamp

type MessageType int

// Message is a generic container for a WAMP message.
type Message interface {
	MessageType() MessageType
}

// Dict is a dictionary that maps keys to objects in a WAMP message.
type Dict map[string]interface{}

// List represents a list of items in a WAMP message.
type List []interface{}

// Message Codes and Direction
const (
	//                           // | Pub  | Brk  | Subs | Calr | Dealr| Calee|
	//                           // | ---- | ---- | ---- | ---- | ---- | ---- |
	HELLO        MessageType = 1 // | Tx   | Rx   | Tx   | Tx   | Rx   | Tx   |
	WELCOME      MessageType = 2 // | Rx   | Tx   | Rx   | Rx   | Tx   | Rx   |
	ABORT        MessageType = 3 // | Rx   | TxRx | Rx   | Rx   | TxRx | Rx   |
	CHALLENGE    MessageType = 4 // |      |      |      |      |      |      |
	AUTHENTICATE MessageType = 5 // |      |      |      |      |      |      |
	GOODBYE      MessageType = 6 // | TxRx | TxRx | TxRx | TxRx | TxRx | TxRx |
	// HEARTBEAT    MessageType = 7 |      |      |      |      |      |      |
	ERROR MessageType = 8 //        | Rx   | Tx   | Rx   | Rx   | TxRx | TxRx |
	//                              |      |      |      |      |      |      |
	PUBLISH   MessageType = 16 //   | Tx   | Rx   |      |      |      |      |
	PUBLISHED MessageType = 17 //   | Rx   | Tx   |      |      |      |      |
	//                              |      |      |      |      |      |      |
	SUBSCRIBE    MessageType = 32 //|      | Rx   | Tx   |      |      |      |
	SUBSCRIBED   MessageType = 33 //|      | Tx   | Rx   |      |      |      |
	UNSUBSCRIBE  MessageType = 34 //|      | Rx   | Tx   |      |      |      |
	UNSUBSCRIBED MessageType = 35 //|      | Tx   | Rx   |      |      |      |
	EVENT        MessageType = 36 //|      | Tx   | Rx   |      |      |      |
	//                              |      |      |      |      |      |      |
	CALL   MessageType = 48 //      |      |      |      | Tx   | Rx   |      |
	CANCEL MessageType = 49 //      |      |      |      | Tx   | Rx   |      |
	RESULT MessageType = 50 //      |      |      |      | Rx   | Tx   |      |
	//                              |      |      |      |      |      |      |
	REGISTER     MessageType = 64 //|      |      |      |      | Rx   | Tx   |
	REGISTERED   MessageType = 65 //|      |      |      |      | Tx   | Rx   |
	UNREGISTER   MessageType = 66 //|      |      |      |      | Rx   | Tx   |
	UNREGISTERED MessageType = 67 //|      |      |      |      | Tx   | Rx   |
	INVOCATION   MessageType = 68 //|      |      |      |      | Tx   | Rx   |
	INTERRUPT    MessageType = 69 //|      |      |      |      | Tx   | Rx   |
	YIELD        MessageType = 70 //|      |      |      |      | Rx   | Tx   |
)

var mtStrings = map[MessageType]string{
	HELLO:        "HELLO",
	WELCOME:      "WELCOME",
	ABORT:        "ABORT",
	CHALLENGE:    "CHALLENGE",
	AUTHENTICATE: "AUTHENTICATE",
	GOODBYE:      "GOODBYE",
	// HEARTBEAT: "HEARTBEAT",
	ERROR:        "ERROR",
	PUBLISH:      "PUBLISH",
	PUBLISHED:    "PUBLISHED",
	SUBSCRIBE:    "SUBSCRIBE",
	SUBSCRIBED:   "SUBSCRIBED",
	UNSUBSCRIBE:  "UNSUBSCRIBE",
	UNSUBSCRIBED: "UNSUBSCRIBED",
	EVENT:        "EVENT",
	CALL:         "CALL",
	CANCEL:       "CANCEL",
	RESULT:       "RESULT",
	REGISTER:     "REGISTER",
	REGISTERED:   "REGISTERED",
	UNREGISTER:   "UNREGISTER",
	UNREGISTERED: "UNREGISTERED",
	INVOCATION:   "INVOCATION",
	INTERRUPT:    "INTERRUPT",
	YIELD:        "YIELD",
}

// String returns the message type string.
func (mt MessageType) String() string { return mtStrings[mt] }

// NewMessage returns an empty message of the type specified.
func NewMessage(t MessageType) Message {
	switch t {
	case HELLO:
		return &Hello{}
	case WELCOME:
		return &Welcome{}
	case ABORT:
		return &Abort{}
	case CHALLENGE:
		return &Challenge{}
	case AUTHENTICATE:
		return &Authenticate{}
	case GOODBYE:
		return &Goodbye{}
	// case HEARTBEAT: return &Heartbeat{}
	case ERROR:
		return &Error{}
	case PUBLISH:
		return &Publish{}
	case PUBLISHED:
		return &Published{}
	case SUBSCRIBE:
		return &Subscribe{}
	case SUBSCRIBED:
		return &Subscribed{}
	case UNSUBSCRIBE:
		return &Unsubscribe{}
	case UNSUBSCRIBED:
		return &Unsubscribed{}
	case EVENT:
		return &Event{}
	case CALL:
		return &Call{}
	case CANCEL:
		return &Cancel{}
	case RESULT:
		return &Result{}
	case REGISTER:
		return &Register{}
	case REGISTERED:
		return &Registered{}
	case UNREGISTER:
		return &Unregister{}
	case UNREGISTERED:
		return &Unregistered{}
	case INVOCATION:
		return &Invocation{}
	case INTERRUPT:
		return &Interrupt{}
	case YIELD:
		return &Yield{}
	}
	return nil
}

// ----- Session Lifecycle -----

// Sent by a Client to initiate opening of a WAMP session to a Router attaching
// to a Realm.
//
// [HELLO, Realm|uri, Details|dict]
type Hello struct {
	Realm   URI
	Details Dict
}

func (msg *Hello) MessageType() MessageType { return HELLO }

// Sent by a Router to accept a Client. The WAMP session is now open.
//
// [WELCOME, Session|id, Details|dict]
type Welcome struct {
	ID      ID
	Details Dict
}

func (msg *Welcome) MessageType() MessageType { return WELCOME }

// Sent by a Peer to abort the opening of a WAMP session. No response is
// expected.
//
// [ABORT, Details|dict, Reason|uri]
type Abort struct {
	Details Dict
	Reason  URI
}

func (msg *Abort) MessageType() MessageType { return ABORT }

// Sent by a Peer to close a previously opened WAMP session. Must be echo'ed by
// the receiving Peer.
//
// [GOODBYE, Details|dict, Reason|uri]
type Goodbye struct {
	Details Dict
	Reason  URI
}

func (msg *Goodbye) MessageType() MessageType { return GOODBYE }

// Error reply sent by a Peer as an error response to different kinds of
// requests.
//
// [ERROR, REQUEST.Type|int, REQUEST.Request|id, Details|dict, Error|uri]
// [ERROR, REQUEST.Type|int, REQUEST.Request|id, Details|dict, Error|uri,
//     Arguments|list]
// [ERROR, REQUEST.Type|int, REQUEST.Request|id, Details|dict, Error|uri,
//     Arguments|list, ArgumentsKw|dict]
type Error struct {
	Type        MessageType
	Request     ID
	Details     Dict
	Error       URI
	Arguments   List `wamp:"omitempty"`
	ArgumentsKw Dict `wamp:"omitempty"`
}

func (msg *Error) MessageType() MessageType { return ERROR }

// ----- Publish & Subscribe -----

// Sent by a Publisher to a Broker to publish an event.
//
// [PUBLISH, Request|id, Options|dict, Topic|uri]
// [PUBLISH, Request|id, Options|dict, Topic|uri, Arguments|list]
// [PUBLISH, Request|id, Options|dict, Topic|uri, Arguments|list,
//     ArgumentsKw|dict]
type Publish struct {
	Request     ID
	Options     Dict
	Topic       URI
	Arguments   List `wamp:"omitempty"`
	ArgumentsKw Dict `wamp:"omitempty"`
}

func (msg *Publish) MessageType() MessageType { return PUBLISH }

// Acknowledge sent by a Broker to a Publisher for acknowledged publications.
//
// [PUBLISHED, PUBLISH.Request|id, Publication|id]
type Published struct {
	Request     ID
	Publication ID
}

func (msg *Published) MessageType() MessageType { return PUBLISHED }

// Subscribe request sent by a Subscriber to a Broker to subscribe to a topic.
//
// [SUBSCRIBE, Request|id, Options|dict, Topic|uri]
type Subscribe struct {
	Request ID
	Options Dict
	Topic   URI
}

func (msg *Subscribe) MessageType() MessageType { return SUBSCRIBE }

// Acknowledge sent by a Broker to a Subscriber to acknowledge a subscription.
//
// [SUBSCRIBED, SUBSCRIBE.Request|id, Subscription|id]
type Subscribed struct {
	Request      ID
	Subscription ID
}

func (msg *Subscribed) MessageType() MessageType { return SUBSCRIBED }

// Unsubscribe request sent by a Subscriber to a Broker to unsubscribe a
// subscription.
//
// [UNSUBSCRIBE, Request|id, SUBSCRIBED.Subscription|id]
type Unsubscribe struct {
	Request      ID
	Subscription ID
}

func (msg *Unsubscribe) MessageType() MessageType { return UNSUBSCRIBE }

// Acknowledge sent by a Broker to a Subscriber to acknowledge unsubscription.
//
// [UNSUBSCRIBED, UNSUBSCRIBE.Request|id]
type Unsubscribed struct {
	Request ID
}

func (msg *Unsubscribed) MessageType() MessageType { return UNSUBSCRIBED }

// Event dispatched by Broker to Subscribers for subscriptions the event was
// matching.
//
// [EVENT, SUBSCRIBED.Subscription|id, PUBLISHED.Publication|id, Details|dict]
// [EVENT, SUBSCRIBED.Subscription|id, PUBLISHED.Publication|id, Details|dict,
//     PUBLISH.Arguments|list]
// [EVENT, SUBSCRIBED.Subscription|id, PUBLISHED.Publication|id, Details|dict,
//     PUBLISH.Arguments|list, PUBLISH.ArgumentsKw|dict]
type Event struct {
	Subscription ID
	Publication  ID
	Details      Dict
	Arguments    List `wamp:"omitempty"`
	ArgumentsKw  Dict `wamp:"omitempty"`
}

func (msg *Event) MessageType() MessageType { return EVENT }

// ----- Router Remote Procedure Calls -----

// A Callee announces the availability of an endpoint implementing a procedure
// with a Dealer by sending a REGISTER message:
//
// [REGISTER, Request|id, Options|dict, Procedure|uri]
type Register struct {
	Request   ID
	Options   Dict
	Procedure URI
}

func (msg *Register) MessageType() MessageType { return REGISTER }

// If the Dealer is able to fulfill and allowing the registration, it answers
// by sending a REGISTERED message to the Callee:
//
// [REGISTERED, REGISTER.Request|id, Registration|id]
type Registered struct {
	Request      ID
	Registration ID
}

func (msg *Registered) MessageType() MessageType { return REGISTERED }

// When a Callee is no longer willing to provide an implementation of the
// registered procedure, it sends an UNREGISTER message to the Dealer:
//
// [UNREGISTER, Request|id, REGISTERED.Registration|id]
type Unregister struct {
	Request      ID
	Registration ID
}

func (msg *Unregister) MessageType() MessageType { return UNREGISTER }

// Upon successful unregistration, the Dealer sends an UNREGISTERED message to
// the Callee:
//
// [UNREGISTERED, UNREGISTER.Request|id]
type Unregistered struct {
	Request ID
}

func (msg *Unregistered) MessageType() MessageType { return UNREGISTERED }

// When a Caller wishes to call a remote procedure, it sends a CALL message to
// a Dealer:
//
// [CALL, Request|id, Options|dict, Procedure|uri]
// [CALL, Request|id, Options|dict, Procedure|uri, Arguments|list]
// [CALL, Request|id, Options|dict, Procedure|uri, Arguments|list,
//     ArgumentsKw|dict]
type Call struct {
	Request     ID
	Options     Dict
	Procedure   URI
	Arguments   List `wamp:"omitempty"`
	ArgumentsKw Dict `wamp:"omitempty"`
}

func (msg *Call) MessageType() MessageType { return CALL }

// If the Dealer is able to fulfill (mediate) the call and it allows the call,
// it sends a INVOCATION message to the respective Callee implementing the
// procedure:
//
// [INVOCATION, Request|id, REGISTERED.Registration|id, Details|dict]
// [INVOCATION, Request|id, REGISTERED.Registration|id, Details|dict,
//     CALL.Arguments|list]
// [INVOCATION, Request|id, REGISTERED.Registration|id, Details|dict,
//     CALL.Arguments|list, CALL.ArgumentsKw|dict]
type Invocation struct {
	Request      ID
	Registration ID
	Details      Dict
	Arguments    List `wamp:"omitempty"`
	ArgumentsKw  Dict `wamp:"omitempty"`
}

func (msg *Invocation) MessageType() MessageType { return INVOCATION }

// If the Callee is able to successfully process and finish the execution of
// the call, it answers by sending a YIELD message to the Dealer:
//
// [YIELD, INVOCATION.Request|id, Options|dict]
// [YIELD, INVOCATION.Request|id, Options|dict, Arguments|list]
// [YIELD, INVOCATION.Request|id, Options|dict, Arguments|list,
//     ArgumentsKw|dict]
type Yield struct {
	Request     ID
	Options     Dict
	Arguments   List `wamp:"omitempty"`
	ArgumentsKw Dict `wamp:"omitempty"`
}

func (msg *Yield) MessageType() MessageType { return YIELD }

// The Dealer will then send a RESULT message to the original Caller:
//
// [RESULT, CALL.Request|id, Details|dict]
// [RESULT, CALL.Request|id, Details|dict, YIELD.Arguments|list]
// [RESULT, CALL.Request|id, Details|dict, YIELD.Arguments|list,
//     YIELD.ArgumentsKw|dict]
type Result struct {
	Request     ID
	Details     Dict
	Arguments   List `wamp:"omitempty"`
	ArgumentsKw Dict `wamp:"omitempty"`
}

func (msg *Result) MessageType() MessageType { return RESULT }

// ----- Advanced Profile Messages -----

// The CHALLENGE message is used with certain Authentication Methods. During
// authenticated session establishment, a Router sends a challenge message.
//
// [CHALLENGE, AuthMethod|string, Extra|dict]
type Challenge struct {
	AuthMethod string
	Extra      Dict
}

func (msg *Challenge) MessageType() MessageType { return CHALLENGE }

// The AUTHENTICATE message is used with certain Authentication Methods. A
// Client having received a challenge is expected to respond by sending a
// signature or token.
//
// [AUTHENTICATE, Signature|string, Extra|dict]
type Authenticate struct {
	Signature string
	Extra     Dict
}

func (msg *Authenticate) MessageType() MessageType { return AUTHENTICATE }

// The CANCEL message is used with the Call Canceling advanced feature. A
// Caller can cancel an issued call actively by sending a cancel message to
// the Dealer.
//
// [CANCEL, CALL.Request|id, Options|dict]
type Cancel struct {
	Request ID
	Options Dict
}

func (msg *Cancel) MessageType() MessageType { return CANCEL }

// The INTERRUPT message is used with the Call Canceling advanced feature. Upon
// receiving a cancel for a pending call, a Dealer will issue an interrupt to
// the Callee.
//
// [INTERRUPT, INVOCATION.Request|id, Options|dict]
type Interrupt struct {
	Request ID
	Options Dict
}

func (msg *Interrupt) MessageType() MessageType { return INTERRUPT }

// IsGoodbyeAck checks if the message is an ack to end of session.  This is
// used by transports to avoid logging an error if unable to send a goodbye
// acknowledgment to a client, since the client may not have waited for the
// acknowledgment.
func IsGoodbyeAck(msg Message) bool {
	if gb, ok := msg.(*Goodbye); ok {
		if gb.Reason == ErrGoodbyeAndOut {
			return true
		}
	}
	return false
}
