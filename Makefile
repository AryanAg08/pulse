BIN := pulse
PREFIX ?= /opt/homebrew

.PHONY: build test fmt vet install uninstall clean check ci

build:
	go build -o $(BIN) .

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test

# Mirrors .github/workflows/go.yml exactly. Unlike `check`, this verifies
# formatting instead of rewriting it, so it fails the way CI fails.
ci:
	go build -v ./...
	go vet ./...
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; echo "$$unformatted"; exit 1; \
	fi
	go test -race ./...

install: build
	ln -sf $(CURDIR)/$(BIN) $(PREFIX)/bin/$(BIN)
	@echo "linked $(PREFIX)/bin/$(BIN) -> $(CURDIR)/$(BIN)"

uninstall:
	rm -f $(PREFIX)/bin/$(BIN)

clean:
	rm -f $(BIN)
