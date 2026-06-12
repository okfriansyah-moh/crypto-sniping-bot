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

# ── Production Gate Review ────────────────────────────────────────────────────
# Collect live bot logs, compute production-gate-reviewer evidence, and write
# a structured gate-review brief ready for the production-gate-reviewer Copilot
# session.
#
# Usage:
#   make gate-collect               # collect for 60 min (default)
#   make gate-collect MINS=10       # quick smoke test — 10 min window
#   make gate-collect MINS=5 SVC=bot MODE=PIPELINE_PROOF
#   make gate-latest                # print the most recent gate brief to stdout
#   make gate-list                  # list all gate review sessions
#   make gate-analyze LOG=output/logs/gate_raw_TIMESTAMP.log
#
# Output files (output/logs/):
#   gate_raw_<TIMESTAMP>.log        — raw collected JSON log
#   gate_brief_<TIMESTAMP>.txt      — structured gate-review brief (paste into Copilot)
#   gate_evidence_<TIMESTAMP>.json  — machine-readable evidence snapshot

MODE ?=

.PHONY: gate-collect
gate-collect:
	@echo "Starting gate-review log collection for $(MINS) minute(s) on service '$(SVC)'..."
	@echo "Mode override: $(if $(MODE),$(MODE),(auto-detected))"
	@echo "Press Ctrl-C to stop early — the brief is written on exit."
	@bash scripts/gate_review_collect.sh $(MINS) $(SVC) $(MODE)

# Print the most recent gate-review brief.
.PHONY: gate-latest
gate-latest:
	@ls -t output/logs/gate_brief_*.txt 2>/dev/null | head -1 | xargs cat || \
	  echo "No gate brief files found. Run 'make gate-collect' first."

# List all gate-review sessions.
.PHONY: gate-list
gate-list:
	@ls -lt output/logs/gate_brief_*.txt 2>/dev/null || echo "No gate briefs yet."

# Re-run analysis on a previously collected gate raw log.
# Usage: make gate-analyze LOG=output/logs/gate_raw_TIMESTAMP.log
.PHONY: gate-analyze
gate-analyze:
	@[[ -n "$(LOG)" ]] || (echo "Usage: make gate-analyze LOG=output/logs/gate_raw_TIMESTAMP.log" && exit 1)
	@bash scripts/gate_review_collect.sh --analyze $(LOG) $(MODE)

# PIPELINE_PROOF acceptance — collect (optional) then validate latest evidence.
# Usage:
#   make gate-validate                          # validate newest gate_evidence_*.json
#   make gate-validate EVIDENCE=output/logs/gate_evidence_TIMESTAMP.json
#   make gate-proof MINS=30                     # collect 30m then validate (Task 18)
.PHONY: gate-validate
gate-validate:
	@bash scripts/validate_pipeline_proof.sh $(EVIDENCE)

.PHONY: gate-proof
gate-proof:
	@echo "Collecting gate evidence for $(MINS) minute(s), then running pipeline-proof acceptance..."
	@bash scripts/gate_review_collect.sh $(MINS) $(SVC) $(MODE)
	@bash scripts/validate_pipeline_proof.sh

# Phase 2 full §1.1 acceptance (Task 19) — all six success criteria.
.PHONY: phase2-validate
phase2-validate:
	@bash scripts/validate_phase2_acceptance.sh $(EVIDENCE)

.PHONY: phase2-proof
phase2-proof:
	@echo "Collecting gate evidence for $(MINS) minute(s), then running Phase 2 full acceptance..."
	@bash scripts/gate_review_collect.sh $(MINS) $(SVC) $(MODE)
	@bash scripts/validate_phase2_acceptance.sh

# ── Docker targets ────────────────────────────────────────────────────────────

# Build the Docker image (does not start any services).
.PHONY: docker-build
docker-build:
	docker compose build

# Build and start all services in detached mode.
.PHONY: docker-up
docker-up:
	docker compose up --build -d

