BIN := pulse
PREFIX ?= /opt/homebrew
REPO := AryanAg08/pulse
# Overridden on a tagged build: make build VERSION=v0.1.0
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test fmt vet install uninstall clean check ci release formula

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

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

# release tags the current commit and pushes it. The tarball GitHub generates
# from the tag is what the formula points at, so the tag must exist and be
# pushed before `make formula` can compute its checksum.
release:
	@test "$(VERSION)" != "dev" || { echo "usage: make release VERSION=v0.1.0"; exit 1; }
	@git diff --quiet || { echo "working tree is dirty; commit first"; exit 1; }
	git tag -a $(VERSION) -m "pulse $(VERSION)"
	git push origin $(VERSION)
	@echo "tagged and pushed $(VERSION) — now run: make formula VERSION=$(VERSION)"

# formula renders the Homebrew formula with the checksum of the released
# tarball. Run it after `make release`.
formula:
	@test "$(VERSION)" != "dev" || { echo "usage: make formula VERSION=v0.1.0"; exit 1; }
	@url="https://github.com/$(REPO)/archive/refs/tags/$(VERSION).tar.gz"; \
	echo "fetching $$url"; \
	tmp=$$(mktemp); \
	code=$$(curl -sL -o "$$tmp" -w '%{http_code}' "$$url"); \
	if [ "$$code" != "200" ]; then \
		rm -f "$$tmp"; \
		echo "HTTP $$code fetching the tarball."; \
		echo "A private repository returns 404 to anonymous requests, which is"; \
		echo "also what brew will get. Make the repo public, or host the"; \
		echo "release elsewhere, before publishing a formula."; \
		exit 1; \
	fi; \
	case "$$(file -b --mime-type "$$tmp")" in application/gzip|application/x-gzip) ;; \
		*) rm -f "$$tmp"; echo "downloaded file is not a gzip archive"; exit 1;; esac; \
	sha=$$(shasum -a 256 "$$tmp" | cut -d' ' -f1); \
	rm -f "$$tmp"; \
	sed -e "s|@URL@|$$url|" -e "s|@SHA@|$$sha|" -e "s|@VERSION@|$(VERSION)|" \
		dist/homebrew/pulse.rb.tmpl > dist/homebrew/pulse.rb; \
	echo "wrote dist/homebrew/pulse.rb (sha256 $$sha)"
