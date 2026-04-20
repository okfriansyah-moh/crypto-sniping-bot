# crypto-sniping-bot
# ─────────────────────────────────────────

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
GOMOD=$(GOCMD) mod
BINARY_NAME=crypto-sniping-bot

# Build
.PHONY: build
build:
	$(GOBUILD) -o bin/$(BINARY_NAME) ./cmd/

# Run
.PHONY: run
run:
	$(GOCMD) run ./cmd/ serve

# Test
.PHONY: test
test:
	$(GOTEST) -v -race -count=1 ./...

# Test with coverage
.PHONY: test-cover
test-cover:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Vet
.PHONY: vet
vet:
	$(GOVET) ./...

# Lint (requires golangci-lint)
.PHONY: lint
lint:
	golangci-lint run ./...

# Tidy modules
.PHONY: tidy
tidy:
	$(GOMOD) tidy

# Database migration
.PHONY: migrate-up
migrate-up:
	$(GOCMD) run ./cmd/ migrate up

.PHONY: migrate-down
migrate-down:
	$(GOCMD) run ./cmd/ migrate down

# Clean
.PHONY: clean
clean:
	rm -rf bin/ coverage.out coverage.html

# All quality gates
.PHONY: quality
quality: vet lint test
