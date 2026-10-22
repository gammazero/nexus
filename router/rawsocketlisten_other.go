//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package router

import "net"

// listenRawSocket listens on the network address. Recovery from a stale Unix
// socket file is not supported on this platform.
func listenRawSocket(network, address string) (net.Listener, error) {
	return net.Listen(network, address)
}
