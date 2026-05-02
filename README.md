# crypto-sniping-bot

> **Deterministic, event-driven microstructure sniper system** with controlled risk, testability, and adaptive learning. Built as a modular monolith on the skeleton-parallel framework.

**Core Invariant:**

```
Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality
```

If any factor → 0, profit → 0. Every change must preserve every factor.

---

## Quick Start

```bash
# Build
go build ./...

# Run tests
go test ./...

# Start server
go run ./cmd serve

# Run migrations
go run ./cmd migrate up
```

---

## Makefile Targets

All common operations are wrapped in `make` targets. Run `make <target>`.

### Build & Quality

| Target              | Description                                        |
| ------------------- | -------------------------------------------------- |
| `make build`        | Compile to `bin/crypto-sniping-bot`                |
| `make run`          | `go run ./cmd serve`                               |
| `make test`         | Run all tests with race detector                   |
| `make test-cover`   | Tests + HTML coverage report                       |
| `make vet`          | `go vet ./...`                                     |
| `make lint`         | `golangci-lint run ./...` (requires golangci-lint) |
| `make tidy`         | `go mod tidy`                                      |
| `make migrate-up`   | Apply all pending migrations                       |
| `make migrate-down` | Roll back last migration                           |
| `make clean`        | Remove `bin/`, `coverage.out`, `coverage.html`     |
| `make quality`      | Runs `vet` + `lint` + `test` (full gate)           |

### Docker

| Target              | Description                                          |
| ------------------- | ---------------------------------------------------- |
| `make docker-build` | Build Docker image without starting services         |
| `make docker-up`    | Build image + start all services in detached mode    |
| `make docker-down`  | Stop all services (data volume preserved)            |
| `make docker-clean` | Stop all services **and delete the database volume** |
| `make docker-logs`  | Tail live bot logs (`docker compose logs -f bot`)    |

### Log Collection & Pre-Analysis

Collect live bot logs unattended, pre-analyse them against all 10 PRS dimensions, and write a structured summary ready to paste into a Copilot log-reviewer session.

```bash
make log-collect              # collect for 60 min (default), then write summary
make log-collect MINS=5       # quick smoke test — 5 min window
make log-collect MINS=10 SVC=bot
make log-latest               # print the most recent summary to stdout
make log-list                 # list all collected session summaries
```

**Workflow:**

1. Run `make log-collect` in any terminal — it runs completely unattended.
2. After the window elapses (or you press Ctrl-C), it writes two files to `output/logs/`:
   - `summary_<TIMESTAMP>.txt` — human-readable findings (PRS score, stage counts, stub detection, invariant checks)
   - `prs_<TIMESTAMP>.json` — machine-readable PRS breakdown
3. Open a new Copilot chat and paste:
   > _"Review this using the log-reviewer skill:"_ followed by the summary content.
4. Copilot runs a full log-reviewer analysis (Verdict + Findings + Plan + Confirmation Gate).

The script detects: pipeline stage completeness (L0–L10), stubbed numeric fields, R4 invariants (join_timeout, duplicate event IDs, missing trace_id), PANIC/FATAL lines, and reject-rate spikes. `output/logs/` is gitignored.

---

## Pipeline

The system runs a 11-layer sequential pipeline per market instance (e.g. `eth-uniswap-v2`):

```
[INGEST] → [DQ FILTER] → [FEATURES] → [EDGE] → [P/S/L MODELS] → [VALIDATE] → [SELECT] → [CAPITAL] → [EXECUTE] → [POSITION] → [LEARN]
    ↓            ↓            ↓           ↓            ↓               ↓           ↓           ↓            ↓            ↓          ↓
MarketData  DataQuality  FeatureDTO   EdgeDTO    Prob/Slip/Lat   ValidatedEdge Selection Allocation  Execution  PositionState Learning
   DTO         DTO                                  DTOs             DTO         DTO        DTO       ResultDTO     DTO        Record
```

