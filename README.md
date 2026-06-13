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

### Build, Quality, and Analysis

| Target              | Description                                                |
| ------------------- | ---------------------------------------------------------- |
| `make build`        | Compile to `bin/crypto-sniping-bot`                        |
| `make run`          | `go run ./cmd serve`                                       |
| `make test`         | Run all tests with race detector                           |
| `make test-cover`   | Tests + HTML coverage report                               |
| `make vet`          | `go vet ./...`                                             |
| `make lint`         | `golangci-lint run ./...` (requires golangci-lint)         |
| `make tidy`         | `go mod tidy`                                              |
| `make migrate-up`   | Apply all pending migrations                               |
| `make migrate-down` | Roll back last migration                                   |
| `make clean`        | Remove `bin/`, `coverage.out`, `coverage.html`             |
| `make quality`      | Runs `vet` + `lint` + `test` (full gate)                   |
| `make log-collect`  | Collect logs + write log-reviewer summary (default 60 min) |
| `make log-latest`   | Print the most recent log-reviewer summary to stdout       |
| `make log-list`     | List all log-reviewer session summaries                    |
| `make log-analyze`  | Re-analyse an existing raw log file (`LOG=path`)           |
| `make gate-collect`  | Collect logs + write production-gate-reviewer brief        |
| `make gate-latest`   | Print the most recent gate-review brief to stdout          |
| `make gate-list`     | List all gate-review sessions                              |
| `make gate-analyze`  | Re-analyse an existing gate raw log (`LOG=path`)           |
| `make gate-validate`  | PIPELINE_PROOF acceptance check on latest evidence JSON    |
| `make gate-proof`     | Collect gate logs, then run pipeline-proof acceptance      |
| `make gate-proof-mock` | Offline L0→L10 proof via synthetic fixture (no Helius)   |
| `make gate-proof-inject` | Live inject + wait for L10 (known-good token, no Helius) |
| `make phase2-validate`| Phase 2 full §1.1 acceptance (six criteria) on evidence    |
| `make phase2-proof`   | Collect gate logs, then run Phase 2 full acceptance        |

### Docker

| Target                                      | Description                                                              |
| ------------------------------------------- | ------------------------------------------------------------------------ |
| `make docker-build`                         | Build Docker image without starting services                             |
| `make docker-up`                            | Build image + run DB migration + hydrate historical profiles + start bot |
| `make postgres`                             | Start PostgreSQL only                                                    |
| `make docker-up-postgres`                   | Alias for `make postgres`                                                |
| `make docker-down`                          | Stop all services (data volume preserved)                                |
| `make docker-clean`                         | Stop all services (data volume preserved)                                |
| `make docker-clean-all`                     | Stop all services **and delete the database volume**                     |
| `make docker-logs`                          | Tail live bot logs (`docker compose logs -f bot`)                        |
| `make db-backup`                            | Create compressed PostgreSQL dump to `backups/`                          |
| `make db-restore FILE=...`                  | Restore local dump into local Docker DB                                  |
| `make db-backup-vps VPS_HOST=...`           | Download VPS DB dump to local `backups/`                                 |
| `make db-restore-vps VPS_HOST=... FILE=...` | Upload local dump and restore into VPS DB                                |

### Docker + Postgres Persistence Scenarios (Beginner)

This section explains when to use each command in real life, with copy-paste flows.

#### Scenario 1: Daily local development without losing DB data

Use this when you are coding/testing on your laptop and want Postgres data to stay across restarts.

1. Start everything:

```bash
make docker-up
```

2. Stop safely at end of session (data preserved):

```bash
make docker-clean
# or
make docker-down
```

3. Next day, continue from previous DB state:

```bash
make docker-up
```

Important: `make docker-clean-all` is destructive. Use it only when you intentionally want a fresh empty database.

---

#### Scenario 2: You only want Postgres running (no bot)

Use this when running SQL checks, ad-hoc analysis, or local tooling against DB only.

```bash
make postgres
# alias:
make docker-up-postgres
```

Then stop later while keeping data:

```bash
make docker-down
```

---

#### Scenario 3: Take a safety snapshot before risky changes

Use this before migrations, config experiments, or branch switches.

```bash
make db-backup
ls -lh backups/
```

If something goes wrong, restore from snapshot:

```bash
make db-restore FILE=backups/sniper_YYYYMMDD_HHMMSS.dump
```

---

#### Scenario 4: Download VPS data to local for analysis/backtest

Use this when your VPS has richer live data and you want the same dataset locally.

```bash
# 1) Pull VPS snapshot to local backups/
make db-backup-vps VPS_HOST=YOUR_VPS_IP VPS_USER=root VPS_APP_DIR=/opt/crypto-sniping-bot

# 2) Ensure local DB container is running
make postgres

# 3) Restore the pulled dump into local DB
make db-restore FILE=backups/sniper_YYYYMMDD_HHMMSS.dump
```

