VERSION = $(shell git describe --tags)

.PHONY: all clean install uninstall

all: nexusd

nexusd: ../wamp/*.go ../wamp/crsign/*.go ../router/*.go ../router/auth/*.go ../transport/*.go ../transport/serialize/*.go ./*.go
	@go build -ldflags="-X 'github.com/gammazero/nexus/v3/router.Version=${VERSION}'"
	@echo "===> built $@"

install: nexusd
	@go install
	@echo "===> installed $^"

uninstall:
	@rm -f $(GOPATH)/bin/nexusd
	@echo "===> removed $(GOPATH)/bin/nexusd"

clean:
	@rm -f nexusd
