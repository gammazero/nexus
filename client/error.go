package client

import "errors"

var (
	ErrAlreadyClosed                = errors.New("already closed")
	ErrCallerNoProg                 = errors.New("caller not accepting progressive results")
	ErrNotConn                      = errors.New("not connected")
	ErrNotRegistered                = errors.New("not registered for procedure")
	ErrNotSubscribed                = errors.New("not subscribed to topic")
	ErrReplyTimeout                 = errors.New("timeout waiting for reply")
	ErrRouterNoRoles                = errors.New("router did not announce any supported roles")
	ErrPPTNotSupportedByRouter      = errors.New("payload passthru mode is not supported by the router")
	ErrPPTNotSupportedByPeer        = errors.New("peer is trying to use Payload PassThru Mode while it was not announced during HELLO handshake") //nolint:lll
	ErrPPTSchemeInvalid             = errors.New("ppt scheme provided is invalid")
	ErrPPTSerializerInvalid         = errors.New("ppt serializer provided is invalid or not supported")
	ErrSerialization                = errors.New("can not serialize/deserialize payload")
	ErrProgCallNotSupportedByRouter = errors.New("progressive call invocations are not supported by the router")
)