Tip: use `ls -lt backups/` to find the newest dump filename.

---

#### Scenario 5: Upload local dataset to VPS

Use this when you prepared data locally (for example, curated historical dataset) and want VPS to use it.

```bash
# 1) Create local snapshot
make db-backup

# 2) Push and restore into VPS DB
make db-restore-vps VPS_HOST=YOUR_VPS_IP VPS_USER=root VPS_APP_DIR=/opt/crypto-sniping-bot FILE=backups/sniper_YYYYMMDD_HHMMSS.dump
```

Warning: `db-restore-vps` restores with `--clean --if-exists`, so objects in VPS DB are replaced by the dump contents.

---

#### One-file example script (copy-paste)

Save as `scripts/db_sync_example.sh` if you want an automated routine.

```bash
#!/usr/bin/env bash
set -euo pipefail

# ---------- edit these ----------
VPS_HOST="YOUR_VPS_IP"
VPS_USER="root"
VPS_APP_DIR="/opt/crypto-sniping-bot"
# -------------------------------

echo "[1/6] Start local Postgres"
make postgres

echo "[2/6] Backup local DB"
make db-backup

echo "[3/6] Pull VPS DB snapshot"
make db-backup-vps VPS_HOST="$VPS_HOST" VPS_USER="$VPS_USER" VPS_APP_DIR="$VPS_APP_DIR"

LATEST_DUMP="$(ls -t backups/sniper_*.dump | head -1)"
echo "Latest dump: $LATEST_DUMP"

echo "[4/6] Restore latest dump into local DB"
make db-restore FILE="$LATEST_DUMP"

echo "[5/6] (Optional) Push local DB back to VPS"
# Uncomment if needed:
# make db-restore-vps VPS_HOST="$VPS_HOST" VPS_USER="$VPS_USER" VPS_APP_DIR="$VPS_APP_DIR" FILE="$LATEST_DUMP"

echo "[6/6] Done"
```

Run it:

```bash
chmod +x scripts/db_sync_example.sh
./scripts/db_sync_example.sh
```

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

### Production Gate Review

Collect live bot logs unattended, compute production-gate-reviewer evidence, and write a structured gate-review brief ready to paste into a Copilot session using the `production-gate-reviewer` skill.

```bash
make gate-collect              # collect for 60 min (default), then write brief
make gate-collect MINS=5       # quick smoke test — 5 min window
make gate-collect MINS=10 SVC=bot
make gate-collect MINS=10 MODE=PIPELINE_PROOF   # force review mode
make gate-latest               # print the most recent gate brief to stdout
make gate-list                 # list all gate review sessions
make gate-analyze LOG=output/logs/gate_raw_TIMESTAMP.log   # re-analyse existing log
make gate-validate             # validate newest gate_evidence_*.json (PIPELINE_PROOF exit)
make gate-validate EVIDENCE=output/logs/gate_evidence_TIMESTAMP.json
make gate-proof MINS=30        # collect 30m, then run acceptance check in one step
make gate-proof-mock           # offline fixture — prove L0→L10 without Helius (recommended first)
make gate-proof-inject         # inject known-good token, wait for L10, validate (stack must be up)
make phase2-validate           # full Phase 2 §1.1 gate (six criteria)
make phase2-proof MINS=30      # collect 30m, then run full Phase 2 acceptance
```

**Workflow:**

1. Run `make gate-collect` in any terminal — it runs completely unattended.
2. After the window elapses (or you press Ctrl-C), it writes three files to `output/logs/`:
   - `gate_brief_<TIMESTAMP>.txt` — structured gate-review brief (MODE, BLOCKERS, OPERATIONAL EVIDENCE, PRODUCTION DECISION)
   - `gate_evidence_<TIMESTAMP>.json` — machine-readable evidence snapshot
   - `gate_raw_<TIMESTAMP>.log` — full raw log for deep analysis
3. Run the pipeline-proof acceptance check:
   ```bash
   make gate-validate
   # or directly:
   scripts/validate_pipeline_proof.sh
   scripts/validate_pipeline_proof.sh output/logs/gate_evidence_TIMESTAMP.json
   ```
   - **PASS** (exit 0): `PRODUCTION_DECISION: SHADOW_READY` — at least one full L0→L10 trace, zero duplicate executions, zero WSOL-as-token emissions.
   - **FAIL** (exit 1): `PRODUCTION_DECISION: NOT_READY` plus a single-line reason (e.g. `traces_completed=0`).
4. Open a new Copilot chat and paste:
   > _"Review this using the production-gate-reviewer skill:"_ followed by the brief content.
