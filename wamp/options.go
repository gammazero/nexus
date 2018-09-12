package wamp

// Consts for message options and option values.
const (
	// Message option keywords.
	OptAcknowledge     = "acknowledge"
	OptDiscloseCaller  = "disclose_caller"
	OptDiscloseMe      = "disclose_me"
	OptError           = "error"
	OptExcludeMe       = "exclude_me"
	OptInvoke          = "invoke"
	OptMatch           = "match"
	OptMode            = "mode"
	OptProcedure       = "procedure"
	OptProgress        = "progress"
	OptReason          = "reason"
	OptReceiveProgress = "receive_progress"
	OptTimeout         = "timeout"

	// Values for URI matching mode.
	MatchExact    = "exact"
	MatchPrefix   = "prefix"
	MatchWildcard = "wildcard"

	// Values for call cancel mode.
	CancelModeKill       = "kill"
	CancelModeKillNoWait = "killnowait"
	CancelModeSkip       = "skip"

	// Values for call invocation policy.
	InvokeSingle     = "single"
	InvokeRoundRobin = "roundrobin"
	InvokeRandom     = "random"
	InvokeFirst      = "first"
	InvokeLast       = "last"

	// Options for subscriber filtering.
	BlacklistKey = "exclude"
	WhitelistKey = "eligible"
)
