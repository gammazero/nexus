package router

import (
	"errors"
	"fmt"
)

var (
	ErrPPTNotSupportedByPeer = errors.New("peer is trying to use Payload PassThru Mode while it was not" +
		" announced during HELLO handshake")
)

type configError struct {
	Err error
}

func (e configError) Error() string {
	return fmt.Sprintf("configuration error: %v", e.Err)
}

func (e configError) Unwrap() error {
	return e.Err
}
