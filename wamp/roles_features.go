package wamp

const (
	// Roles
	RoleBroker     = "broker"
	RoleDealer     = "dealer"
	RoleCallee     = "callee"
	RoleCaller     = "caller"
	RolePublisher  = "publisher"
	RoleSubscriber = "subscriber"

	// RPC features
	FeatureCallCanceling       = "call_canceling"
	FeatureCallTimeout         = "call_timeout"
	FeatureCallerIdent         = "caller_identification"
	FeaturePatternBasedReg     = "pattern_based_registration"
	FeatureProgCallResults     = "progressive_call_results"
	FeatureProgCallInvocations = "progressive_call_invocations"
	FeatureSessionMetaAPI      = "session_meta_api"
	FeatureSharedReg           = "shared_registration"
	FeatureRegMetaAPI          = "registration_meta_api"
	FeatureTestamentMetaAPI    = "testament_meta_api"

	// PubSub features
	FeaturePatternSub           = "pattern_based_subscription"
	FeaturePubExclusion         = "publisher_exclusion"
	FeaturePubIdent             = "publisher_identification"
	FeatureSubBlackWhiteListing = "subscriber_blackwhite_listing"
	FeatureSubMetaAPI           = "subscription_meta_api"
	FeatureEventHistory         = "event_history"

	// Other features
	FeaturePayloadPassthruMode = "payload_passthru_mode"
)
