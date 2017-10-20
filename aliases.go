package nexus

import (
	"time"

	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/router/auth"
	"github.com/gammazero/nexus/stdlog"
)

type RouterConfig = router.RouterConfig
type RealmConfig = router.RealmConfig
type Router = router.Router

func NewRouter(config *RouterConfig, logger stdlog.StdLog) (Router, error) {
	return router.NewRouter(config, logger)
}

type WebsocketServer = router.WebsocketServer

func NewWebsocketServer(r Router) *WebsocketServer {
	return router.NewWebsocketServer(r)
}

type RawSocketServer = router.RawSocketServer

func NewRawSocketServer(r Router, recvLimit int, keepalive time.Duration) *RawSocketServer {
	return router.NewRawSocketServer(r, recvLimit, keepalive)
}

type Authorizer = router.Authorizer

type Authenticator = auth.Authenticator
