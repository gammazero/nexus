package client

import "github.com/gammazero/nexus/v3/wamp"

// Features supported by nexus client.
var clientRoles = wamp.Dict{ //nolint:gochecknoglobals
	wamp.RolePublisher: wamp.Dict{
		wamp.DetailFeatures: wamp.Dict{
			wamp.FeatureSubBlackWhiteListing: true,
			wamp.FeaturePubExclusion:         true,
			wamp.FeaturePubIdent:             true,
			wamp.FeaturePayloadPassthruMode:  true,
		},
	},
	wamp.RoleSubscriber: wamp.Dict{
		wamp.DetailFeatures: wamp.Dict{
			wamp.FeaturePatternSub:          true,
			wamp.FeaturePubIdent:            true,
			wamp.FeaturePayloadPassthruMode: true,
		},
	},
	wamp.RoleCallee: wamp.Dict{
		wamp.DetailFeatures: wamp.Dict{
			wamp.FeaturePatternBasedReg:     true,
			wamp.FeatureSharedReg:           true,
			wamp.FeatureCallCanceling:       true,
			wamp.FeatureCallTimeout:         true,
			wamp.FeatureCallerIdent:         true,
			wamp.FeatureProgCallInvocations: true,
			wamp.FeatureProgCallResults:     true,
			wamp.FeaturePayloadPassthruMode: true,
		},
	},
	wamp.RoleCaller: wamp.Dict{
		wamp.DetailFeatures: wamp.Dict{
			wamp.FeatureCallCanceling:       true,
			wamp.FeatureCallTimeout:         true,
			wamp.FeatureCallerIdent:         true,
			wamp.FeatureProgCallInvocations: true,
			wamp.FeatureProgCallResults:     true,
			wamp.FeaturePayloadPassthruMode: true,
		},
	},
}
