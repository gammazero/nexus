SERVICE_NAME	= nexusd
SERVICE_BIN	= $(BIN_DIR)/$(SERVICE_NAME)
BIN_DIR	= ./bin
LIB_DIR	= ./lib

.PHONY: all $(SERVICE_BIN) clean

all: vet $(SERVICE_BIN)

vet:
	go vet -all -composites=false -shadow=true ./...

test:
	go test -race ./...
	go test -race ./aat -websocket
	go test ./aat -websocket -msgpack

$(BIN_DIR)/:
	mkdir -p $@

$(SERVICE_BIN): test $(BIN_DIR)/
	go build -i -o $(SERVICE_BIN) .

clean:
	rm -rf $(BIN_DIR)
