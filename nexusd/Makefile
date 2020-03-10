VERSION = $(shell git describe --tags)

.PHONY: all clean

all: nexusd

nexusd:
	@go build -ldflags="-X 'github.com/gammazero/nexus/v3/router.Version=${VERSION}'"

clean:
	@rm -f nexusd