| Layer | Name                             | Responsibility                                            |
| ----- | -------------------------------- | --------------------------------------------------------- |
| 0     | Detection & Ingestion            | Subscribe to DEX events, emit `MarketDataDTO`             |
| 1     | Data Quality Engine              | Reject rugs, honeypots, wash trades, fake liquidity       |
| 2     | Feature Extraction               | Normalize to `FeatureDTO` + `FeatureConfidence`           |
| 3     | Signal & Edge Discovery          | `NEW_LAUNCH_EDGE` detection, adaptive momentum threshold  |
| 4     | Probability / Slippage / Latency | P(success), slippage impact, latency decay models         |
| 5     | Edge Validation                  | EV gate, adaptive thresholds, mode-gated filters          |
| 6     | Selection Engine                 | Top-K greedy + diversity + exploration band               |
| 7     | Capital Engine                   | Size ∝ Score × P × Confidence, cohort multipliers         |
| 8     | Execution Engine                 | Wallet sharding, prebuilt calldata, bounded parallelism   |
| 9     | Position Engine                  | TP1/TP2/SL/TIME exits, adaptive per cohort                |
| 10    | Learning Engine                  | FP/FN analysis, cohort updates, bounded adaptive learning |

See [`docs/architecture.md`](docs/architecture.md) for the full design and invariants.

---

## Repository Structure

```
crypto-sniping-bot/
├── cmd/                        # Entry points (serve, migrate, telegram)
├── contracts/                  # Immutable DTO definitions — the ONLY inter-module coupling
│   ├── contracts.go            # Base types and shared constants
│   ├── market_data.go          # MarketDataDTO (Layer 0 output)
│   ├── data_quality.go         # DataQualityDTO (Layer 1 output)
│   ├── feature.go              # FeatureDTO + FeatureConfidence (Layer 2 output)
│   ├── edge.go                 # EdgeDTO (Layer 3 output)
│   ├── probability.go          # ProbabilityEstimateDTO (Layer 4 output)
│   ├── validated_edge.go       # ValidatedEdgeDTO (Layer 5 output)
│   ├── selection.go            # SelectionOutput (Layer 6 output)
│   ├── allocation.go           # AllocationDTO (Layer 7 output)
│   ├── execution.go            # ExecutionResultDTO (Layer 8 output)
│   ├── position.go             # PositionState (Layer 9 output)
│   └── learning_record.go      # LearningRecord (Layer 10 output)
├── database/
│   ├── adapter.go              # Single DB access interface (all modules use this)
│   ├── engines/postgres/       # PostgreSQL adapter implementation
│   └── migrations/             # Append-only SQL migrations (17 files)
├── internal/
│   ├── app/
│   │   ├── config/             # Application config structs (YAML-backed)
│   │   └── web/server.go       # HTTP server + health check endpoint
│   ├── modules/                # Domain modules — one package per pipeline stage
│   │   ├── ingestion/          # Layer 0: EVM DEX event subscription
│   │   ├── ingestion_solana/   # Layer 0: Solana Raydium/PumpFun event subscription
│   │   ├── data_quality/       # Layer 1: Scam/rug/honeypot/wash detection
│   │   ├── features/           # Layer 2: Feature extraction + normalization
│   │   ├── edge/               # Layer 3: Edge detection + creator filters
│   │   ├── models/             # Layer 4: Probability, slippage, latency, congestion models
│   │   ├── validation/         # Layer 5: EV gate + consecutive-pass debounce
│   │   ├── selection/          # Layer 6: Top-K greedy + per-creator dedup
│   │   ├── capital/            # Layer 7: Kelly sizing + cohort multipliers
│   │   ├── execution/          # Layer 8: EVM wallet sharding + tx submission
│   │   ├── execution_solana/   # Layer 8: Solana swap execution
│   │   ├── position/           # Layer 9: TP1/TP2/SL/trailing stop monitoring
│   │   ├── evaluation/         # Layer 9→10: Trade outcome evaluation + sim-diff
│   │   ├── learning/           # Layer 10: Adaptive learning + creator blacklist
│   │   ├── state_machine/      # Token lifecycle state machine
│   │   ├── traceability/       # Four-field trace contract enforcement
│   │   └── health/             # Health check module
│   ├── orchestrator/           # Pipeline orchestration + checkpointing
│   ├── rpc/                    # Multi-endpoint RPC pool + circuit breaker
│   ├── resource_control/       # Drawdown protection + kill switch
│   ├── telegram/               # Event-bus-only Telegram dispatcher
│   └── workers/                # Event bus worker dispatchers
├── config/                     # All tunable parameters — no hardcoded values in code
│   ├── pipeline.yaml           # Pipeline metadata, position, validation, edge, selection
│   ├── capital.yaml            # Kelly sizing, cohort multipliers, exploration budget
│   ├── chains.yaml             # EVM + Solana chain config, RPC endpoints, factories
│   ├── data_quality.yaml       # Scam detector flags, thresholds, risk weights
│   ├── feature.yaml            # Feature extractor config + Phase 11 holder/social
│   ├── probability.yaml        # Probability model consumption rules
│   ├── execution.yaml          # Wallet sharding, concurrency, Solana exec params
│   ├── gas.yaml                # Gas strategy, fee bump config
│   ├── budgets.yaml            # Daily trade budgets per chain/market
│   ├── priority.yaml           # Event priority weights, evaluation flags
│   └── phases.yaml             # Phase definitions, complexity scores, skill assignments
├── scripts/
│   └── run_parallel.sh         # Parallel development orchestrator (3-mode)
├── docs/                       # Architecture specs and implementation roadmap
│   ├── architecture.md         # Single source of truth — system design
│   ├── implementation_roadmap.md # Phase-by-phase build guide (execution-grade)
│   ├── dto_contracts.md        # DTO registry — all fields, types, constraints
│   ├── db_adapter_spec.md      # Database adapter interface + migration strategy
│   ├── orchestrator_spec.md    # Orchestrator execution model, checkpointing, resume
│   ├── PARALLEL_DEV.md         # Parallel development operator guide
│   ├── AGENTS_AND_SKILLS.md    # Agent and skill registry
│   ├── PROGRESS_REPORT.md      # Implementation phase progress tracking
│   └── STARTER_GUIDE.md        # Getting started playbook (beginner-friendly)
├── tests/
│   ├── unit/                   # Unit tests per module
│   ├── integration/            # End-to-end pipeline wiring tests
│   └── modules/                # Module-level contract tests
├── .github/
│   ├── skills/                 # 50+ skills — pre-digested knowledge packages for agents
│   └── copilot-instructions.md # Agent architectural constraints
└── output/                     # Generated artifacts (gitignored)
```

