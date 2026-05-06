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

## Production Readiness Score (PRS)

The PRS is a 0–100 health score computed automatically by `make log-collect`. It measures whether the full pipeline is running end-to-end with real (non-stubbed) values.

| Tier     | Score  | Meaning                                                        |
| -------- | ------ | -------------------------------------------------------------- |
| HEALTHY  | 80–100 | All 10 dimensions active. Safe to trade.                       |
| DEGRADED | 50–79  | Some dimensions inactive. Reduce position sizes.               |
| BLOCKED  | 0–49   | Critical stubs detected or 100% reject rate. **Do not trade.** |

### PRS Dimensions

| Dim | Name                  | What it checks                                                 | 10/10 requires                                  |
| --- | --------------------- | -------------------------------------------------------------- | ----------------------------------------------- |
| D1  | Pipeline completeness | All L0–L10 stage events present in the log window              | Every stage emitting at least 1 event           |
| D2  | Data quality          | `risk_score` varies; DQ decisions present                      | `risk_score` not a constant across all tokens   |
| D3  | Feature signals       | `features_extracted` events with non-stub field values         | Feature worker running with live RPC data       |
| D4  | Probability model     | `probability_used` varies (not fixed prior 0.35)               | Logistic model live; prior fallback disabled    |
| D5  | Slippage model        | `p50_bps` / `p95_bps` vary per token                           | CPMM model active; not falling back to buckets  |
| D6  | Capital safety        | Max exposure and concurrent position limits respected          | No over-exposure log lines detected             |
| D7  | Execution engine      | `execution_submitted` + `execution_confirmed` events present   | Live swap txs submitted and confirmed on-chain  |
| D8  | Learning/adaptation   | `learning_record` events; strategy version increments          | Evaluation + learning cycle running             |
| D9  | Probe coverage        | Solana/EVM enrichment probes enabled in `config/pipeline.yaml` | At least 2 probe types active (`probes:` block) |
| D10 | Live P&L evidence     | `position_closed` with realized `pnl_bps` logged               | At least 1 closed position with real P&L        |

### Non-Tolerable Patterns

Any of the following auto-sets PRS tier to **BLOCKED**:

| Pattern                            | Root cause                                             | Fix                                                          |
| ---------------------------------- | ------------------------------------------------------ | ------------------------------------------------------------ |
| `probability_used` constant (0.35) | Validation using prior; probability worker not writing | Verify probability worker running and writing to DB          |
| `p50_bps` / `p95_bps` constant     | Slippage using fallback bucket, CPMM not active        | Check `models.slippage.model_version_id` in `pipeline.yaml`  |
| `ev_bps` constant negative         | No live probability; prior EV always negative          | Fix D4 (probability model) first                             |
| 100% reject rate at L5             | EV always below `ev_threshold_bps`                     | Lower `validation.ev_threshold_bps` or fix probability model |
| Missing `trace_id` > 10%           | Events emitted without trace propagation               | Check traceability module and `PropagateTrace` calls         |
| Duplicate `event_id`s              | Non-idempotent event emission                          | Verify SHA256 content-addressable ID generation              |
| `heartbeat_zero_emitted` > 5       | Worker running but emitting nothing                    | Check worker eligibility filters and DB query results        |

---

## Pipeline

The system runs a 11-layer sequential pipeline per market instance (e.g. `eth-uniswap-v2`):

```
[INGEST] → [DQ FILTER] → [FEATURES] → [EDGE] → [P/S/L MODELS] → [VALIDATE] → [SELECT] → [CAPITAL] → [EXECUTE] → [POSITION] → [LEARN]
    ↓            ↓            ↓           ↓            ↓               ↓           ↓           ↓            ↓            ↓          ↓
MarketData  DataQuality  FeatureDTO   EdgeDTO    Prob/Slip/Lat   ValidatedEdge Selection Allocation  Execution  PositionState Learning
   DTO         DTO                                  DTOs             DTO         DTO        DTO       ResultDTO     DTO        Record
```

