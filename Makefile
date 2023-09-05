SERVICE_DIR = nexusd

.PHONY: all vet test service clean install uninstall

all: vet test service

vet:
	go vet -all -composites=false ./...

test:
	go build ./examples/...
	go test -race ./wamp/...
	go test -race ./transport/...
	go test -race ./router/...
	go test -race ./client/...
	go test -race ./aat/...
	go test -race ./aat -scheme=ws
	go test -race ./aat -scheme=unix
	go test ./aat -scheme=ws -serialize=msgpack
	go test ./aat -scheme=tcp -serialize=msgpack
	go test ./aat -scheme=ws -serialize=cbor -compress
	go test ./aat -scheme=tcp -serialize=cbor
	go test ./aat -scheme=wss
	go test ./aat -scheme=tcps

benchmark:
	go test ./aat -run=XXX -bench=.
	go test ./aat -run=XXX -bench=. -scheme=ws
	go test ./aat -run=XXX -bench=. -scheme=wss
	go test ./aat -run=XXX -bench=. -scheme=tcp
	go test ./aat -run=XXX -bench=. -scheme=tcps
	go test ./aat -run=XXX -bench=. -scheme=ws -compress

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
