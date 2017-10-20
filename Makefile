SERVICE_DIR = nexusd

.PHONY: all vet test service clean

all: vet test service

vet:
	go vet -all -composites=false -shadow=true ./...

test:
	go get github.com/fortytw2/leaktest
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
	go test ./aat -scheme=wss
	go test ./aat -scheme=tcps

service: $(SERVICE_DIR)/nexusd

$(SERVICE_DIR)/nexusd:
	@cd $(SERVICE_DIR); go build
	@echo "===> built $(SERVICE_DIR)/nexusd"

clean:
	@rm -f $(SERVICE_DIR)/nexusd
	@rm -f $(SERVICE_DIR)/*.log
