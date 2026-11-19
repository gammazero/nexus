package helpers

import (
	"github.com/gammazero/nexus/v3/wamp"
)

// error message for peer not announcing PPT support.
const PPTNotSupportedMsg = "peer is trying to use Payload PassThru Mode without announcing support for it"

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
