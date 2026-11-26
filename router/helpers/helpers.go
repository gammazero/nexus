package helpers

import (
	"github.com/gammazero/nexus/v3/wamp"
)

// MetaID is a Special ID reserved for the meta session.
const MetaID = wamp.ID(1)

// error message for peer not announcing PPT support.
const PPTNotSupportedMsg = "peer is trying to use Payload PassThru Mode without announcing support for it"

// TrustedAuthRole is the authrole of the realm's meta session, and of local
// clients that join the realm without authenticating. A trusted callee may
// request caller disclosure even when the realm does not allow it.
const TrustedAuthRole = "trusted"

func PPTOptionsToDetails(options wamp.Dict, details wamp.Dict) {
	details[wamp.OptPPTScheme] = options[wamp.OptPPTScheme].(string)
	if val, ok := options[wamp.OptPPTSerializer]; ok {
		details[wamp.OptPPTSerializer] = val.(string)
	}
	if val, ok := options[wamp.OptPPTCipher]; ok {
		details[wamp.OptPPTCipher] = val.(string)
	}
	if val, ok := options[wamp.OptPPTKeyID]; ok {
		details[wamp.OptPPTKeyID] = val.(string)
	}
}
