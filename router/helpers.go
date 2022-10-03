package router

import "github.com/gammazero/nexus/v3/wamp"

func pptOptionsToDetails(options wamp.Dict, details wamp.Dict) {
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