5. Copilot confirms or overrides the auto-detected MODE, BLOCKER list, and PRODUCTION DECISION.

**What the script computes automatically:**

| Item                        | How it works                                                                                                                                             |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Mode auto-detection         | `PIPELINE_PROOF` (0 completed traces) → `SHADOW_TRADING` (<500 closed) → `MICRO_CAPITAL` (≥500) → `LIVE_MONITORING` (kill-switch/over-exposure detected) |
| BLOCKER detection           | Dead pipeline stages, duplicate `event_id`s, PANIC/FATAL, kill switch, over-exposure — max 3 per review                                                  |
| SAFE_TO_IGNORE              | WARN counts, cold-start learning, transient RPC errors — auto-classified non-blockers                                                                    |
| Operational Evidence        | `traces_completed`, `executions`, `positions_closed`, `learning_records`, `avg_latency`, `avg_slippage`                                                  |
| Production Confidence Model | 5 dimensions scored 0–100: `pipeline_stability`, `execution_reliability`, `determinism_integrity`, `capital_safety`, `operational_consistency`           |
| Production Decision         | `NOT_READY` → `PIPELINE_PROOF_READY` → `SHADOW_READY` → `MICRO_CAPITAL_READY` → `LIMITED_PRODUCTION_READY`                                               |
| Throughput metrics        | `wsol_token_address_emitted`, `ingestion_valid_token_ratio`, probe backlog ratio, `dq_pass_or_risky_pass`, `shadow_observer_failed`, per-program heartbeat finals |
| Throughput verdict        | `THROUGHPUT_VERDICT: CODE_DEFECT` \| `GUARDRAILS_ACTIVE` \| `MARKET_QUIET` \| `HEALTHY` — distinguishes code defects from guardrail-dominated feeds and quiet markets |
| Pipeline-proof acceptance | `scripts/validate_pipeline_proof.sh` — binary PASS/FAIL for advancing past PIPELINE_PROOF (`make gate-validate` / `make gate-proof`)                    |

> **Difference from `make log-collect`:** `log-collect` uses the `log-reviewer` skill (health scoring, PRS dimensions, stub detection). `gate-collect` uses the `production-gate-reviewer` skill (operational progression, capital safety gate, BLOCKER/SAFE_TO_IGNORE classification, and production decision). `gate-validate` is the scripted exit gate for PIPELINE_PROOF — run it after every gate session before starting extended shadow trading.

### Mock pipeline proof (no Helius)

Live Helius pump.fun traffic almost always hits the mandatory L1 `serial_launcher` reject, so `make gate-proof` can sit at `traces_completed=0` forever even when L2–L10 code is fine. Use the mock harness to prove the full pipeline first:

```bash
make gate-proof-mock
# or:
scripts/run_pipeline_proof_mock.sh offline
```

This analyzes `tests/fixtures/gate_pipeline_proof_pass.log` (synthetic L0→L10 JSON log with `learning_record_emitted` + `trace_id`) and runs `validate_pipeline_proof.sh`. No Docker, database, or RPC required. Expect `PRODUCTION_DECISION: SHADOW_READY`.

To exercise the **real workers** with a known-good injected token (still no Helius):

```bash
make docker-up                    # stack running
export DATABASE_URL=postgres://...  # or SNIPER_DB_* vars
make gate-proof-inject            # default mock token
# optional custom token:
scripts/run_pipeline_proof_mock.sh live --token YourTokenAddress...
```

Injection uses `scripts/inject_test_token.py` — pre-approved quality flags + `market_data_enriched` row so L1 passes and L2–L10 run in shadow mode.

### Battle-tested certification (11 scenarios)

Full offline scenario matrix — production thresholds, mock inputs only, DQ guardrails never relaxed:

```bash
make battle-test
# docs: docs/analysis/battle-tested-certification.md
```

Expect `BATTLE_TEST: 11/11 scenarios passed` and `BATTLE_TEST_CERTIFICATION: READY`. AI agents may cite `docs/analysis/battle-tested-certification.md` as proof the pipeline mechanics and capital-defense paths are regression-tested.

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

The system runs a **10-layer sequential pipeline** per market instance (e.g. `eth-uniswap-v2`).

Layer 0.5 (Rescan Worker) is an **optional/auxiliary stage** for time-banded re-emission of missed tokens and is disabled by default (see `config/pipeline.yaml`).

```
DETECT → FILTER → SCORE → SELECT → EXECUTE → EXIT → EVALUATE → ADJUST
   ↓        ↓        ↓       ↓        ↓        ↓        ↓        ↓
MarketData  DataQuality  FeatureDTO  EdgeDTO  Prob/Slip/Lat  ValidatedEdge  Selection  Allocation  Execution  PositionState  Learning
   DTO         DTO         DTO         DTO         DTOs         DTO             DTO        DTO      ResultDTO     DTO         Record
```

