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
├── cmd/                        # Entry points (serve, migrate)
├── contracts/
│   └── contracts.go            # Immutable DTO definitions — the ONLY inter-module coupling
├── database/
│   ├── adapter.go              # Single DB access interface (all modules use this)
│   └── migrations/             # Append-only SQL migrations
├── internal/
│   ├── app/
│   │   ├── config/config.go    # Application config wiring
│   │   └── web/server.go       # HTTP server bootstrap
│   ├── modules/                # Domain modules — one package per pipeline stage
│   │   └── health/             # Reference implementation (vertical slice pattern)
│   ├── orchestrator/           # Pipeline orchestration + checkpointing
│   └── workers/                # Event bus worker dispatchers
├── config/
│   └── phases.yaml             # Phase definitions, complexity scores, skill assignments
├── scripts/
│   └── run_parallel.sh         # Parallel development orchestrator (3-mode)
├── docs/                       # Architecture specs and implementation roadmap
│   ├── architecture.md         # Single source of truth — system design
│   ├── implementation_roadmap.md # Phase-by-phase build guide (execution-grade)
│   ├── dto_contracts.md        # DTO registry — field-level definitions
│   ├── db_adapter_spec.md      # Database adapter interface and migration strategy
│   ├── orchestrator_spec.md    # Orchestrator execution model, checkpointing, resume
│   ├── PARALLEL_DEV.md         # Parallel development operator guide
│   ├── AGENTS_AND_SKILLS.md    # Agent and skill registry
│   ├── PROGRESS_REPORT.md      # Implementation phase progress tracking
│   └── STARTER_GUIDE.md        # Getting started playbook
├── .github/
│   ├── skills/                 # 41 skills — pre-digested knowledge packages for agents
│   └── copilot-instructions.md # Agent architectural constraints
└── output/                     # Generated artifacts (gitignored)
```

---

## Implementation Phases

| Phase | Name                   | Group | Description                                                    |
| ----- | ---------------------- | ----- | -------------------------------------------------------------- |
| 0     | core-infrastructure    | A     | DB, event bus, adapter, orchestrator, migrations               |
| 1     | dex-ingestion          | A     | DEX scanner, RPC pool, `MarketDataDTO` → event bus             |
| 2     | first-trade-pipeline   | A     | End-to-end: DQ → Feature → Edge → Capital → Execute → Position |
| 3     | evaluation-correctness | B     | Learning records, strategy versioning, replay engine           |
| 4     | signal-quality         | B     | Full probability models, feature stability, anti-manipulation  |
| 5     | learning-engine        | B     | Adaptive learning, strategy decay detection, auto-disable      |
| 6     | production-hardening   | C     | Observability, drawdown protection, wallet sharding, Telegram  |

**Group rules:** Group A phases are sequential (each requires the prior). Group B phases (3–5) may run in parallel. Group C runs only after all Group B phases pass.

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

- `config/phases.yaml` — phase definitions, complexity scores, group assignments, per-phase skills
- See [`docs/architecture.md § 7`](docs/architecture.md) for operational mode configs (`STRICT` / `BALANCED` / `EXPLORATION`)

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
