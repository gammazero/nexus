package client

import "time"

const (
	// Error URIs returned by this client.
	errUnexpectedMessageType = "nexus.error.unexpected_message_type"
	errNoAuthHandler         = "nexus.error.no_handler_for_authmethod"
	errAuthFailure           = "nexus.error.authentication_failure"

	// Time client will wait for expected router response if not specified.
	defaultResponseTimeout = 5 * time.Second
)
