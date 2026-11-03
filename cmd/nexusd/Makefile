VERSION = $(shell git describe --tags)

.PHONY: all build clean install uninstall

all: nexusd

build: nexusd

nexusd: ../../wamp/*.go ../../wamp/crsign/*.go ../../router/*.go ../../router/auth/*.go ../../transport/*.go ../../transport/serialize/*.go ./*.go
	go build -ldflags="-X 'github.com/gammazero/nexus/v3/router.Version=${VERSION}'"

install:
	go install -ldflags="-X 'github.com/gammazero/nexus/v3/router.Version=${VERSION}'"

uninstall:
	rm -f $(GOPATH)/bin/nexusd

clean:
	go clean