| Layer | Name                             | Responsibility                                                    |
| ----- | -------------------------------- | ----------------------------------------------------------------- |
| 0     | Detection & Ingestion            | Subscribe to DEX events, emit `MarketDataDTO`                     |
| 0.5   | Rescan Worker (optional)         | Re-emit temporally-missed tokens by age band; disabled by default |
| 1     | Data Quality Engine              | Reject rugs, honeypots, wash trades, fake liquidity               |
| 2     | Feature Extraction               | Normalize to `FeatureDTO` + `FeatureConfidence`                   |
| 3     | Signal & Edge Discovery          | `NEW_LAUNCH_EDGE` detection, adaptive momentum threshold          |
| 4     | Probability / Slippage / Latency | P(success), slippage impact, latency decay models                 |
| 5     | Edge Validation                  | EV gate, adaptive thresholds, mode-gated filters                  |
| 6     | Selection Engine                 | Top-K greedy + diversity + exploration band                       |
| 7     | Capital Engine                   | Size ∝ Score × P × Confidence, cohort multipliers                 |
| 8     | Execution Engine                 | Wallet sharding, prebuilt calldata, bounded parallelism           |
| 9     | Position Engine                  | TP1/TP2/SL/TIME exits, adaptive per cohort                        |
| 10    | Learning Engine                  | FP/FN analysis, cohort updates, bounded adaptive learning         |

### Layer 1: Mandatory Structural Hard-Rejects

Layer 1 (Data Quality Engine) enforces three **mandatory rejection criteria** that cannot be bypassed by any mode (STRICT / BALANCED / EXPLORATION) or profit condition. All three are **fail-closed**: if the underlying probe fails to run, the token is rejected.

| Criterion                                       | Reject Reason (probe ran) | Reject Reason (probe failed) | Config Flag                                                             |
| ----------------------------------------------- | ------------------------- | ---------------------------- | ----------------------------------------------------------------------- |
| **No real social profile / website**            | `no_social_links`         | `unknown_social_links`       | `reject_no_social_links: true`, `reject_unknown_social_links: true`     |
| **Excessive total supply** (>1B)                | `high_total_supply`       | `unknown_total_supply`       | `max_total_supply: 1000000000`, `reject_unknown_total_supply: true`     |
| **Serial launcher developer** (≥1 prior launch) | `serial_launcher`         | `unknown_creator_count`      | `max_creator_prev_token_count: 1`, `reject_unknown_creator_count: true` |

**Social link validation rules:**

- Twitter/X: must be a profile URL — tweet links (`/status/`), `t.co` shortlinks, and retweet redirects are rejected.
- Telegram: any `t.me/` channel link is accepted.
- Website: real project domains only — pump.fun pages, DEX scanner pages (dexscreener.com, birdeye.so, solscan.io, raydium.io, jup.ag, geckoterminal.com, axiom.trade, etc.) are **not** accepted as project websites.

Probe failure always means rejection — a `*Known=false` field with the matching `reject_unknown_*: true` flag triggers an immediate structural reject before any detector runs.

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

See [`docs/reference/architecture.md`](docs/reference/architecture.md) for the full design and invariants.

### AI Enrichment Flow (Cross-Cutting Layers 0/1/3/10)

AI narrative enrichment runs **autonomously and fail-open** across four pipeline layers via `internal/ai/CopilotClient`. It never blocks the pipeline — any error produces `NarrativeKnown=false` and processing continues using degraded signals only.

**Auth:** `GITHUB_COPILOT_TOKEN` env var only. HTTPS-only endpoint (`https://api.githubcopilot.com/chat/completions`). 4 KiB response cap. 1-shot per token (one retry on 429/5xx). Model is configurable — see model priority table below.

| Layer            | Component                  | What AI does                                                                                                    | Output fields                                                                                                                    |
| ---------------- | -------------------------- | --------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| **L0 / L0.5**    | `AINarrativeProbe`         | Scores token narrative quality: trending alignment, scam signals, copy-paste detection, impersonation detection | `NarrativeScore` (0–10), `ScamProbabilityScore` (0–10), `IsCopyPasteDesc`, `IsImpersonation`, `NarrativeType`, `NarrativeReason` |
| **L1 DQ**        | `DataQualityWorker`        | Applies risk bumps when `NarrativeKnown=true`                                                                   | `+0.30` to `RiskScore` if `IsCopyPasteDesc`; `+0.20` if `IsImpersonation`                                                        |
| **L3 Edge**      | `applyNarrativeMultiplier` | Adjusts edge confidence from narrative quality score                                                            | `±10%` to `EdgeConfidence` based on `NarrativeScore`                                                                             |
| **L10 Learning** | `LossExplainer` probe      | Classifies losing trades with AI-generated category + natural-language reason                                   | `LearningRecordDTO.AIExplanation`, `LearningRecordDTO.AICategory`                                                                |

