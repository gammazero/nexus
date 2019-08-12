package client

import "errors"

var (
	ErrAlreadyClosed = errors.New("already closed")
	ErrCallerNoProg  = errors.New("caller not accepting progressive results")
	ErrClosed        = errors.New("client closed")
	ErrNotConn       = errors.New("client not connected")
	ErrNotRegistered = errors.New("not registered for procedure")
	ErrNotSubscribed = errors.New("not subscribed to topic")
	ErrReplyTimeout  = errors.New("timeout waiting for reply")
	ErrRouterNoRoles = errors.New("router did not announce any supported roles")
)
