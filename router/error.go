package router

import "errors"

var (
	ErrPPTNotSupportedByPeer = errors.New("peer is trying to use Payload PassThru Mode while it was not announced during HELLO handshake")
)
