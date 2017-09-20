SERVICE_DIR = nexusd

.PHONY: all vet test service clean

all: vet test service

vet:
	go vet -all -composites=false -shadow=true ./...

test:
	go get github.com/fortytw2/leaktest
	go test -race ./...
	go test -race ./aat -socket=web
	go test ./aat -socket=web -serialize=msgpack
	go test -race ./aat -socket=unix

service: $(SERVICE_DIR)/nexusd

$(SERVICE_DIR)/nexusd:
	@cd $(SERVICE_DIR); go build
	@echo "===> built $(SERVICE_DIR)/nexusd"

clean:
	@rm -f $(SERVICE_DIR)/nexusd
	@rm -f $(SERVICE_DIR)/*.log