**Model priority (highest → lowest):**

1. `AI_ENRICH_MODEL` env var
2. `ai_enrichment.model` in `config/pipeline.yaml`
3. Built-in default: `gpt-5.4-mini`

**Log keys to monitor AI enrichment:**

| `msg` field                  | When emitted                                     | Key fields                                                                                   |
| ---------------------------- | ------------------------------------------------ | -------------------------------------------------------------------------------------------- |
| `ai_narrative_probe_skipped` | `NarrativeKnown` already true, or probe disabled | —                                                                                            |
| `ai_narrative_scored`        | Successful enrichment                            | `narrative_score`, `scam_probability`, `is_copy_paste`, `is_impersonation`, `narrative_type` |
| `ai_narrative_failed`        | API error (fail-open, pipeline continues)        | `error`, `duration_ms`                                                                       |
| `ai_narrative_dq_bump`       | L1 applied narrative risk bump                   | `copy_paste`, `impersonation`, `narrative_risk_bump`, `narrative_score`                      |

---

## Solana Launchpad Coverage (P4)

Four launchpad DEXes are tracked on Solana via on-chain instruction decoding (layer 0):

| Launchpad      | Program ID (prefix) | Discriminator (first 8 bytes)                                     | Pool accounts layout                                   |
| -------------- | ------------------- | ----------------------------------------------------------------- | ------------------------------------------------------ |
| PumpFun AMM    | `pAMMBay6…`         | `[233,146,209,142,207,104,64,188]` (SHA256 `global:create_pool`)  | Pool=0, Creator=2, BaseMint=3, QuoteMint=4             |
| Raydium CLMM   | `CAMMCzo5…`         | `[233,146,209,142,207,104,64,188]` (same; dispatch by programID)  | PoolCreator=0, PoolState=2, TokenMint0=3, TokenMint1=4 |
| Orca Whirlpool | `whirLbMiic…`       | `[95,180,10,172,84,174,232,40]` (SHA256 `global:initialize_pool`) | TokenMintA=1, TokenMintB=2, Funder=3, Pool=4           |
| Meteora DLMM   | `LBUZKhRx…`         | `[110,106,20,253,63,145,232,63]`                                  | LbPair=0, MintX=1, MintY=3, Funder=2                   |

All new launchpad families emit `MarketDataDTO` (same contract as Raydium V4/PumpFun BC) and are routed through the same 10-layer pipeline with no code changes downstream.

Config: `config/chains.yaml` → `solana.programs[]` (4 new entries). All families are active when Solana is enabled; no per-family flag needed.

---

## Private Mempool & Bundle Submission (P5)

Three components provide sub-100ms execution on Solana (all boot `shadow_mode: true`):

### Yellowstone gRPC Transport

- Real-time block stream vs. ~200ms WebSocket+RPC latency.
- Modes: `rpc` (default), `grpc`, `hybrid` (gRPC primary, RPC fallback).
- Config: `config/chains.yaml` → `solana.transport`.
- Auth token: `SOLANA_GRPC_TOKEN` env var **only** — never stored in YAML.
- Endpoint: `SOLANA_GRPC_ENDPOINT` env var (e.g. `my-node.quiknode.pro:10000`).

### ZeroSlot Priority RPC

- Routes transaction submissions through ZeroSlot's private mempool (pre-landed).
- Activation: set `SOLANA_ZEROSLOT_HTTP` env var to your ZeroSlot endpoint.
- Config: `config/chains.yaml` → `solana.rpc.zeroslot`.

### Jito Bundle Submission

- Submits atomic bundles via Jito's Block Engine (MEV-friendly, bribe-based inclusion).
- Config: `config/execution.yaml` → `solana.jito`.
- Env vars: `JITO_BUNDLE_URL` (must be HTTPS in production), `JITO_TIP_ACCOUNT`.
- Security: plain HTTP URLs are rejected unless the host is `localhost`/`127.x` (test only).
- Shadow mode: `shadow_mode: true` (default) — logs bundle content without submitting.

---

## Data Quality Providers (P8 + earlier)

The Data Quality Engine (Layer 1) aggregates signals from multiple providers. Every provider failure degrades gracefully (`Degraded: true`) without blocking the pipeline.

