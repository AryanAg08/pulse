BIN := pulse
PREFIX ?= /opt/homebrew

.PHONY: build test fmt vet install uninstall clean check

build:
	go build -o $(BIN) .

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test

install: build
	ln -sf $(CURDIR)/$(BIN) $(PREFIX)/bin/$(BIN)
	@echo "linked $(PREFIX)/bin/$(BIN) -> $(CURDIR)/$(BIN)"

uninstall:
	rm -f $(PREFIX)/bin/$(BIN)

clean:
	rm -f $(BIN)
