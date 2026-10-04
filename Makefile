# Development tasks. `make` builds; `make check` is what CI runs.
BINARY  := truffles
PKG     := ./...
GOFLAGS ?=

.PHONY: all build test race cover fmt fmtcheck vet tidy check clean install

all: build

build: ## Build the binary into the repo root
	go build $(GOFLAGS) -o $(BINARY) ./cmd/truffles

install: ## Install into $GOPATH/bin
	go install ./cmd/truffles

test: ## Run unit tests
	go test $(PKG)

race: ## Run unit tests under the race detector
	go test -race $(PKG)

cover: ## Run tests and report coverage per package
	go test -coverprofile=coverage.out $(PKG)
	go tool cover -func=coverage.out | tail -1

fmt: ## Rewrite sources with gofmt
	gofmt -w .

fmtcheck: ## Fail if anything is not gofmt-clean
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

vet: ## Run go vet
	go vet $(PKG)

tidy: ## Tidy go.mod
	go mod tidy

check: fmtcheck vet test ## Everything CI runs

clean: ## Remove build and coverage artefacts
	rm -f $(BINARY) coverage.out
	rm -rf dist/