| Layer | Name                             | Responsibility                                                                                 |
| ----- | -------------------------------- | ---------------------------------------------------------------------------------------------- |
| 0     | Detection & Ingestion            | Subscribe to DEX events, emit `MarketDataDTO`                                                  |
| 0.5   | Rescan Worker                    | Re-emit temporally-missed tokens by age band; disabled by default (see `config/pipeline.yaml`) |
| 1     | Data Quality Engine              | Reject rugs, honeypots, wash trades, fake liquidity                                            |
| 2     | Feature Extraction               | Normalize to `FeatureDTO` + `FeatureConfidence`                                                |
| 3     | Signal & Edge Discovery          | `NEW_LAUNCH_EDGE` detection, adaptive momentum threshold                                       |
| 4     | Probability / Slippage / Latency | P(success), slippage impact, latency decay models                                              |
| 5     | Edge Validation                  | EV gate, adaptive thresholds, mode-gated filters                                               |
| 6     | Selection Engine                 | Top-K greedy + diversity + exploration band                                                    |
| 7     | Capital Engine                   | Size ∝ Score × P × Confidence, cohort multipliers                                              |
| 8     | Execution Engine                 | Wallet sharding, prebuilt calldata, bounded parallelism                                        |
| 9     | Position Engine                  | TP1/TP2/SL/TIME exits, adaptive per cohort                                                     |
| 10    | Learning Engine                  | FP/FN analysis, cohort updates, bounded adaptive learning                                      |

### Pipeline Stage Log Keys

Every stage emits a structured JSON log line. Use these `msg` field values with `make log-collect` to monitor pipeline health per the PRS dimensions above:

| Stage | `msg` field           | Key fields                                                              |
| ----- | --------------------- | ----------------------------------------------------------------------- |
| L0    | `ingestion`           | `token_address`, `chain`, `transport` (`live` or `rescan_<band>`)       |
| L1    | `dq_decision`         | `result` (PASS/REJECT/RISKY_PASS), `risk_score`, `reason`               |
| L2    | `features_extracted`  | `liquidity_score`, `tx_velocity_score`, `holder_dist`, `wallet_entropy` |
| L3    | `edge_decision`       | `edge_type`, `edge_strength`, `result` (ACCEPT/REJECT)                  |
| L4    | `probability_scored`  | `probability`, `model_version_id`                                       |
| L4    | `slippage_estimated`  | `p50_bps`, `p95_bps`, `model_version_id`                                |
| L5    | `validation_decision` | `result`, `ev_bps`, `probability_used`, `reject_reason`                 |
| L6    | `selection_decision`  | `selected`, `rank`, `diversity_score`                                   |
| L7    | `allocation_decision` | `size_usd`, `kelly_fraction`, `cohort`                                  |
| L8    | `execution_submitted` | `tx_hash`, `wallet`, `nonce`                                            |
| L8    | `execution_confirmed` | `tx_hash`, `block_number`, `gas_used`                                   |
| L9    | `position_opened`     | `entry_price`, `tp1_bps`, `tp2_bps`, `sl_bps`                           |
| L9    | `position_closed`     | `exit_reason` (TP1/TP2/SL/TIME/FORCE), `pnl_bps`, `hold_seconds`        |
| L10   | `learning_record`     | `outcome`, `loss_bucket`, `strategy_version_id`                         |

See [`docs/architecture.md`](docs/architecture.md) for the full design and invariants.

---

## Telegram Operator Commands

All operator interaction goes through Telegram. The bot connects via `SNIPER_TELEGRAM_BOT_TOKEN` + `SNIPER_TELEGRAM_CHAT_ID`. Destructive commands require `SNIPER_TELEGRAM_ALLOWED_USERS` to be set (comma-separated user IDs).

### Read-only

| Command              | Description                                                                             |
| -------------------- | --------------------------------------------------------------------------------------- |
| `/status`            | System mode, drawdown state, open positions, exposure, active strategy                  |
| `/pnl`               | Realized + unrealized PnL, win rate, stuck position count                               |
| `/positions`         | All open positions: full address, age, entry price, current price, PnL%                 |
| `/position <prefix>` | Detail view for one position by ID or token address prefix                              |
| `/health`            | Worker heartbeats, kill switch state, halt reason                                       |
| `/pipeline`          | Cumulative token validation funnel stats and recent tickers (last 24h)                  |
| `/rescan`            | Rescan worker config, band eligibility thresholds, last 24h emission counts by band     |
| `/dq [hours]`        | Data quality stats: total decisions, rug reject rate, DQ funnel pass rate (default 24h) |
| `/dlq`               | Dead-letter queue: last 10 failed events, reason breakdown, retry counts                |
| `/version`           | Active strategy version ID and promotion status                                         |
| `/help`              | Show all available commands                                                             |

### Operational

| Command           | Description                                                          |
| ----------------- | -------------------------------------------------------------------- |
| `/mode strict`    | Switch to STRICT mode — conservative thresholds, ≤1% explore budget  |
| `/mode balanced`  | Switch to BALANCED mode — default operating mode                     |
| `/mode explore`   | Switch to EXPLORATION mode — relaxed thresholds, 3–5% explore budget |
| `/enable_trading` | Clear the safety-net halt set after Phase 6 shadow run               |

### Destructive (requires `SNIPER_TELEGRAM_ALLOWED_USERS`)