---

## Implementation Phases

| Phase | Name                      | Group | Description                                                                                           |
| ----- | ------------------------- | ----- | ----------------------------------------------------------------------------------------------------- |
| 0     | core-infrastructure       | A     | DB, event bus, adapter, orchestrator, migrations                                                      |
| 1     | dex-ingestion             | A     | DEX scanner, RPC pool, `MarketDataDTO` → event bus                                                    |
| 2     | first-trade-pipeline      | A     | End-to-end: DQ → Feature → Edge → Capital → Execute → Position                                        |
| 3     | evaluation-correctness    | B     | Learning records, strategy versioning, replay engine                                                  |
| 4     | signal-quality            | B     | Full probability models, feature stability, anti-manipulation                                         |
| 5     | learning-engine           | B     | Adaptive learning, strategy decay detection, auto-disable                                             |
| 6     | production-hardening      | C     | Observability, drawdown protection, wallet sharding, Telegram                                         |
| 7     | solana-market             | C     | Solana Raydium/PumpFun ingestion + execution, hybrid transport                                        |
| 8     | production-hardening-r2   | C     | Reconciliation, partition leasing, DLQ, crash recovery, reorg guard                                   |
| 9     | profitability-restoration | D     | Real scam detection, live features, Kelly sizing, price-feed monitor                                  |
| 10    | reference-repo-r1         | D     | Trailing stop, consecutive-pass gate, bonding curve filter                                            |
| 11    | reference-repo-r2         | D     | Creator hygiene, holder concentration, social links, congestion slippage, per-creator dedup, sim-diff |

