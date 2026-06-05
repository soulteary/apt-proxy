.DEFAULT_GOAL := help

GO            ?= go
GOFLAGS       ?= -trimpath
PKG           := ./...
BINARY        := apt-proxy
CMD_DIR       := ./cmd/apt-proxy
DIST_DIR      := dist
COVERAGE_OUT  := coverage.out

VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT        ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE          ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS       ?= -s -w \
                 -X main.version=$(VERSION) \
                 -X main.commit=$(COMMIT) \
                 -X main.date=$(DATE)

.PHONY: help
help:
	@echo "Common targets:"
	@echo "  make build        - build binary into ./$(DIST_DIR)/$(BINARY)"
	@echo "  make run          - go run the daemon"
	@echo "  make test         - run unit tests with -race -short"
	@echo "  make test-full    - run all tests (including integration)"
	@echo "  make cover        - generate coverage profile + summary"
	@echo "  make lint         - run golangci-lint"
	@echo "  make fmt          - run gofmt + goimports"
	@echo "  make vet          - run go vet"
	@echo "  make tidy         - go mod tidy"
	@echo "  make vuln         - run govulncheck"
	@echo "  make docker       - build local docker image"
	@echo "  make release-snap - goreleaser snapshot (no publish)"
	@echo "  make clean        - remove build artifacts"

.PHONY: build
build:
	mkdir -p $(DIST_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(DIST_DIR)/$(BINARY) $(CMD_DIR)

.PHONY: run
run:
	$(GO) run $(CMD_DIR)

.PHONY: test
test:
	$(GO) test -race -short -count=1 $(PKG)

.PHONY: test-full
test-full:
	$(GO) test -race -count=1 $(PKG)

.PHONY: test-integration
test-integration:
	$(GO) test -tags=integration -race -count=1 -timeout=10m ./tests/integration/...

.PHONY: cover
cover:
	$(GO) test -race -short -count=1 -coverprofile=$(COVERAGE_OUT) -covermode=atomic $(PKG)
	$(GO) tool cover -func=$(COVERAGE_OUT) | tail -n 1

.PHONY: lint
lint:
	@which golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found; install: https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	}
	golangci-lint run --timeout=5m

.PHONY: fmt
fmt:
	gofmt -s -w .
	@which goimports >/dev/null 2>&1 && goimports -w . || true

.PHONY: vet
vet:
	$(GO) vet $(PKG)

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: vuln
vuln:
	@which govulncheck >/dev/null 2>&1 || $(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck $(PKG)

.PHONY: docker
docker:
	docker build -t $(BINARY):$(VERSION) -f docker/Dockerfile .

.PHONY: release-snap
release-snap:
	@which goreleaser >/dev/null 2>&1 || { \
		echo "goreleaser not found; install: https://goreleaser.com/install/"; \
		exit 1; \
	}
	goreleaser release --snapshot --clean

.PHONY: clean
clean:
	rm -rf $(DIST_DIR) $(COVERAGE_OUT)