| Provider      | Phase | Signal                                          | Env Vars Required                      | Shadow Default   |
| ------------- | ----- | ----------------------------------------------- | -------------------------------------- | ---------------- |
| RugCheck      | P9    | On-chain rug score (bonding curve, freeze auth) | —                                      | `enabled: false` |
| Social Gate   | P2    | Twitter/X follower legitimacy gate              | `TWITTER_BEARER_TOKEN`                 | `enabled: false` |
| BirdEye Intel | P3    | Price velocity, holder count, creator history   | `BIRDEYE_API_KEY`                      | `enabled: false` |
| Copy Trade    | P8    | Alpha wallet activity match on DEXScreener API  | `COPY_TRADE_WALLETS` (comma-separated) | `enabled: false` |
| AI Narrative  | AI    | Copilot API narrative score + copy-paste rug    | `GITHUB_COPILOT_TOKEN`                 | `enabled: false` |

### Copy Trade Provider (P8)

Detects when known "alpha wallets" are active in the token being evaluated — a strong execution timing signal.

- `COPY_TRADE_WALLETS`: comma-separated Solana/EVM wallet addresses to watch.
- Chain allowlist: `ethereum`/`eth`, `bsc`/`bnb`, `solana`/`sol`, `base` (unknown chains → `Degraded: true`).
- Score: `0.0` (strong positive) on alpha match, `0.5` (neutral) otherwise.
- Response body capped at 128 KiB; timeout 280ms; no real traffic in shadow mode.

---

## AI Enrichment (Optional)

GitHub Copilot API-powered narrative scoring and loss attribution layered across the pipeline. All calls are:

- **1-shot autonomous** — `Complete()` fires one HTTP request and returns immediately. No human approval gate, no interaction loop.
- **Fail-open** — any error leaves `NarrativeKnown=false` / `AIExplanationKnown=false`; the pipeline is never blocked.
- **Configurable model** — set `AI_ENRICH_MODEL` env var (highest priority), or `ai_enrichment.model` in `config/pipeline.yaml` (default: `gpt-5.4-mini`).

### What it adds

| Layer | Component             | Effect                                                                                                           |
| ----- | --------------------- | ---------------------------------------------------------------------------------------------------------------- |
| L0/1  | `AINarrativeProbe`    | Scores token narrative quality 0–10; detects copy-paste descriptions                                             |
| L1 DQ | Copy-paste detector   | Adds +0.30 risk score for boilerplate descriptions, +0.20 for impersonation                                      |
| L3    | `NarrativeMultiplier` | Soft ±10% adjustment to `EdgeConfidence` based on `NarrativeScore`                                               |
| L10   | `LossExplainer`       | AI loss category (`timing`/`scam`/`momentum_fade`/etc.) + natural-language reason written to `LearningRecordDTO` |

### Enable

```bash
# 1. Set the Copilot API token
export GITHUB_COPILOT_TOKEN=<your-token>

# 2. Optionally override the model (default: gpt-5.4-mini)
export AI_ENRICH_MODEL=gpt-5.4-mini   # or any model supported by the Copilot API

# 3. Enable in config/pipeline.yaml
#    ai_enrichment.enabled: true
#    ai_enrichment.narrative_probe.enabled: true
#    ai_enrichment.loss_explainer.enabled: true
```

### Security invariants

- `GITHUB_COPILOT_TOKEN` read via `os.Getenv` only — **never in YAML, never logged**.
- `AI_ENRICH_MODEL` env var also read via `os.Getenv` — same pattern as `MODEL_HEAVY` in `scripts/run_parallel.sh`.
- Endpoint must be HTTPS; non-HTTPS is rejected at construction.
- Response body bounded at `max_response_bytes` (default 4 KiB).
- Prompts truncated to `max_prompt_chars` (default 600) before sending.

---

## Environment Variables

| Variable                        | Required When                      | Purpose                                             |
| ------------------------------- | ---------------------------------- | --------------------------------------------------- |
| `DATABASE_URL`                  | Always                             | PostgreSQL connection string                        |
| `SNIPER_TELEGRAM_BOT_TOKEN`     | Telegram enabled                   | Telegram bot token for operator notifications       |
| `SNIPER_TELEGRAM_CHAT_ID`       | Telegram enabled                   | Chat ID to send messages to                         |
| `SNIPER_TELEGRAM_ALLOWED_USERS` | Destructive Telegram commands      | Comma-separated allowed Telegram user IDs           |
| `SOLANA_RPC_HTTP`               | Solana markets active              | Solana RPC HTTP endpoint                            |
| `SOLANA_RPC_WSS`                | Solana markets active              | Solana RPC WebSocket endpoint                       |
| `SOLANA_GRPC_ENDPOINT`          | `transport.mode: grpc` or `hybrid` | Yellowstone gRPC endpoint (`host:port`)             |
| `SOLANA_GRPC_TOKEN`             | `transport.mode: grpc` or `hybrid` | gRPC auth token — **never put in YAML**             |
| `SOLANA_ZEROSLOT_HTTP`          | ZeroSlot private mempool           | ZeroSlot HTTP endpoint for pre-landed submissions   |
| `JITO_BUNDLE_URL`               | `jito.enabled: true`               | Jito Block Engine URL (must be HTTPS in production) |
| `JITO_TIP_ACCOUNT`              | `jito.enabled: true`               | Jito tip account public key                         |
| `COPY_TRADE_WALLETS`            | Copy trade provider active         | Comma-separated alpha wallet addresses              |
| `BIRDEYE_API_KEY`               | BirdEye provider active            | BirdEye API key for price/holder data               |
| `TWITTER_BEARER_TOKEN`          | Social gate provider active        | Twitter/X Bearer Token for social gate              |
| `GITHUB_COPILOT_TOKEN`          | `ai_enrichment.enabled: true`      | GitHub Copilot API token — **never put in YAML**    |
| `AI_ENRICH_MODEL`               | AI enrichment active (optional)    | Override model (default: `gpt-5.4-mini`)            |