| Command                 | Description                                                    |
| ----------------------- | -------------------------------------------------------------- |
| `/kill`                 | Activate kill switch — halts all trading immediately           |
| `/resume`               | Clear kill switch — resumes trading                            |
| `/force_close <prefix>` | Force-exit all open positions for a token (logged, reversible) |

All mode transitions and destructive commands are logged with timestamp and user ID. No remote code execution is permitted via Telegram.

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
│       ├── run_rescan.go       # Layer 0.5: time-banded rescan worker (Phase 10)
│       └── ...                 # Other event bus workers
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

| Phase | Name                      | Group | Description                                                                                                                                     |
| ----- | ------------------------- | ----- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| 0     | core-infrastructure       | A     | DB, event bus, adapter, orchestrator, migrations                                                                                                |
| 1     | dex-ingestion             | A     | DEX scanner, RPC pool, `MarketDataDTO` → event bus                                                                                              |
| 2     | first-trade-pipeline      | A     | End-to-end: DQ → Feature → Edge → Capital → Execute → Position                                                                                  |
| 3     | evaluation-correctness    | B     | Learning records, strategy versioning, replay engine                                                                                            |
| 4     | signal-quality            | B     | Full probability models, feature stability, anti-manipulation                                                                                   |
| 5     | learning-engine           | B     | Adaptive learning, strategy decay detection, auto-disable                                                                                       |
| 6     | production-hardening      | C     | Observability, drawdown protection, wallet sharding, Telegram                                                                                   |
| 7     | solana-market             | C     | Solana Raydium/PumpFun ingestion + execution, hybrid transport                                                                                  |
| 8     | production-hardening-r2   | C     | Reconciliation, partition leasing, DLQ, crash recovery, reorg guard                                                                             |
| 9     | profitability-restoration | D     | Real scam detection, live features, Kelly sizing, price-feed monitor                                                                            |
| 10    | reference-repo-r1         | D     | Trailing stop, consecutive-pass gate, bonding curve filter; **rescan worker** (Layer 0.5) — time-banded re-emission of temporally-missed tokens |
| 10.5  | observability-r1          | D     | Cumulative pipeline funnel fix (`/pipeline`), new Telegram commands: `/rescan`, `/dq`, `/dlq`                                                   |
| 11    | reference-repo-r2         | D     | Creator hygiene, holder concentration, social links, congestion slippage, per-creator dedup, sim-diff                                           |

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

### Key Parameters by Layer

All values below live in `config/pipeline.yaml` unless noted.

#### Layer 0.5 — Rescan Worker (`rescan:`)

| Parameter                        | Default        | Description                                                                 |
| -------------------------------- | -------------- | --------------------------------------------------------------------------- |
| `enabled`                        | `false`        | Set `true` to activate; disabled by default                                 |
| `interval_seconds`               | `60`           | Poll cadence — lower = faster second-chance detection                       |
| `max_per_band_per_tick`          | `100`          | Max tokens re-emitted per age band per tick                                 |
| `skip_open_positions`            | `true`         | Never rescan tokens already in an open position                             |
| Bands                            | 15m/30m/45m/1h | Age windows; each band has `min_age_seconds`, `max_age_seconds`, `priority` |
| `eligibility.max_honeypot_score` | `0.5`          | Tokens above this score are excluded from rescan                            |
| `eligibility.max_rug_score`      | `0.65`         | Tokens above this score are excluded from rescan                            |
| `eligibility.max_buy_tax_bps`    | `3000`         | Tokens above this tax are excluded from rescan                              |

#### Layer 3 — Edge Detection (`edge:`)

| Parameter                   | Default | Description                                       |
| --------------------------- | ------- | ------------------------------------------------- |
| `min_velocity_score`        | `0.3`   | Minimum tx velocity to detect an edge             |
| `min_liquidity_score`       | `0.2`   | Minimum liquidity score to detect an edge         |
| `new_launch_window_seconds` | `300`   | Pool age ceiling for `NEW_LAUNCH_EDGE` (5 min)    |
| `min_price_momentum`        | `0.4`   | Cold-start floor for adaptive momentum threshold  |
| `min_volume_momentum`       | `0.3`   | Hard gate on `VolumeMomentum` for `MOMENTUM_EDGE` |
| `momentum_quantile`         | `0.7`   | Rolling-window quantile for adaptive threshold    |
| `baseline_min_samples`      | `30`    | Below this, use `min_price_momentum` (cold start) |

#### Layer 4 — Probability / Slippage / Latency (`models:`)