# Start PostgreSQL only (persistent volume, no bot).
.PHONY: postgres
postgres:
	docker compose up -d db

# Alias: explicit name for Postgres-only startup.
.PHONY: docker-up-postgres
docker-up-postgres: postgres

# Stop all services (data volume is preserved).
.PHONY: docker-down
docker-down:
	docker compose down

# Stop all services and delete the database volume.
.PHONY: docker-clean
docker-clean:
	docker compose down

# Stop all services and delete the database volume (destructive).
.PHONY: docker-clean-all
docker-clean-all:
	docker compose down -v

# Tail bot logs.
.PHONY: docker-logs
docker-logs:
	docker compose logs -f bot

# ── Postgres backup / restore (local + VPS sync) ─────────────────────────────

BACKUP_DIR ?= backups
BACKUP_FILE ?= $(BACKUP_DIR)/sniper_$(shell date +%Y%m%d_%H%M%S).dump
FILE ?=
VPS_HOST ?=
VPS_USER ?= root
VPS_APP_DIR ?= /opt/crypto-sniping-bot

# Create a compressed PostgreSQL dump from local Docker DB.
.PHONY: db-backup
db-backup:
	@mkdir -p "$(BACKUP_DIR)"
	docker compose exec -T db pg_dump -U sniper -d sniper -Fc > "$(BACKUP_FILE)"
	@echo "Backup created: $(BACKUP_FILE)"

# Restore a local dump into local Docker DB.
# Usage: make db-restore FILE=backups/sniper_YYYYMMDD_HHMMSS.dump
.PHONY: db-restore
db-restore:
	@[[ -n "$(FILE)" ]] || (echo "Usage: make db-restore FILE=backups/sniper_YYYYMMDD_HHMMSS.dump" && exit 1)
	@test -f "$(FILE)" || (echo "File not found: $(FILE)" && exit 1)
	cat "$(FILE)" | docker compose exec -T db pg_restore -U sniper -d sniper --clean --if-exists --no-owner --no-privileges
	@echo "Restore completed from: $(FILE)"

# Stream a VPS Docker DB dump directly to local backup file.
# Usage: make db-backup-vps VPS_HOST=1.2.3.4 [VPS_USER=root] [VPS_APP_DIR=/opt/crypto-sniping-bot]
.PHONY: db-backup-vps
db-backup-vps:
	@[[ -n "$(VPS_HOST)" ]] || (echo "Usage: make db-backup-vps VPS_HOST=1.2.3.4" && exit 1)
	@mkdir -p "$(BACKUP_DIR)"
	ssh "$(VPS_USER)@$(VPS_HOST)" "cd $(VPS_APP_DIR) && docker compose exec -T db pg_dump -U sniper -d sniper -Fc" > "$(BACKUP_FILE)"
	@echo "VPS backup downloaded to: $(BACKUP_FILE)"

# Stream a local dump directly into VPS Docker DB.
# Usage: make db-restore-vps VPS_HOST=1.2.3.4 FILE=backups/sniper_YYYYMMDD_HHMMSS.dump
.PHONY: db-restore-vps
db-restore-vps:
	@[[ -n "$(VPS_HOST)" ]] || (echo "Usage: make db-restore-vps VPS_HOST=1.2.3.4 FILE=backups/....dump" && exit 1)
	@[[ -n "$(FILE)" ]] || (echo "Usage: make db-restore-vps VPS_HOST=1.2.3.4 FILE=backups/....dump" && exit 1)
	@test -f "$(FILE)" || (echo "File not found: $(FILE)" && exit 1)
	cat "$(FILE)" | ssh "$(VPS_USER)@$(VPS_HOST)" "cd $(VPS_APP_DIR) && docker compose exec -T db pg_restore -U sniper -d sniper --clean --if-exists --no-owner --no-privileges"
	@echo "VPS restore completed from: $(FILE)"

# All quality gates
.PHONY: quality
quality: vet lint test