All API keys and secrets are read via `os.Getenv()` at startup only — never stored in YAML, never logged, never passed across module boundaries.

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
| `/executions`        | Last 20 tokens that reached execution stage (success + failed) with full CA address     |
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
│   ├── gate_review_collect.sh      # Production gate evidence + brief collector
│   ├── validate_pipeline_proof.sh    # PIPELINE_PROOF acceptance harness (Task 18)
│   ├── validate_phase2_acceptance.sh # Phase 2 full §1.1 acceptance gate (Task 19)
│   └── run_parallel.sh             # Parallel development orchestrator (3-mode)
├── docs/                       # Documentation — see docs/README.md
│   ├── README.md               # Index (only file at docs root besides REDIRECTS.md)
│   ├── reference/              # Canonical specs (architecture, DTOs, DB, orchestrator)
│   ├── guides/                   # STARTER_GUIDE, PARALLEL_DEV, AGENTS_AND_SKILLS
│   ├── ops/                      # PROGRESS_REPORT
│   ├── plans/                    # Implementation plans
│   ├── analysis/                 # Dated investigations
│   ├── specs/                    # Pre-plan design specs
│   ├── archive/                  # Superseded chunks
│   └── mockups/                  # UI mockups
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

| Phase | Name                      | Group | Status  | Description                                                                                                                                                                                                    |
| ----- | ------------------------- | ----- | ------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0     | core-infrastructure       | A     | ✅ Done | DB, event bus, adapter, orchestrator, migrations                                                                                                                                                               |
| 1     | dex-ingestion             | A     | ✅ Done | DEX scanner, RPC pool, `MarketDataDTO` → event bus                                                                                                                                                             |
| 2     | first-trade-pipeline      | A     | ✅ Done | End-to-end: DQ → Feature → Edge → Capital → Execute → Position                                                                                                                                                 |
| 3     | evaluation-correctness    | B     | ✅ Done | Learning records, strategy versioning, replay engine                                                                                                                                                           |
| 4     | signal-quality            | B     | ✅ Done | Full probability models, feature stability, anti-manipulation                                                                                                                                                  |
| 5     | learning-engine           | B     | ✅ Done | Adaptive learning, strategy decay detection, auto-disable                                                                                                                                                      |
| 6     | production-hardening      | C     | ✅ Done | Observability, drawdown protection, wallet sharding, Telegram                                                                                                                                                  |
| 7     | solana-market             | C     | ✅ Done | Solana Raydium/PumpFun ingestion + execution, hybrid transport                                                                                                                                                 |
| 8     | production-hardening-r2   | C     | ✅ Done | Reconciliation, partition leasing, DLQ, crash recovery, reorg guard                                                                                                                                            |
| 9     | profitability-restoration | D     | ✅ Done | Real scam detection, live features, Kelly sizing, price-feed monitor                                                                                                                                           |
| 10    | reference-repo-r1         | D     | ✅ Done | Trailing stop, consecutive-pass gate, bonding curve filter; **rescan worker** (Layer 0.5) — time-banded re-emission of temporally-missed tokens                                                                |
| 10.5  | observability-r1          | D     | ✅ Done | Cumulative pipeline funnel fix (`/pipeline`), new Telegram commands: `/rescan`, `/dq`, `/dlq`                                                                                                                  |
| 11    | reference-repo-r2         | D     | ✅ Done | Creator hygiene, holder concentration, social links, congestion slippage, per-creator dedup, sim-diff                                                                                                          |
| P4    | multi-launchpad-ingestion | D     | ✅ Done | PumpFun AMM + Raydium CLMM + Orca Whirlpool + Meteora DLMM on-chain decoder (17 tests)                                                                                                                         |
| P5    | yellowstone-jito-zeroslot | D     | ✅ Done | Yellowstone gRPC transport, Jito bundle submission (shadow), ZeroSlot priority RPC (8 tests)                                                                                                                   |
| P8    | copy-trade-amplifier      | D     | ✅ Done | Alpha wallet copy-trade DQ provider via DEXScreener API (8 tests), MED-01/HIGH-01/MED-02 security hardening                                                                                                    |
| AI    | ai-enrichment             | D     | ✅ Done | GitHub Copilot API narrative probe (L0/1), copy-paste rug detector (L1 DQ), NarrativeScore edge multiplier (L3), LossExplainer (L10); model configurable via `AI_ENRICH_MODEL` env var; default `gpt-5.4-mini` |

