VERSION = $(shell git describe --tags)

.PHONY: all clean

all: nexusd

nexusd: ../wamp/*.go ../wamp/crsign/*.go ../router/*.go ../router/auth/*.go ../transport/*.go ../transport/serialize/*.go ./*.go
	@go build -ldflags="-X 'github.com/gammazero/nexus/v3/router.Version=${VERSION}'"
	@echo "===> built $@"

clean:
	@rm -f nexusd
