package helpers

import "github.com/gammazero/nexus/v3/wamp"

// Special ID for meta session.
const MetaID = wamp.ID(1)

func PPTOptionsToDetails(options wamp.Dict, details wamp.Dict) {
	details[wamp.OptPPTScheme] = options[wamp.OptPPTScheme].(string)
	if val, ok := options[wamp.OptPPTSerializer]; ok {
		details[wamp.OptPPTSerializer] = val.(string)
	}
	if val, ok := options[wamp.OptPPTCipher]; ok {
		details[wamp.OptPPTCipher] = val.(string)
	}
	if val, ok := options[wamp.OptPPTKeyId]; ok {
		details[wamp.OptPPTKeyId] = val.(string)
	}
}