**Group rules:** Groups A → B → C → D are sequential. Phases within the same group may run in parallel.

See [`docs/reference/implementation_roadmap.md`](docs/reference/implementation_roadmap.md) for exact file paths, function signatures, and exit criteria per phase.

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

See [`docs/guides/PARALLEL_DEV.md`](docs/guides/PARALLEL_DEV.md) for the full operator guide, model routing, phase grouping, and parallel safety invariants.

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

| Parameter                        | Default                                                                                                                                                            | Description                                                                 |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------- |
| `enabled`                        | `true`                                                                                                                                                             | Enabled by default; set `false` to disable                                  |
| `interval_seconds`               | `60`                                                                                                                                                               | Poll cadence — lower = faster second-chance detection                       |
| `max_per_band_per_tick`          | `100`                                                                                                                                                              | Max tokens re-emitted per age band per tick                                 |
| `skip_open_positions`            | `true`                                                                                                                                                             | Never rescan tokens already in an open position                             |
| Bands                            | **14 bands**: 15m/30m/45m/1h/1.5h/2h/3h/4h/6h/8h (Phase 1 — Goal A organic momentum, 0–8h) + 12h/24h/36h/48h (Phase 2 — Goals B+C reversal + CEX catalyst, 12–48h) | Age windows; each band has `min_age_seconds`, `max_age_seconds`, `priority` |
| `eligibility.max_honeypot_score` | `0.5`                                                                                                                                                              | Tokens above this score are excluded from rescan                            |
| `eligibility.max_rug_score`      | `0.65`                                                                                                                                                             | Tokens above this score are excluded from rescan                            |
| `eligibility.max_buy_tax_bps`    | `3000`                                                                                                                                                             | Tokens above this tax are excluded from rescan                              |

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

#### AI Enrichment (`ai_enrichment:`)

| Parameter                                             | Default        | Description                                                                         |
| ----------------------------------------------------- | -------------- | ----------------------------------------------------------------------------------- |
| `ai_enrichment.enabled`                               | `false`        | Master switch; requires `GITHUB_COPILOT_TOKEN`                                      |
| `ai_enrichment.model`                                 | `gpt-5.4-mini` | Model name; overridden by `AI_ENRICH_MODEL` env var                                 |
| `ai_enrichment.timeout_ms`                            | `8000`         | Per-call HTTP timeout                                                               |
| `ai_enrichment.max_retries`                           | `1`            | Retries on HTTP 429/5xx; all calls fail-open                                        |
| `ai_enrichment.rate_limit_per_min`                    | `8`            | Token-bucket rate limit (non-blocking; excess requests fail-open)                   |
| `ai_enrichment.narrative_probe.enabled`               | `false`        | Enable `AINarrativeProbe` (Layer 0/1); scores token name/description                |
| `ai_enrichment.narrative_probe.min_narrative_score`   | `3.0`          | Minimum score to treat narrative as non-negative                                    |
| `ai_enrichment.narrative_probe.copy_paste_rug_reject` | `false`        | Hard-reject (not soft) on copy-paste detection when `true`                          |
| `ai_enrichment.loss_explainer.enabled`                | `false`        | Enable `LossExplainer` (Layer 10); adds AI category + reason to `LearningRecordDTO` |

See [`docs/reference/architecture.md § 7`](docs/reference/architecture.md) for operational mode configs (`STRICT` / `BALANCED` / `EXPLORATION` / `VERY_EXPLORATION`).

---

## Documentation

**Start at [`docs/README.md`](docs/README.md)** — seven folders, no loose spec files at root. Old bookmarks: [`docs/REDIRECTS.md`](docs/REDIRECTS.md).

| Folder | Purpose |
| ------ | ------- |
| [`docs/reference/`](docs/reference/) | Canonical specs (architecture, DTOs, DB, orchestrator, roadmap) |
| [`docs/guides/`](docs/guides/) | STARTER_GUIDE, PARALLEL_DEV, AGENTS_AND_SKILLS |
| [`docs/ops/`](docs/ops/) | PROGRESS_REPORT |
| [`docs/plans/`](docs/plans/) | Executable implementation plans |
| [`docs/analysis/`](docs/analysis/) | Dated investigations and certifications |
