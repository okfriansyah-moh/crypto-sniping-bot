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

# ── Log collection + pre-analysis ────────────────────────────────────────────
# Collect live bot logs for DURATION minutes, pre-analyse them, and write
# a structured summary to output/logs/ ready for the log-reviewer Copilot session.
#
# Usage:
#   make log-collect            # collect for 60 min (default)
#   make log-collect MINS=10    # collect for 10 min (quick test)
#   make log-collect MINS=5 SVC=bot
#
# After it finishes, open a new Copilot chat and paste the summary file path.

MINS ?= 60
SVC  ?= bot

.PHONY: log-collect
log-collect:
	@echo "Starting log collection for $(MINS) minute(s) on service '$(SVC)'..."
	@echo "Press Ctrl-C to stop early — the summary is written on exit."
	@bash scripts/collect_logs.sh $(MINS) $(SVC)

# Show the most recent summary file in the terminal.
.PHONY: log-latest
log-latest:
	@ls -t output/logs/summary_*.txt 2>/dev/null | head -1 | xargs cat || \
	  echo "No summary files found. Run 'make log-collect' first."

# List all collected log sessions.
.PHONY: log-list
log-list:
	@ls -lt output/logs/summary_*.txt 2>/dev/null || echo "No summaries yet."

# Re-run Phase 2+3 analysis on a previously collected raw log file.
# Usage: make log-analyze LOG=output/logs/raw_20260501_232953.log
.PHONY: log-analyze
log-analyze:
	@[[ -n "$(LOG)" ]] || (echo "Usage: make log-analyze LOG=output/logs/raw_TIMESTAMP.log" && exit 1)
	@bash scripts/collect_logs.sh --analyze $(LOG)

# ── Docker targets ────────────────────────────────────────────────────────────

# Build the Docker image (does not start any services).
.PHONY: docker-build
docker-build:
	docker compose build

# Build and start all services in detached mode.
.PHONY: docker-up
docker-up:
	docker compose up --build -d

# Stop all services (data volume is preserved).
.PHONY: docker-down
docker-down:
	docker compose down

# Stop all services and delete the database volume.
.PHONY: docker-clean
docker-clean:
	docker compose down -v

# Tail bot logs.
.PHONY: docker-logs
docker-logs:
	docker compose logs -f bot

# All quality gates
.PHONY: quality
quality: vet lint test