| Parameter                       | Default         | Description                                          |
| ------------------------------- | --------------- | ---------------------------------------------------- |
| `probability.model_version_id`  | `logistic-v1`   | Model stamp on `ProbabilityEstimateDTO`              |
| `probability.bias`              | `-0.5`          | Logistic model bias (negative = conservative)        |
| `probability.w_liquidity_score` | `1.5`           | Feature weight for liquidity in probability model    |
| `slippage.model_version_id`     | `cpmm-alpha-v1` | CPMM closed-form slippage model                      |
| `slippage.max_slippage_bps`     | `5000`          | Hard upper bound on estimated slippage (50%)         |
| `slippage.volatility_z`         | `1.65`          | Normal 95th-percentile multiplier for p95            |
| `latency.fallback_p50_ms`       | `250`           | Used when sample count < `min_samples`               |
| `latency.fallback_p95_ms`       | `800`           | Used when sample count < `min_samples`               |
| `model_join_timeout_ms`         | `0`             | `0` = fall back to priors immediately if unavailable |

#### Layer 5 — Edge Validation (`validation:`)

| Parameter                     | Default | Description                                                       |
| ----------------------------- | ------- | ----------------------------------------------------------------- |
| `ev_threshold_bps`            | `100`   | Minimum expected value to ACCEPT (1%); lower if 100% reject rate  |
| `prior_probability`           | `0.35`  | Fixed prior P(success) — replaced by live model when D4 is active |
| `prior_gain_bps`              | `3000`  | Expected gain on win used in EV formula                           |
| `prior_loss_bps`              | `4000`  | Expected loss on loss used in EV formula                          |
| `join_timeout_ms`             | `250`   | Max wait for probability/slippage events before using prior       |
| `required_consecutive_passes` | `1`     | Debounce gate — set > 1 to require N consecutive passes           |

#### Layer 6 — Selection (`selection:`)

| Parameter                   | Default | Description                             |
| --------------------------- | ------- | --------------------------------------- |
| `max_open_positions`        | `10`    | Global cap on concurrent open positions |
| `max_positions_per_creator` | `0`     | Per-creator dedup cap; `0` disables     |

#### Layer 7 — Capital (`capital:` in `pipeline.yaml`, tuned in `capital.yaml`)

| Parameter                  | Default | Description                                          |
| -------------------------- | ------- | ---------------------------------------------------- |
| `fixed_entry_size_usd`     | `5.0`   | Base entry size per trade                            |
| `max_total_exposure_usd`   | `500.0` | Hard cap on total open exposure                      |
| `max_concurrent_positions` | `1`     | Phase 2 cap; raise after execution is validated live |
| `max_size_usd`             | `100.0` | Single-position USD ceiling                          |

#### Layer 9 — Position Exits (`position:`)

| Parameter               | Default | Description                          |
| ----------------------- | ------- | ------------------------------------ |
| `tp1_bps`               | `1500`  | Take-profit 1 — partial exit at +15% |
| `tp2_bps`               | `4000`  | Take-profit 2 — full exit at +40%    |
| `sl_bps`                | `500`   | Stop-loss — exit at -5%              |
| `max_hold_seconds`      | `300`   | Time stop — force exit at 5 min      |
| `poll_interval_seconds` | `5`     | Position monitor poll cadence        |

#### Layer 10 — Learning (`learning:`)

| Parameter                    | Default | Description                                                 |
| ---------------------------- | ------- | ----------------------------------------------------------- |
| `min_sample_size`            | `30`    | Minimum records before updating any parameter               |
| `max_delta_pct`              | `0.10`  | Max fractional change per parameter per cycle (10%)         |
| `eval_window_seconds`        | `86400` | Lookback window for evaluation (24h)                        |
| `observation_window_seconds` | `3600`  | How long to track a rejected token's return (FN window)     |
| `fn_gain_threshold_pct`      | `0.10`  | Min return to classify a rejected trade as a false negative |

#### Probes — Enrichment (`probes:`)

| Parameter                           | Default | Description                                              |
| ----------------------------------- | ------- | -------------------------------------------------------- |
| `probes.enabled`                    | `true`  | Master switch for all probes                             |
| `probes.solana_authorities.enabled` | `true`  | Mint/freeze authority check (sets DQ authority flags)    |
| `probes.solana_pumpfun_lp.enabled`  | `true`  | Live bonding curve reserves + USD liquidity              |
| `probes.solana_holder_dist.enabled` | `true`  | Top-5 holder concentration via `getTokenLargestAccounts` |
| `probes.honeypot_sim.enabled`       | `false` | EVM honeypot simulation; requires deployed contract      |
| `probes.evm_pair_reserves.enabled`  | `false` | Live Uniswap-V2 `getReserves`; requires EVM RPC          |

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