**Group rules:** Groups A → B → C → D are sequential. Phases within the same group may run in parallel.

See [`docs/implementation_roadmap.md`](docs/implementation_roadmap.md) for exact file paths, function signatures, and exit criteria per phase.

---

## Parallel Development

```bash
# Mode 1 — Full parallel (fastest, highest token cost)
./scripts/run_parallel.sh --mode 1

# Mode 2 — Token-optimized sequential (single agent session)
./scripts/run_parallel.sh --mode 2

# Mode 3 — Hybrid (parallel within groups, sequential across groups)
./scripts/run_parallel.sh --mode 3

# Single phase
./scripts/run_parallel.sh --mode 2 --phase 0
```

Each phase runs through a mandatory agent pipeline:

```
phase-builder → dto-guardian → integration → security-auditor → test-builder → refactor (remediation only)
```

See [`docs/PARALLEL_DEV.md`](docs/PARALLEL_DEV.md) for the full operator guide, model routing, phase grouping, and parallel safety invariants.

---

## Module Rules

1. Modules communicate **only** through immutable DTOs defined in `contracts/`
2. No module imports another module's internals — only `contracts/` types
3. All database access goes through `database.Adapter` — no direct driver imports in `internal/modules/`
4. The orchestrator is the **only** component that calls modules and writes to the database
5. Same input + same config = identical output (determinism enforced)
6. All IDs are content-addressable: `entity_id = SHA256(content_signature)[:16]`

---

## Configuration

All thresholds, paths, and tunable parameters live in `config/`. No hardcoded values in code.

Key files:

| File                       | Purpose                                                                   |
| -------------------------- | ------------------------------------------------------------------------- |
| `config/pipeline.yaml`     | Pipeline metadata, position exits, edge thresholds, validation, selection |
| `config/capital.yaml`      | Kelly sizing, cohort multipliers, exploration budget, failure policy      |
| `config/chains.yaml`       | EVM chain RPC endpoints, factory addresses; Solana programs + transport   |
| `config/data_quality.yaml` | Scam detector flags, thresholds, risk weights (Layer 1)                   |
| `config/feature.yaml`      | Feature extractor config incl. holder concentration and social links      |
| `config/probability.yaml`  | Probability model consumption rules, fallback alerts                      |
| `config/execution.yaml`    | Wallet sharding, concurrency limits, Solana execution params              |
| `config/gas.yaml`          | Gas strategy, fee bump config, priority fee settings                      |
| `config/budgets.yaml`      | Daily trade budgets per chain/market                                      |
| `config/priority.yaml`     | Event priority weights, evaluation flags (e.g. `enable_simulation_diff`)  |
| `config/phases.yaml`       | Phase definitions, complexity scores, group assignments, per-phase skills |

See [`docs/architecture.md § 7`](docs/architecture.md) for operational mode configs (`STRICT` / `BALANCED` / `EXPLORATION`).

---

## Documentation

| Document                                                           | Purpose                                                       |
| ------------------------------------------------------------------ | ------------------------------------------------------------- |
| [`docs/architecture.md`](docs/architecture.md)                     | System design, pipeline layers, invariants, operational modes |
| [`docs/implementation_roadmap.md`](docs/implementation_roadmap.md) | Phase-by-phase build guide with exact code                    |
| [`docs/dto_contracts.md`](docs/dto_contracts.md)                   | DTO registry — all fields, types, constraints                 |
| [`docs/db_adapter_spec.md`](docs/db_adapter_spec.md)               | Database adapter interface + migration strategy               |
| [`docs/orchestrator_spec.md`](docs/orchestrator_spec.md)           | Orchestrator execution model and checkpointing                |
| [`docs/PARALLEL_DEV.md`](docs/PARALLEL_DEV.md)                     | Parallel development operator guide                           |
| [`docs/AGENTS_AND_SKILLS.md`](docs/AGENTS_AND_SKILLS.md)           | Agent and skill registry (41 skills, 12 agents)               |
| [`docs/PROGRESS_REPORT.md`](docs/PROGRESS_REPORT.md)               | Live phase progress tracking                                  |
