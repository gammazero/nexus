SERVICE_DIR = nexusd

.PHONY: all vet test service clean install uninstall

all: vet test service

vet:
	go vet -all -composites=false ./...

test:
	go build ./examples/...
	go test -race ./...
	go test -race ./test -scheme=ws
	go test -race ./test -scheme=unix
	go test ./test -scheme=ws -serialize=msgpack
	go test ./test -scheme=tcp -serialize=msgpack
	go test ./test -scheme=ws -serialize=cbor -compress
	go test ./test -scheme=tcp -serialize=cbor
	go test ./test -scheme=wss
	go test ./test -scheme=tcps

benchmark:
	go test ./test -run=XXX -bench=.
	go test ./test -run=XXX -bench=. -scheme=ws
	go test ./test -run=XXX -bench=. -scheme=wss
	go test ./test -run=XXX -bench=. -scheme=tcp
	go test ./test -run=XXX -bench=. -scheme=tcps
	go test ./test -run=XXX -bench=. -scheme=ws -compress

service: $(SERVICE_DIR)/nexusd

$(SERVICE_DIR)/nexusd:
	@$(MAKE) -C $(SERVICE_DIR)

install: service
	@$(MAKE) -C $(SERVICE_DIR) install

uninstall:
	@$(MAKE) -C $(SERVICE_DIR) uninstall

clean:
	@$(MAKE) -C $(SERVICE_DIR) clean
	@rm -f $(SERVICE_DIR)/*.log
	@GO111MODULE=off go clean ./...
	@GO111MODULE=off go clean -cache
