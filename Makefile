SERVICE_DIR = nexusd

.PHONY: all vet test service clean install uninstall test-coverage coverage-badge coverage

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

# Run the full suite once with coverage instrumentation. The transport-matrix
# variants in `test` are not re-run here — they exercise transport edge cases,
# not different code paths in the router/wamp/client packages we measure.
test-coverage:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out | tail -1 | awk '{print $$3}' > coverage.txt
	@printf 'total coverage: %s\n' "$$(cat coverage.txt)"

# Rewrite the README coverage badge line to a shields.io URL with the
# current percentage and a color picked from the same thresholds shields.io
# uses for its built-in coverage badges. shields.io renders the badge on
# the fly, so no SVG file is committed. Idempotent: same percentage → no
# diff. Cross-platform sed -i (works under BSD and GNU sed).
coverage-badge: test-coverage
	@PCT=$$(tr -d '%' < coverage.txt); \
	COLOR=$$(awk -v p="$$PCT" 'BEGIN { \
		if (p<50) print "red"; \
		else if (p<70) print "orange"; \
		else if (p<80) print "yellow"; \
		else if (p<90) print "yellowgreen"; \
		else print "brightgreen" }'); \
	sed -i.bak -E "s|coverage-[0-9.]+%25-[a-z]+|coverage-$${PCT}%25-$${COLOR}|" README.md; \
	rm -f README.md.bak; \
	printf 'coverage badge: %s%% (%s)\n' "$$PCT" "$$COLOR"

# Top-level: tests with coverage + badge regen. Run before pushing.
coverage: coverage-badge

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
