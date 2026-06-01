# PLAN.md — Production Gate Hardening (Credit Burn + Creator Attribution + Mode-Aware DQ + Market-Cap Filters + Shadow Trading)

> **Version:** 1.1
> **Date:** 2026-05-29 (v1.0); 2026-05-31 (v1.1 — Phase 6 gate-review remediation, Tasks 22–28 added)
> **Author:** crypto-sniping-bot core
> **Status:** Ready for Implementation
> **Source of Truth:** [docs/PRODUCTION_GATE_ANALYSIS.md](../PRODUCTION_GATE_ANALYSIS.md) (sections 1–10 + Appendices A/B/C)
> **Pipeline Layers Affected:** L0 (ingestion), L0.5 (rescan — read-only), L1 (data quality), L7 (capital), L9 (position), L10 (learning), Config, Contracts, DB Migration, Workers (telegram dispatcher), Platform
> **Profit Factors Affected:** DataQuality (primary — fixes 100% reject rate), Edge (secondary — first tokens reach L2+), Execution (none — preserved), Capital (interprets new `RISKY_PASS`), AdaptationQuality (cleaner reject statistics → better learning signal). Operational sustainability (credit burn fix) gates ALL six factors.

---

## 1. Goal

Convert the bot from its current **Pipeline Proof** stage — in which Layer 1 rejects
100% of tokens and Layers 2–10 are starved of input — to a state where it can
sustainably operate within the Helius Developer plan (10M credits/month), pass real
graduation tokens through every layer, run in **Shadow Mode** for 2–4 weeks, and then
progress to micro-capital live trading. This plan covers **every actionable item** in
[docs/PRODUCTION_GATE_ANALYSIS.md](../PRODUCTION_GATE_ANALYSIS.md) (Sections 1–10 plus
Appendices A/B/C), grouped into five sequential phases.

**Sub-goals (in order of execution):**

- **Phase 1 — Credit Burn Reduction** (P0, Sections 4 + 5 + Section 8 Change 1):
  fix the ~2M credits/day burn that empties the plan in ~5 days. Disable raw pump.fun
  streaming and replace Raydium V4 `logsSubscribe` with `transactionSubscribe` +
  account-filter (Option 3, in-process, no new HTTP infra). Fix wrong cost comments.
- **Phase 2 — Real Creator Attribution** (P1-A, Section 7): stop treating the Pump.fun
  factory program as the creator identity; track wallet-level launches; aggregate into
  `creator_profiles`; expose OKX-style dev-token stats; preserve fail-closed behavior.
- **Phase 3 — Mode-Aware Serial Launcher Threshold** (Section 9): hard-gate in
  STRICT/BALANCED (unchanged behavior), conditional `RISKY_PASS` in
  EXPLORATION/VERY_EXPLORATION when quality gates pass, `SKIP` (silent drop, not a
  reject) when they fail. Introduces the `SKIP` decision and the
  `serial_launcher_monitored` flag consumed by L7/L9.
- **Phase 4 — Market Cap & Volume Filters** (Section 10): expand the existing
  DEXScreener parser to capture market cap and volume (already in the response,
  currently discarded), add additive `MarketDataDTO` fields, add optional structural
  rejects (`market_cap_too_low|too_high`, `volume_too_low`).
- **Phase 5 — Pipeline Validation & Shadow Trading** (P1-C + P2-A + P2-B, Section 8
  Change 6): inject a known-good token bypassing L1 to validate L2–L10 end-to-end,
  enable `execution.mode: "shadow"`, monitor paper P&L for 2 weeks before any live
  capital.

**Why:** the bot is correctly built — its filters are right, its math is sound — but it
is fishing in the wrong water (raw pump.fun) and burning credits on data it discards
(Raydium V4 streaming). Fix market selection and infra cost, restore meaningful
serial-launcher semantics via wallet-level identity, then prove L2–L10 with a real token.

**Profit factor mapping:**

| Factor             | Impact                                                                              |
| ------------------ | ----------------------------------------------------------------------------------- |
| DataQuality        | Phase 2 (right identity) + Phase 3 (mode-aware) + Phase 4 (early structural reject) |
| Edge               | Phase 5 unblocks L3 — never run on live data before                                 |
| Probability        | Unchanged in this plan; Phase 5 validates calibration                               |
| Execution          | Unchanged; Phase 5 (shadow) measures pre-live execution quality                     |
| Capital            | Consumes new `RISKY_PASS` from Phase 3 (smaller allocation)                         |
| AdaptationQuality  | Cleaner reject stream (SKIP not REJECT) → better Layer-10 learning signal           |
| **Sustainability** | Phase 1 prevents credit-plan exhaustion; without it, all six → 0                    |

---

## 2. Architecture Impact

### Affected Pipeline Layers & Subsystems

| Layer / Subsystem     | Path                                                    | Change type                                                                                 |
| --------------------- | ------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| L0 — Solana ingestion | `internal/modules/ingestion_solana/`                    | Subscription strategy switch (logsSubscribe → transactionSubscribe); creator-identity guard |
| L0 — chains config    | `internal/app/config/chains.go` (or equivalent loader)  | New optional `disabled` field on program entries                                            |
| Config — chains.yaml  | `config/chains.yaml`                                    | Disable raw pump.fun; comment fixes; subscription method update                             |
| L1 — Data Quality     | `internal/modules/data_quality/`                        | Mode-aware serial launcher; SKIP outcome; market-cap/volume structural reject               |
| L1 — Probes           | `internal/modules/probes/` (DEXScreener probe)          | Populate new MarketDataDTO fields                                                           |
| Price feed            | `internal/rpc/price_fetcher.go`                         | Expanded DEXScreener parser struct                                                          |
| L7 — Capital          | `internal/modules/capital/`                             | Honour `RISKY_PASS` (smaller allocation, existing pattern)                                  |
| L9 — Position         | `internal/modules/position/`                            | Honour `serial_launcher_monitored` flag (tighter trailing/TP1, kill-switch priority)        |
| L10 — Learning        | `internal/modules/learning/`                            | Exclude `SKIP` outcomes from reject-rate signal                                             |
| Config structs        | `internal/app/config/data_quality_runtime_config.go`    | New mode-profile + threshold fields                                                         |
| YAML config           | `config/data_quality.yaml`                              | Per-mode profiles + commented-out market-cap/volume thresholds                              |
| Contracts             | `contracts/data_quality.go`, `contracts/market_data.go` | Additive: new `SKIP` decision value, new market-cap/volume fields                           |
| Database              | `database/migrations/`                                  | New `creator_profiles` table migration (append-only)                                        |
| Workers               | `internal/workers/`                                     | New creator-profile aggregator worker; new diagnostics emitter                              |
| Telegram dispatcher   | `internal/telegram/`                                    | New operator command surfacing creator stats (event-bus only, no direct API)                |
| Tests                 | `tests/unit/`, `tests/integration/`, `tests/modules/`   | New tests for every behavioural change                                                      |
| Docs                  | `docs/PROGRESS_REPORT.md`                               | Phase Progress + Session History append (only writable doc)                                 |

### DTO Flow (before → after)

```
Before:
  MarketDataDTO (no MarketCapUsd, no Volume*) → DQ → DataQualityDTO {PASS|RISKY_PASS|REJECT}
  pump.fun token  → MarketDataDTO.CreatorAddress = factory program → serial_launcher REJECT
  serial launcher → always REJECT in every mode → rejection-rate stats polluted

After:
  MarketDataDTO {+ MarketCapUsd, + VolumeUsd5m/1h/24h} → DQ → DataQualityDTO {PASS|RISKY_PASS|REJECT|SKIP}
  pump.fun token  → ingestion guard prefers event.User → real wallet → meaningful serial check
  serial launcher in EXPLORATION + quality gates pass → RISKY_PASS + serial_launcher_monitored flag
  serial launcher in EXPLORATION + quality gates fail → SKIP (silent, not in reject stats)
  L7 sees RISKY_PASS → smaller allocation
  L9 sees serial_launcher_monitored → tighter TP1, tighter trail, kill-switch priority
  L10 ignores SKIP outcomes when computing reject-rate signal
```

### Subscription Strategy Decision (Section 5)

Two options were presented in PRODUCTION_GATE_ANALYSIS.md for Raydium V4. This plan
selects **Option 3 (transactionSubscribe + accountInclude / accountRequired filter)**:

| Aspect                      | Option 2 (Webhooks)                                         | Option 3 (transactionSubscribe) — **CHOSEN** |
| --------------------------- | ----------------------------------------------------------- | -------------------------------------------- |
| Code path changed           | Add new HTTP receiver + route                               | Modify existing WebSocket subscription only  |
| External infra required     | Public HTTPS endpoint + secrets                             | None — same Helius WS endpoint               |
| Latency                     | 100–500 ms (HTTP delivery)                                  | Sub-millisecond (same WS as today)           |
| Credit cost (pool-creation) | 1 credit / event × 500/day                                  | 2 cr / 0.1 MB × ~1–2 KB × 500/day            |
| Plan compatibility          | Any                                                         | Requires Developer plan (✅ current)         |
| Architectural footprint     | New `cmd/server.go` route + handler + webhook secret in env | Surgical change in `ingestion_solana.go`     |

Option 2 (webhooks) is documented as a fallback in §7 (deep knowledge) for ops to
adopt later if needed; this plan does not implement both.

### Key Decisions

| Decision                                                                                                                               | Rationale                                                                                                                                |
| -------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| **Choose Option 3 over Option 2** for Raydium V4                                                                                       | Keeps single in-process ingestion path; no new HTTP attack surface; same WS endpoint already authenticated                               |
| **Add `SKIP` as a new `DataQualityDTO.Decision` value** (additive, not replacing existing)                                             | Section 9 requires "silent drop, not logged as rejection" semantics distinct from REJECT/RISKY_PASS/PASS                                 |
| **Do NOT raise global `max_creator_prev_token_count` from 1 to 4** (Section 8 Change 3 superseded)                                     | Per Section 9: STRICT/BALANCED must stay at 1 (hard gate). Per-mode overrides give exploration breathing room.                           |
| **Wallet-level creator identity via ingestion guard, not retroactive DB cleanup**                                                      | `NormalizePumpFun*` already sets `CreatorAddress = event.User`; we only need to GUARD against factory-program drift, not rewrite history |
| **`creator_profiles` table with idempotent CAS updates**                                                                               | Append-only event-bus invariant preserved; rebuildable from `events` log + `execution_results` (replay-safe)                             |
| **Market-cap / volume thresholds COMMENTED OUT by default**                                                                            | Section 10 caveat: graduation tokens may exceed $20k cap immediately; tune in shadow mode before enabling                                |
| **Webhook (Option 2) is NOT implemented**                                                                                              | Avoids new HTTPS endpoint, secret management, and `cmd/server.go` route — keeps blast radius small                                       |
| **DAS probe stays disabled**                                                                                                           | Per PRODUCTION_GATE_ANALYSIS § 6 / P1-B: only enable after credit-burn fix verified; cost is 10 cr/call                                  |
| **Telegram operator commands ONLY via event bus**                                                                                      | Per copilot-instructions.md: modules MUST NOT call Telegram directly; dispatcher reads from `events`                                     |
| **Shadow mode is gated on at least one clean L1 ACCEPT + one successful L1→L10 trace**                                                 | Per Section 8 Change 6: "find issues in L2-L10 now, while there is no money at risk"                                                     |
| **DEXScreener probe HTTP body remains capped at 128 KiB**                                                                              | Existing security invariant — parser expansion adds fields, NOT bytes                                                                    |
| **Position monitoring availability gate** — Layer 9 health is consulted via existing system-state read, not a direct cross-module call | Preserves module isolation; uses `internal/modules/state_machine/` or `system_state.go` health view                                      |

---

## 3. Invariants Preserved

- [x] **Profit invariant** — `Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality` — all six factors preserved; Phase 1 protects sustainability that gates all six
- [x] **Determinism** — no randomness; all thresholds and gates are deterministic; same input + same config + same StrategyVersion = identical decision
- [x] **Idempotency** — `creator_profiles` updates use CAS; all migrations use `ON CONFLICT DO NOTHING`; subscription change does not introduce duplicate-event paths
- [x] **Module isolation** — no module imports another module's internals; capital/position consume `RISKY_PASS` and `serial_launcher_monitored` via DTO fields they already read; new aggregator worker reads via `database/adapter.*`
- [x] **No direct DB access from modules** — DQ continues to receive creator stats via injected reader interface (resolved by orchestrator wiring); new aggregator worker lives in `internal/workers/` and uses the adapter
- [x] **DTO additive-only** — new `MarketDataDTO` fields are zero-default; `SKIP` is a new enum value (existing consumers branch on PASS/RISKY_PASS/REJECT and treat unknown values as drop — verify)
- [x] **Config-driven** — every new threshold lives in `config/data_quality.yaml`; new subscription-strategy and `disabled` flag in `config/chains.yaml`; zero hardcoded values in Go
- [x] **Event bus backbone** — DQ continues to emit `data_quality_event`; new `creator_profile_updated` events follow the same append-only pattern; `SKIP` outcomes do NOT emit a `data_quality_event` (silent drop is the contract) but DO update token_lifecycle to a terminal SKIPPED state for traceability — see §7.6
- [x] **Security invariants** — HTTPS-only preserved (no new endpoints); no API keys in YAML; DEXScreener parser stays inside the existing 128 KiB `LimitReader`; no new env vars except optional `HELIUS_WEBHOOK_SECRET` (NOT added because Option 2 is not implemented)
- [x] **Layer-1 hard rejects intact** — `serial_launcher` in STRICT/BALANCED unchanged; `no_social_links`, `high_total_supply`, `unknown_*` (in STRICT/BALANCED) all preserved exactly
- [x] **Migrations append-only** — one new migration file `database/migrations/20260101000NNN_creator_profiles.sql`; no existing migrations modified
- [x] **Telegram via event bus only** — new operator command for creator stats follows the dispatcher pattern; no direct API call from modules
- [x] **Bounded HTTP bodies** — Jito 64 KiB / DEXScreener 128 KiB / Groq 4 KiB unchanged; parser expansion adds fields parsed from already-bounded bytes
- [x] **Rescan worker contract preserved** — no new event types; rescan continues to re-emit `market_data_event` with `replay:`-prefixed `EventID` when appropriate; SKIP decisions exclude tokens from future rescan eligibility (treated like REJECT for rescan purposes, see §7.5)

**Factors NOT affected by this plan:** Execution engine semantics (wallet sharding,
nonce management, prebuilt calldata, RPC fallback, fee bumping); Probability/Slippage/Latency
model internals (Layer 4); StrategyVersion creation flow; replay engine prefix isolation;
gRPC auth handling; Jito bundle URL validation.

---

## 4. Implementation Tasks

### Dependency Graph

```
                                  Phase 1 — Credit Burn
                                  ────────────────────────
Task 1  ✅ COMPLETED — (Comment fixes: chains.yaml + DAS docs)
   │
   ▼
Task 2  ✅ COMPLETED — (Chains config struct: add `disabled` flag)        ─ precondition for #3
   │
   ▼
Task 3  (Disable raw pump.fun + switch Raydium V4 to transactionSubscribe)
   │
   ▼
Task 4  (Runbook: verify pump.fun-AMM event flow — SQL only, no code)
   │
   ▼                              Phase 2 — Creator Attribution
                                  ────────────────────────────
Task 5  (Migration: creator_profiles table)
   │
   ▼
Task 6  (Contracts additive: SKIP decision + MarketCap/Volume MarketDataDTO fields)
   │
   ▼
Task 7  (Ingestion guard: reject factory-program creator identity)
   │
   ▼
Task 8  ✅ COMPLETED — (Creator profile aggregator worker)
   │
   ▼
Task 9  ✅ COMPLETED — (DQ uses creator_profiles via injected reader; preserves fail-closed)
   │
   ▼
Task 10 ✅ COMPLETED — (/devstats emits creator_stats_request → RunCreatorStatsResponder emits telegram_event; dispatcher pattern preserved; 8 tests pass)
   │
   ▼                              Phase 3 — Mode-Aware Serial Launcher
                                  ────────────────────────────────────
Task 11 ✅ COMPLETED — (Config struct: add per-mode serial-launcher fields; 4 new fields on DataQualityModeProfile; 6 tests pass)
   │
   ▼
Task 12 ✅ COMPLETED — (data_quality.yaml: per-mode profiles; 4 serial-launcher fields added to all 4 mode blocks; STRICT/BALANCED sentinel=0; EXPLORATION=5/true/0.40/50; VERY_EXPLORATION=10/true/0.45/25; 7 tests pass, global threshold=1 unchanged)
   │
   ▼
Task 13 ✅ COMPLETED — (ProcessForMode: mode-aware serial launcher; buildSkipResult helper; DQ_SKIPPED lifecycle state; run_data_quality SKIP handling; 15 new tests pass; STRICT/BALANCED unchanged; build/vet clean)
   │
   ▼
Task 14 ✅ COMPLETED — (canonicalProfile updated with 4 serial-launcher fields; STRICT/BALANCED sentinel=0 preserved; EXPLORATION=5/0.40/50; VERY_EXPLORATION=10/0.45/25; 5 new tests pass; build/vet clean)
   │
   ▼                              Phase 4 — Market Cap / Volume Filters
                                  ────────────────────────────────────
Task 15 ✅ COMPLETED — (3 new float64 fields on DataQualityDetectorThresholds: MinMarketCapUsd/MaxMarketCapUsd/MinVolumeUsd1h; 0=filter-disabled sentinel; 5 new tests pass; build/vet/test clean)
   │
   ▼
Task 16 ✅ COMPLETED — (3 commented-out YAML entries: min_market_cap_usd/max_market_cap_usd/min_volume_usd_1h; all zero until operator uncomments; 0=filter-disabled sentinel; YAML parses clean; 3 new tests pass; build/vet clean)
   │
   ▼
Task 17 ✅ COMPLETED — (price_fetcher.go struct expanded + parseDEXScreenerMarketData added; dexscreener_market_data.go probe created; 128 KiB LimitReader preserved; 11 new tests green; build/vet clean)
   │
   ▼
Task 18 ✅ COMPLETED — (DQ structural rejects market_cap_too_low|too_high|volume_too_low added to ProcessForMode with dual > 0 guard; 8 new tests green; build/vet clean)
   │
   ▼                              Phase 5 — Validation & Shadow Trading
                                  ─────────────────────────────────────
Task 19 ✅ COMPLETED (P1-C: end-to-end test injecting a known-good token bypassing L1)
   │
   ▼
Task 20 ✅ COMPLETED (P2-A: enable shadow mode; P2-B: review min_token_age_seconds for graduation)
   │
   ▼
Task 21 ✅ COMPLETED (Tests + build validation + PROGRESS_REPORT.md update)

                                  ──────────────────────────────────────────────
                                  Phase 6 — Gate-Review Remediation (2026-05-31)
                                  Source: gate review brief gate_brief_20260531_*
                                  ──────────────────────────────────────────────
Task 22  (Phase 3 hotfix: relax VERY_EXPLORATION quality gates → emit RISKY_PASS not SKIP)
   │
   ▼
Task 23  ✅ COMPLETED — (106 duplicate event_ids confirmed BENIGN: gate script counts stage_handler_failed×3 + event_moved_to_dlq×1 = 4 log entries per DQ-retried event; NOT DB rows; events(event_id) PRIMARY KEY + ON CONFLICT DO NOTHING confirmed; EventID=SHA256(chain|txHash|logIndex)[:16] — no UUID/time.Now(); 0 real duplicates in events table; follow-up: add stage_handler_failed/event_moved_to_dlq to gate script exclusion list to suppress false positive)
   │
   ▼
Task 24  ✅ COMPLETED — (Re-run inject_test_token.py — confirm L0→L10 now produces LearningRecordDTO)
   │
Task 25  ✅ COMPLETED — (Pre-cohort filter: drop creator_count > 25 BEFORE DQ to save Helius credits)
   │  (depends on Task 8 creator_profiles + Task 24 proof)
   ▼
Task 26  ✅ COMPLETED — (solana_holder_dist fallback: getTokenSupply + getProgramAccounts on timeout)
   │
   ▼
Task 27  ✅ COMPLETED — (Rescan eligibility: include SKIP'd tokens whose probes failed, mode-gated)
   │
   ▼
Task 28 ✅ COMPLETED  (Re-tighten VERY_EXPLORATION gates after Task 26 restores HolderDistKnown ≥ 90%)
```

**Ordering rules satisfied:**

- Migrations (Task 5) precede code that depends on the new schema (Tasks 8, 9, 10)
- DTO contract additions (Task 6) precede every module/probe task that produces or consumes them
- Config struct changes (Tasks 11, 15) precede YAML edits (Tasks 12, 16) and module changes (Tasks 13, 17, 18)
- Lower-layer changes (L0 in Tasks 2, 3, 7) precede L1 (Tasks 13, 18) which precede L7/L9/L10 reactions (covered in Tasks 9 and 19)
- Final task (21) covers build + vet + test + PROGRESS_REPORT.md
- Phase 3 (mode-aware serial launcher) is **strictly after Phase 2** (real creator identity) per Section 9 Sequencing Requirement — without wallet-level identity, per-mode thresholds of 5/10 would never change pump.fun outcomes (49 > 10)

---

### Task 1 ✅ COMPLETED — Fix Wrong Cost Comments in chains.yaml + Audit DAS Cost Annotations

**Goal:** Correct the misleading "100 credits per `getTransaction` call" comment in
`config/chains.yaml` and any DAS-related "1 credit" notes so future operators do not
make budgeting decisions on wrong numbers. (PRODUCTION_GATE_ANALYSIS § 4, § 5 Option 4,
§ 7 P1-B.)

**Layer(s) affected:** Config (documentation only — no Go code).

**Files to create/modify:**

- `config/chains.yaml` (modify) — replace the wrong cost comment around line 163
  - Old: `# Credit cost is minimal: ≤300 getTransaction calls/day × 100 credits = 900k credits/month`
  - New: `# Credit cost is minimal: ≤300 getTransaction calls/day × 1 credit = ~9k credits/month worst-case`
  - Add explanatory note: "getTransaction is 1 credit per Helius docs (helius.dev/docs/billing/credits). The 100-credit figure refers to Helius's proprietary `getTransactionsForAddress` Enhanced TX API, which this bot does not call."
- Any other file under `config/` or `internal/app/config/` matching `grep -rn "DAS\|das_asset\|1 credit"` — if found, annotate DAS calls as **10 credits per call**, citing `helius.dev/docs/billing/credits`
  - Cross-reference: §7.7 Helius credit reference table

**Invariant check:**

- [x] No code change — comment-only fix
- [x] No new dependencies
- [x] No threshold or magic number introduced
- [x] No security rule affected

**Validation:**

- `git diff config/chains.yaml`: shows only comment-line changes
- `grep -rn "100 credits.*getTransaction\|getTransaction.*100 credits" config/`: returns zero matches after fix
- `grep -rn "1 credit.*DAS\|DAS.*1 credit\|das_asset.*1 credit" config/ internal/app/config/`: returns zero matches after fix
- `go build ./...`: zero errors (sanity)

**Prompt context needed:** §7.7 Helius credit reference table.

---

### Task 2 ✅ COMPLETED — Add `disabled` Flag Support to Chains Program Config Struct

**Goal:** Allow a `disabled: true` field on each `solana.programs[]` entry in
`config/chains.yaml` so subscriptions can be turned off without commenting out blocks
(per Section 5 Option 1, Step 2). This is a precondition for Task 3.

**Layer(s) affected:** L0 ingestion / Config (`internal/app/config/`).

**Files to create/modify:**

- `internal/app/config/chains_config.go` (modify — locate via `grep -rn "ProgramID\|Program_id\|family:" internal/app/config/`)
  - Add field: `Disabled bool \`yaml:"disabled"\`` to the program-entry struct
  - Default = `false` (zero value); explicit `disabled: true` skips subscription
  - No DTO change; this is platform-level config
- `internal/modules/ingestion_solana/ingestion_solana.go` (modify)
  - Where the program list is iterated to create subscriptions, add: `if entry.Disabled { continue }`
  - Emit a structured log line at startup listing every skipped program (`ingestion_program_skipped` event) so operators can see what was disabled — use the existing event-bus emission pattern, NOT direct Telegram

**Invariant check:**

- [x] No SQL or DB driver imports added
- [x] No cross-module imports (config struct change is platform-level)
- [x] All values from YAML
- [x] No randomness
- [x] No security rule affected (no API key, no new endpoint)
- [x] DTO additive-only (no DTO touched)

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/app/config/...`: passes (existing tests + new test asserting `disabled: true` is parsed correctly)
- `go test ./internal/modules/ingestion_solana/...`: passes
- Unit test new: `TestProgramConfig_DisabledFlagDefaultsFalse` and `TestProgramConfig_DisabledFlagRespected`

**Prompt context needed:** §7.1 MarketDataDTO origin, §7.4 Chains config schema.

---

### Task 3 ✅ COMPLETED — Disable Raw pump.fun + Switch Raydium V4 to `transactionSubscribe`

**Goal:** Eliminate ~99% of WebSocket streaming credits by (a) disabling the raw
pump.fun bonding-curve subscription (`6EF8rrec...`, 100% rejection rate per Section 3
Problem B + Section 8 Change 1) and (b) replacing the Raydium V4 (`675kPX9...`)
`logsSubscribe` with `transactionSubscribe` filtered by the Raydium V4 authority account
(Section 5 Option 3). Keeps pumpfun-AMM (`pAMMBay6...`) graduation subscription intact.

**Layer(s) affected:** L0 ingestion / Config.

**Files to create/modify:**

- `config/chains.yaml` (modify)
  - Add `disabled: true` (with explanatory comment citing PRODUCTION_GATE_ANALYSIS § 5 Option 1) to the pump.fun bonding-curve program entry
  - Add `subscription_method: "transactionSubscribe"` and `account_filter: "5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1"` (Raydium V4 authority) to the Raydium V4 entry — see Section 5 Option 3 for the payload shape
  - Leave pumpfun-AMM entry untouched and confirm it is NOT disabled
- `internal/app/config/chains_config.go` (modify) — add optional fields:
  - `SubscriptionMethod string \`yaml:"subscription_method"\``— default`"logsSubscribe"` if empty
  - `AccountFilter string \`yaml:"account_filter"\``— optional; only used when method is`transactionSubscribe`
- `internal/modules/ingestion_solana/ingestion_solana.go` (modify)
  - Branch in the WS subscription builder: if `entry.SubscriptionMethod == "transactionSubscribe"`, send the payload with `accountInclude: [entry.AccountFilter]` per Section 5 Option 3 (commitment: `"confirmed"`, encoding: `"jsonParsed"`, transactionDetails: `"full"`, maxSupportedTransactionVersion: 0)
  - Otherwise keep the existing `logsSubscribe` path verbatim
  - Existing `ray_log` Initialize2 filter and `pumpfun_decode_from_logs` paths are preserved for backwards compatibility
  - Bounded WS message size handling unchanged

**Invariant check:**

- [x] HTTPS / WSS only (WS endpoint already `wss://`)
- [x] API key via existing `os.Getenv("HELIUS_API_KEY")` path — not added to YAML
- [x] No new external endpoint
- [x] DTO additive-only (no DTO change)
- [x] Determinism preserved — same events still produce same DTOs
- [x] No cross-module imports
- [x] Idempotency preserved — content-addressable EventIDs from upstream signature

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/modules/ingestion_solana/...`: all green; add `TestSubscriptionBuilder_TransactionSubscribeShape` asserting the exact JSON payload matches Section 5 Option 3
- Live verification (post-deploy, per Section 5 Option 1 Step 4 and Section 5 Option 2 verification):

  ```sql
  SELECT metadata->>'family' AS family, COUNT(*) AS events
  FROM events
  WHERE event_type = 'market_data_event' AND created_at > NOW() - INTERVAL '1 hour'
  GROUP BY family;
  ```

  - Expect `pumpfun` family count = 0 (disabled)
  - Expect `raydium-v4` family count > 0 within seconds of any new pool
  - Expect `pumpfun-amm` family count > 0 over 1–2 hours

- Helius Dashboard credit usage should drop 50–90% within 2 hours

**Prompt context needed:** §7.4 Chains config schema, §7.7 Helius credit reference table, §7.5 Event bus pattern.

---

### Task 4 ✅ COMPLETED — Runbook: Verify pump.fun-AMM Event Flow (P0-B)

**Goal:** Document and execute the SQL-based verification that pumpfun-AMM graduation
events are arriving in the event bus at the expected 1–3/hour rate (Section 6 + Section 7
P0-B). No code change.

**Layer(s) affected:** Operational runbook only.

**Files to create/modify:**

- `docs/PROGRESS_REPORT.md` (modify) — append a row to Session History noting
  "P0-B verification completed YYYY-MM-DD: pumpfun-amm event rate = X/h"
- No source-code file modified

**Invariant check:**

- [x] No code change → no invariant risk

**Validation:**

- Run the SQL query from PRODUCTION_GATE_ANALYSIS § 7 P0-B:
  ```sql
  SELECT metadata->>'family' AS family, COUNT(*) AS events_2h
  FROM events
  WHERE event_type = 'market_data_event' AND created_at > NOW() - INTERVAL '2 hours'
  GROUP BY family ORDER BY events_2h DESC;
  ```
- Expected: `pumpfun-amm` row present with count > 0 in any 2-hour window during typical trading hours
- If count = 0 after 6 hours, escalate (subscription may be silently failing)

**Prompt context needed:** §7.5 Event bus pattern.

---

### Task 5 — Migration: `creator_profiles` Table ✅ COMPLETED

**Goal:** Create the `creator_profiles` table that stores per-wallet aggregated launch
history (P1-A Goal 2). Idempotent updates, append-only event sourcing semantics
preserved (the table is a materialised view over `events` + `execution_results`).

**Layer(s) affected:** Database migration.

**Files to create/modify:**

- `database/migrations/20260101000NNN_creator_profiles.sql` (create — `NNN` = next sequential number after the highest existing migration; verify via `ls database/migrations/`)
  - Schema (portable SQL — `ON CONFLICT DO NOTHING` semantics required):
    ```sql
    CREATE TABLE IF NOT EXISTS creator_profiles (
        chain              TEXT        NOT NULL,
        creator_address    TEXT        NOT NULL,
        total_tokens       BIGINT      NOT NULL DEFAULT 0,
        rug_tokens         BIGINT      NOT NULL DEFAULT 0,
        migrated_tokens    BIGINT      NOT NULL DEFAULT 0,
        golden_gem_tokens  BIGINT      NOT NULL DEFAULT 0,
        win_tokens         BIGINT      NOT NULL DEFAULT 0,
        loss_tokens        BIGINT      NOT NULL DEFAULT 0,
        first_seen_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
        last_seen_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
        last_updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (chain, creator_address)
    );
    CREATE INDEX IF NOT EXISTS idx_creator_profiles_total ON creator_profiles (chain, total_tokens DESC);
    CREATE INDEX IF NOT EXISTS idx_creator_profiles_last_seen ON creator_profiles (chain, last_seen_at DESC);
    ```
  - File must use portable SQL only — no engine-specific syntax (no `INSERT OR IGNORE`)

**Invariant check:**

- [x] Migration file is append-only (new file, no existing migration touched)
- [x] Portable SQL only — `ON CONFLICT DO NOTHING` semantics, `CURRENT_TIMESTAMP`
- [x] Idempotent (`IF NOT EXISTS`)
- [x] No engine-specific syntax
- [x] Indexed for the two read patterns: per-creator lookup (PK) and top-N by total

**Validation:**

- `make migrate` (or equivalent — see `database/migrations.go`): migration applies cleanly on fresh DB
- Migration is idempotent: running twice produces no error
- `go test ./database/...`: passes

**Prompt context needed:** §7.6 creator_profiles schema, §7.5 Event bus pattern.

---

### Task 6 — Contracts: Add `SKIP` Decision + Market Cap & Volume Fields (Additive) ✅ COMPLETED

**Goal:** Two additive contract changes — required by Phases 3 and 4 respectively.
Must precede every module that reads or writes these fields.

**Layer(s) affected:** Contracts.

**Files to create/modify:**

- `contracts/data_quality.go` (modify)
  - Add `SKIP` as a valid `Decision` value alongside `PASS`, `RISKY_PASS`, `REJECT`
  - Add inline comment: "SKIP — silent drop. Token is dropped from the pipeline without emitting a `data_quality_event`. Used by EXPLORATION/VERY_EXPLORATION modes when a serial-launcher token fails the quality gate (see Section 9). SKIP must NOT contribute to reject-rate statistics in Layer 10."
  - Add `serial_launcher_monitored` and `serial_launcher_skipped` to the list of known flag string constants if such a list exists; otherwise document them as canonical flag values in the file-level comment
- `contracts/market_data.go` (modify) — add four fields, all zero-value-safe (Section 10 Change 2):

  ```go
  // MarketCapUsd is the token's total market capitalisation in USD at the time
  // the DEXScreener probe ran. Zero means the data was not available yet
  // (token too new, pair not yet indexed). A zero value disables the market
  // cap filter so brand-new tokens are not incorrectly rejected.
  MarketCapUsd float64

  // VolumeUsd5m / VolumeUsd1h / VolumeUsd24h are cumulative USD trading
  // volume over each window, sourced from DEXScreener. Zero means not available.
  VolumeUsd5m  float64
  VolumeUsd1h  float64
  VolumeUsd24h float64
  ```

  - Update field-level comment block at top of file referencing PRODUCTION_GATE_ANALYSIS § 10

- `docs/dto_contracts.md` is read-only per copilot-instructions.md — **do NOT modify**; instead the operational note for the DTO update will be captured in `docs/PROGRESS_REPORT.md` (Task 21) per the "PROGRESS_REPORT exception" rule

**Invariant check:**

- [x] DTO changes additive-only (new field + new enum value with zero-value defaults)
- [x] No existing field modified or removed
- [x] No SQL or DB import added
- [x] No cross-module import
- [x] No randomness
- [x] Existing consumers branch on `PASS`/`RISKY_PASS`/`REJECT` — verify default-case behaviour treats unknown values as drop (audit during Task 13 implementation)
- [x] `docs/dto_contracts.md` NOT modified (read-only per project policy)

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./contracts/...`: all green
- Add unit tests: `TestDataQualityDTO_SkipIsValidDecision`, `TestMarketDataDTO_ZeroMarketCapDoesNotPanic`

**Prompt context needed:** §7.2 DataQualityDTO schema, §7.3 MarketDataDTO schema.

---

### Task 7 — Ingestion Guard: Reject Factory-Program Creator Identity (P1-A Goal 1) ✅ COMPLETED

**Goal:** Hard guarantee that `MarketDataDTO.CreatorAddress` is never the
pump.fun factory program ID (`6EF8rrec...`) or the pump.fun-AMM program ID
(`pAMMBay6...`). Prefer event-derived `event.User` / `event.Creator` where available
(already implemented in `NormalizePumpFunCreateFromLogs` and
`NormalizePumpFunAMMCreatePool`). When the resolved creator is a known program ID,
either fall back to the event-derived creator or emit a structured telemetry warning.

**Layer(s) affected:** L0 ingestion (`internal/modules/ingestion_solana/`).

**Files to create/modify:**

- `internal/modules/ingestion_solana/ingestion_solana.go` (modify) — locate the
  `NormalizePumpFun*` helpers and add a guard helper:

  ```go
  // knownPumpFunPrograms is the set of program IDs that must NEVER appear as
  // CreatorAddress. They identify a platform, not a human wallet.
  var knownPumpFunPrograms = map[string]struct{}{
      "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P": {}, // bonding curve
      "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA": {}, // AMM
  }
  func isFactoryProgram(addr string) bool {
      _, ok := knownPumpFunPrograms[addr]
      return ok
  }
  ```

  - In every normaliser that may emit a `MarketDataDTO` for pump.fun families, add a final guard:
    - If `CreatorAddress` is a factory program AND an event-derived wallet exists → use the wallet
    - If `CreatorAddress` is a factory program AND no fallback exists → leave `CreatorAddress=""` and emit a structured `ingestion_creator_identity_unresolved` event (telemetry only; not a DQ reject — DQ will fail-closed via `unknown_creator_count` in STRICT/BALANCED as before)

- Add counters (per the event-bus observability pattern): `count_events_with_program_creator`, `count_events_with_corrected_creator` — emit periodically as a `system_event`

**Invariant check:**

- [x] No cross-module import
- [x] All program IDs in code are constants in this file; longer term they should move to `config/chains.yaml` — defer as a follow-up note in §7
- [x] No randomness
- [x] No security rule affected
- [x] DTO additive-only — `CreatorAddress` field already exists; this only changes which value is assigned
- [x] Determinism preserved

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/modules/ingestion_solana/...`: all green; add `TestNormalize_FactoryCreatorIsReplacedByEventUser` and `TestNormalize_UnknownCreatorFallsToEmpty`

**Prompt context needed:** §7.1 MarketDataDTO origin, §7.8 Section-9 sequencing.

---

### Task 8 ✅ COMPLETED — Creator Profile Aggregator Worker (P1-A Goal 2)

**Goal:** Background worker that consumes the lifecycle of every token through the
pipeline (`market_data_event`, `execution_results`, position-closed events, learning
outcomes) and updates the `creator_profiles` table with idempotent CAS-style upserts
(`ON CONFLICT (chain, creator_address) DO UPDATE` semantics with monotonic counter
increments). Lives in `internal/workers/` per the orchestrator authority rule (modules
must not write to the DB).

**Layer(s) affected:** Workers / Database.

**Files to create/modify:**

- `internal/workers/creator_profile_aggregator.go` (create)
  - Consumes via `SELECT ... FOR UPDATE SKIP LOCKED` from the existing event-bus pattern (see `internal/workers/` neighbours for the loop shape)
  - Tracks offset in `consumer_offsets` table (existing infra)
  - For each `market_data_event` with non-empty `CreatorAddress` and not a known factory program: increments `total_tokens` and `last_seen_at`; INSERTs with `ON CONFLICT DO UPDATE` to maintain idempotency
  - For each `learning_record_event` with classification = rug/migrated/golden: increments the corresponding bucket
  - Uses `database/adapter.*` for ALL DB access — no direct driver import
  - Emits `creator_profile_updated` system_event on every update (Layer-10 may consume this as a feature)
- `database/adapter.go` (modify) — add three thin methods:
  - `UpsertCreatorProfileOnLaunch(ctx, chain, creator string) error`
  - `IncrementCreatorOutcome(ctx, chain, creator, outcome string) error`
  - `GetCreatorProfile(ctx, chain, creator string) (CreatorProfile, error)` — returns DTO, NOT a raw row
- New DTO in `contracts/` if a row mapping is needed: `contracts/creator_profile.go` with `CreatorProfile` struct (additive only; immutable)
- Wire the worker into `cmd/server.go` (or wherever workers are started) — per orchestrator authority

**Invariant check:**

- [x] Idempotent: `ON CONFLICT (chain, creator_address) DO UPDATE` with monotonic increments
- [x] Workers consume via `SELECT FOR UPDATE SKIP LOCKED`
- [x] No SQL leaked into modules — adapter is the sole DB boundary
- [x] DTO additive-only (new struct in contracts/)
- [x] Determinism preserved — same event sequence produces same counts (event ordering by `created_at` + `id`)
- [x] No randomness

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/workers/...`: all green
- `go test ./database/...`: all green
- Add integration test: `TestCreatorProfileAggregator_IsIdempotent` — emit the same event twice, profile is updated exactly once

**Prompt context needed:** §7.5 Event bus pattern, §7.6 creator_profiles schema.

---

### Task 9 ✅ COMPLETED — DQ Uses creator_profiles via Injected Reader (P1-A Goal 3)

**Goal:** Augment the DQ serial-launcher check with the per-wallet count from
`creator_profiles` when available, while preserving fail-closed behaviour when the
profile is unknown. Module isolation is preserved by injecting a `CreatorProfileReader`
interface at construction; the orchestrator wires the adapter-backed implementation.

**Layer(s) affected:** L1 (Data Quality).

**Files to create/modify:**

- `internal/modules/data_quality/data_quality.go` (modify)
  - Add interface `CreatorProfileReader` with `GetCount(ctx, chain, creator string) (count int32, known bool, err error)` — interface lives in the consuming module per Go dependency-inversion idiom
  - Add field to the module struct and constructor parameter
  - In `ProcessForMode()`, BEFORE the existing serial-launcher block: if `in.CreatorAddress != ""` and reader is non-nil, call `GetCount`. When `known == true`, use the larger of (`CreatorPrevTokenCount` from probe, profile count) for the check. When `known == false`, do NOT change behaviour — fall back to existing `CreatorPrevTokenCountKnown` semantics
  - This is **additive only** — the existing probe-based path is unchanged
- `internal/modules/orchestrator/orchestrator.go` (modify — locate the DQ wiring) — pass an adapter-backed `CreatorProfileReader` implementation. Implementation lives in a non-module package (e.g., `internal/app/wiring/` or co-located with adapter) and CALLS the adapter from there, NOT from the module

**Invariant check:**

- [x] No SQL or DB driver imports inside `internal/modules/data_quality/`
- [x] No cross-module imports — uses adapter-backed reader injected at construction
- [x] All thresholds still from config
- [x] No randomness
- [x] Fail-closed when reader returns error or `known == false` — existing behaviour preserved
- [x] Hard rejects in STRICT/BALANCED unchanged

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/modules/data_quality/...`: all green; add tests:
  - `TestProcessForMode_UsesProfileCountWhenAvailable`
  - `TestProcessForMode_FallsBackToProbeWhenProfileUnknown`
  - `TestProcessForMode_FailClosedWhenReaderErrors`

**Prompt context needed:** §7.2 DataQualityDTO schema, §7.6 creator_profiles schema, §7.8 Section-9 sequencing.

---

### Task 10 — Telegram Operator Command `/devstats` via Event Bus (P1-A Goal 4)

**Goal:** Operators can query creator profile stats from Telegram. Modules MUST NOT
call Telegram directly — the dispatcher pattern is preserved: the request emits a
`system_event`, the Telegram dispatcher consumes it and replies via the existing
dispatch path.

**Layer(s) affected:** Telegram dispatcher / Workers.

**Files to create/modify:**

- `internal/telegram/dispatcher.go` (modify — locate via `grep -n "command\|handler" internal/telegram/`)
  - Register new operator command `/devstats <creator_address>`
  - Handler emits a `system_event` of type `creator_stats_request`
  - Response is emitted from a small worker (next item) and dispatcher relays it
- `internal/workers/creator_stats_responder.go` (create)
  - Consumes `creator_stats_request` events
  - Calls `adapter.GetCreatorProfile()` and `adapter.GetCreatorProfileDerivedPercentages()` (small wrapper)
  - Emits `system_event` of type `creator_stats_response` containing: creator address, total tokens, rug_pull_pct, migrated_pct, golden_gem_pct
- Telegram dispatcher consumes the response event and formats per the existing message template

**Invariant check:**

- [x] No direct Telegram API call from any module — dispatcher pattern preserved
- [x] All comms via event bus
- [x] No new HTTP endpoint
- [x] No API token in YAML
- [x] No randomness

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/workers/...`: green; add `TestCreatorStatsResponder_EmitsResponseEvent`
- Manual: `/devstats <known_creator>` in dev Telegram returns formatted stats

**Prompt context needed:** §7.5 Event bus pattern, §7.6 creator_profiles schema.

---

### Task 11 — Config Struct: Add Per-Mode Serial-Launcher Fields (Section 9 Step 1)

**Goal:** Add four new optional fields to `DataQualityModeProfile` so each operational
mode can override the global serial-launcher threshold and define quality-gate values.

**Layer(s) affected:** Config struct (`internal/app/config/`).

**Files to create/modify:**

- `internal/app/config/data_quality_runtime_config.go` (modify) — add the four fields
  verbatim from PRODUCTION_GATE_ANALYSIS § 9 Step 1:

  ```go
  type DataQualityModeProfile struct {
      RejectAbove        float64 `yaml:"reject_above"`
      RiskyPassAbove     float64 `yaml:"risky_pass_above"`
      UnknownFactor      float64 `yaml:"unknown_factor"`
      MinTokenAgeSeconds int32   `yaml:"min_token_age_seconds"`

      MaxCreatorPrevTokenCount          int32   `yaml:"max_creator_prev_token_count"`
      SerialLauncherRequiresSocialLinks bool    `yaml:"serial_launcher_requires_social_links"`
      SerialLauncherMaxRiskScore        float64 `yaml:"serial_launcher_max_risk_score"`
      SerialLauncherMinHolderCount      int32   `yaml:"serial_launcher_min_holder_count"`
  }
  ```

  - Field comments must explain: `MaxCreatorPrevTokenCount == 0` means "use global threshold; STRICT/BALANCED behaviour unchanged"; `> 0` means "exploration mode override" — referencing §7.8 of this plan

**Invariant check:**

- [x] No DTO change
- [x] No SQL / DB driver added
- [x] All new fields have safe zero-value defaults (existing modes that don't set them stay strict)
- [x] No randomness
- [x] Backward compatible — existing YAML without the new keys still parses

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/app/config/...`: green; add `TestModeProfile_NewFieldsZeroByDefault`

**Prompt context needed:** §7.9 Section-9 implementation details.

---

### Task 12 — data_quality.yaml: Per-Mode Profiles (Section 9 Step 2)

**Goal:** Add the four `mode_profiles` blocks per Section 9 Step 2 — STRICT/BALANCED
keep global (0), EXPLORATION = 5, VERY_EXPLORATION = 10, with quality-gate values.

**Layer(s) affected:** YAML config.

**Files to create/modify:**

- `config/data_quality.yaml` (modify) — paste the entire `mode_profiles` block from
  PRODUCTION_GATE_ANALYSIS § 9 Step 2 verbatim. Confirm:
  - `strict` and `balanced` have `max_creator_prev_token_count: 0` (sentinel for "use global")
  - `exploration` has `max_creator_prev_token_count: 5`, `serial_launcher_max_risk_score: 0.40`, `serial_launcher_min_holder_count: 50`, `serial_launcher_requires_social_links: true`
  - `very_exploration` has `max_creator_prev_token_count: 10`, `serial_launcher_max_risk_score: 0.45`, `serial_launcher_min_holder_count: 25`, `serial_launcher_requires_social_links: true`
  - Global `thresholds.max_creator_prev_token_count: 1` remains unchanged

**Invariant check:**

- [x] Existing global threshold (1) is NOT raised
- [x] All values are config-driven
- [x] Backward compatible — existing config keys preserved

**Validation:**

- `go build ./...`: zero errors
- `go test ./internal/app/config/...`: green
- YAML lint passes
- Diff inspection: no existing field removed or modified

**Prompt context needed:** §7.9 Section-9 implementation details.

---

### Task 13 — ProcessForMode: Mode-Aware Serial Launcher + `buildSkipResult` (Section 9 Step 3)

**Goal:** Replace the existing serial-launcher hard-reject blocks in `ProcessForMode()`
with the mode-aware logic from Section 9 Step 3. Introduce the `buildSkipResult` helper
that returns a `DataQualityDTO` with `Decision: SKIP` and **does not emit a
`data_quality_event`** (orchestrator drops the token silently).

**Layer(s) affected:** L1 (Data Quality).

**Files to create/modify:**

- `internal/modules/data_quality/data_quality.go` (modify) — paste the mode-aware logic
  from PRODUCTION_GATE_ANALYSIS § 9 Step 3 verbatim, with these adjustments:
  - `effectiveMaxCreator` resolved from `profile.MaxCreatorPrevTokenCount` (per-mode) falling back to `m.runtime.Thresholds.MaxCreatorPrevTokenCount` (global) — `0 == use global`
  - When per-mode > 0 AND quality gates pass → emit `RISKY_PASS` + flag `serial_launcher_monitored`
  - When per-mode > 0 AND quality gates fail → call `buildSkipResult` → return SKIP outcome
  - For `CreatorPrevTokenCountKnown == false`: STRICT/BALANCED behaviour unchanged; EXPLORATION/VERY_EXPLORATION → SKIP
  - Per Section 9: `SerialLauncherMaxRiskScore` is checked post-aggregation in the decision phase (RiskScore not yet available in this block); document this in a code comment
- New helper `buildSkipResult(in MarketDataDTO, flags []string, profileName string) DataQualityDTO`:
  - Returns DTO with `Decision: contracts.SKIP`, `Flags: flags`, `RejectionReasons: nil`
  - Caller (orchestrator) checks `dto.Decision == SKIP` and drops the token without emitting `data_quality_event` AND updates `token_lifecycle` to a terminal `skipped` state for traceability (per §7.10 — token_lifecycle is the audit trail, not the rejection stream)
- `internal/modules/orchestrator/orchestrator.go` (modify) — handle the new `SKIP` outcome: do NOT emit `data_quality_event`; DO transition token_lifecycle state to `skipped` via the state machine module

**Invariant check:**

- [x] DTO additive-only — `SKIP` is a new enum value (Task 6)
- [x] No SQL in module — DB writes for token_lifecycle go through the orchestrator + adapter
- [x] No cross-module imports
- [x] All thresholds from config (via `profile.*`)
- [x] No randomness
- [x] Layer-1 hard rejects in STRICT/BALANCED unchanged (per Section 9 invariant)
- [x] Fail-closed preserved in STRICT/BALANCED for unknown creator
- [x] `SKIP` does NOT pollute reject-rate stats — Layer 10 (Task 18 of separate plan or as part of Task 21 verification) must filter by `Decision IN ('REJECT')`

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/modules/data_quality/...`: all green; add unit tests covering all four modes × (known | unknown creator) × (quality gates pass | fail) matrix
- `go test ./internal/modules/orchestrator/...`: all green; add `TestOrchestrator_SkipDoesNotEmitDataQualityEvent`

**Prompt context needed:** §7.2 DataQualityDTO schema, §7.9 Section-9 implementation details, §7.10 token_lifecycle.

---

### Task 14 — decision.go: Update canonicalProfile Fallback Map (Section 9 Step 4)

**Goal:** Update the in-code hardcoded fallback map so the new per-mode fields have
correct defaults even if the YAML is hot-reloaded with missing keys.

**Layer(s) affected:** L1 (Data Quality).

**Files to create/modify:**

- `internal/modules/data_quality/decision.go` (modify) — paste the `canonicalProfile` map
  from PRODUCTION_GATE_ANALYSIS § 9 Step 4 verbatim

**Invariant check:**

- [x] Values match `config/data_quality.yaml` defaults from Task 12
- [x] No randomness
- [x] No new threshold introduced beyond what is in config

**Validation:**

- `go build ./...`: zero errors
- `go test ./internal/modules/data_quality/...`: green; add `TestCanonicalProfile_MatchesYAMLDefaults`

**Prompt context needed:** §7.9 Section-9 implementation details.

---

### Task 15 — Config Struct: Market-Cap & Volume Threshold Fields (Section 10)

**Goal:** Add three new optional threshold fields to `DataQualityDetectorThresholds`.
All default to 0 (filter disabled) — matches the "commented-out by default" YAML stance.

**Layer(s) affected:** Config struct.

**Files to create/modify:**

- `internal/app/config/data_quality_runtime_config.go` (modify — same file as Task 11):

  ```go
  MinMarketCapUsd float64 `yaml:"min_market_cap_usd"`
  MaxMarketCapUsd float64 `yaml:"max_market_cap_usd"`
  MinVolumeUsd1h  float64 `yaml:"min_volume_usd_1h"`
  ```

  - Field comments must state: `0 = filter disabled`; threshold guards inside DQ must check `> 0` on BOTH the threshold AND the input field (`in.MarketCapUsd > 0`) so brand-new tokens not yet indexed by DEXScreener are not falsely rejected

**Invariant check:**

- [x] Additive — no field removed or modified
- [x] Safe zero defaults
- [x] No randomness

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/app/config/...`: green

**Prompt context needed:** §7.11 Section-10 implementation details.

---

### Task 16 — data_quality.yaml: Commented-Out Market-Cap & Volume Thresholds (Section 10)

**Goal:** Document the new thresholds in YAML but leave them commented out by default
per Section 10 caveat about graduation tokens potentially exceeding the $20k cap.

**Layer(s) affected:** YAML config.

**Files to create/modify:**

- `config/data_quality.yaml` (modify) — add the three keys under the existing
  `thresholds:` block, all commented out, with the explanatory comments from
  PRODUCTION_GATE_ANALYSIS § 10 Change 3 verbatim:

  ```yaml
  thresholds:
    # ... existing keys unchanged ...

    # Market cap range filter (DEXScreener data).
    # Only applied when MarketCapUsd > 0 (i.e., pair is indexed on DEXScreener).
    # Commented out by default — enable and tune after confirming graduation token
    # market cap distribution in shadow mode.
    # min_market_cap_usd: 3000.0
    # max_market_cap_usd: 20000.0

    # Volume floor (DEXScreener data).
    # Only applied when VolumeUsd1h > 0.
    # min_volume_usd_1h: 100.0
  ```

**Invariant check:**

- [x] No existing field removed or modified
- [x] All new values are commented out — no behavioural change until operator opts in
- [x] Comments reference the source of truth (§ 10)

**Validation:**

- YAML still parses (commented keys ignored by YAML parser)
- `go test ./internal/app/config/...`: green

**Prompt context needed:** §7.11 Section-10 implementation details.

---

### Task 17 — Expand DEXScreener Parser + Probe Populates MarketDataDTO Fields (Section 10 Change 1 + Architecture Note)

**Goal:** Expand the DEXScreener response parser to capture market cap and volume
(already in the response body, currently discarded), and update the Layer-1 DEXScreener
probe to populate the new `MarketDataDTO` fields. Per Section 10 architecture note: the
DQ probe is the correct populating point (data must be available BEFORE `ProcessForMode()`
runs structural rejects), not the price-fetcher used by L9.

**Layer(s) affected:** L1 probes / Price feed parser.

**Files to create/modify:**

- `internal/rpc/price_fetcher.go` (modify) — paste the expanded `dexScreenerResponse`
  struct from PRODUCTION_GATE_ANALYSIS § 10 Change 1 verbatim. Bounded `LimitReader`
  with 128 KiB cap **must remain unchanged** — security invariant
- `internal/modules/probes/dexscreener_probe.go` (modify — locate via
  `grep -rn "dexscreener\|DEXScreener" internal/modules/probes/`) — when the probe
  parses the response, additionally populate:
  - `out.MarketCapUsd = parsed.Pairs[0].MarketCap`
  - `out.VolumeUsd5m = parsed.Pairs[0].Volume.M5` (nil-safe)
  - `out.VolumeUsd1h = parsed.Pairs[0].Volume.H1`
  - `out.VolumeUsd24h = parsed.Pairs[0].Volume.H24`
- Optionally: `internal/rpc/price_fetcher.go` — parse the same fields for observability (not on the DQ critical path). If added, ensure they are not used to feed back into L9 exit decisions without a separate plan (out-of-scope here)

**Invariant check:**

- [x] HTTPS-only DEXScreener URL preserved
- [x] 128 KiB `LimitReader` preserved — parser expansion does NOT raise body cap
- [x] Additive parser — existing parse paths unchanged
- [x] DTO additive (fields from Task 6)
- [x] No randomness
- [x] No new external dependency

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/rpc/...`: green; add `TestDEXScreenerParser_CapturesMarketCapAndVolume`
- `go test ./internal/modules/probes/...`: green; add `TestDEXScreenerProbe_PopulatesNewFields`

**Prompt context needed:** §7.3 MarketDataDTO schema, §7.11 Section-10 implementation details, §7.12 Security invariants.

---

### Task 18 — DQ Structural Rejects: market_cap_too_low|too_high, volume_too_low (Section 10 Change 3)

**Goal:** Add the structural-reject block from Section 10 Change 3 to `ProcessForMode()`.
Guards on `> 0` for both threshold and input value mean: filter is inert until the
operator enables it AND the probe has populated the field.

**Layer(s) affected:** L1 (Data Quality).

**Files to create/modify:**

- `internal/modules/data_quality/data_quality.go` (modify — same file as Task 13) —
  paste the structural-reject snippet from PRODUCTION_GATE_ANALYSIS § 10 Change 3
  verbatim. Place AFTER the existing mandatory hard rejects (serial launcher, social
  links, total supply) so those continue to fire first

**Invariant check:**

- [x] Layer-1 mandatory hard rejects still fire first (serial launcher in STRICT/BALANCED, social links, total supply)
- [x] All thresholds from config
- [x] No hardcoded values
- [x] Filter inert when threshold = 0 OR input = 0 (brand-new token guard)
- [x] DTO additive (rejection reason strings are not enum-bound)
- [x] No randomness

**Validation:**

- `go build ./...`: zero errors
- `go test ./internal/modules/data_quality/...`: green; add tests:
  - `TestProcessForMode_MarketCapFilterInertWhenThresholdZero`
  - `TestProcessForMode_MarketCapFilterInertWhenFieldZero`
  - `TestProcessForMode_RejectsMarketCapTooLow`
  - `TestProcessForMode_RejectsMarketCapTooHigh`
  - `TestProcessForMode_RejectsVolumeTooLow`

**Prompt context needed:** §7.2 DataQualityDTO schema, §7.11 Section-10 implementation details.

---

### Task 19 — End-to-End Pipeline Validation: Inject Known-Good Test Token Bypassing L1 (P1-C)

**Goal:** Per Section 7 P1-C and Section 8 Change 5: trace a known-good token
through L2–L10 by directly injecting a `market_data_event` with quality flags
pre-approved, bypassing L1. Find bugs in L2–L10 BEFORE shadow trading begins.

**Layer(s) affected:** L2–L10 (read-only validation); replay engine pattern (uses
`replay:` prefix).

**Files to create/modify:**

- `scripts/inject_test_token.py` (create) — script that:
  - Accepts a token address and chain via CLI
  - Builds a `market_data_event` payload with realistic values for a known-good historical token (e.g., one that 10x'd on Raydium in the past 30 days — operator selects)
  - Marks all probe-derived flags as `*Known=true, Has*=true, risk_score=0.10`
  - INSERTs into `events` with `EventID = "replay:" + SHA256(content)[:16]` per the replay engine pattern
  - Uses the existing replay prefix so production workers do NOT consume the event — only replay-mode workers do
- Operator runbook in `docs/PROGRESS_REPORT.md` Session History entry: how to start a replay-mode worker scope and inspect the resulting `feature_event`, `edge_event`, `probability_event`, `slippage_event`, `latency_event`, `validated_edge_event`, `selection_event`, `allocation_event` chain
- For each layer that produces no output or errors: file a follow-up task in a new PLAN (separate plan, NOT this one) — Task 19 is read-only validation, not fixing

**Invariant check:**

- [x] Replay events use `replay:` prefix — production workers NEVER consume them
- [x] No code change in pipeline modules — this task is read-only validation
- [x] No production state corruption
- [x] Idempotent: `EventID` is content-addressable; re-running the script produces no duplicates

**Validation:**

- Script runs cleanly: `python scripts/inject_test_token.py --chain solana --token <address>`
- Replay worker scope produces non-empty results for at least L2–L7
- All discrepancies documented in `docs/PROGRESS_REPORT.md` Session History; new plan file created for any L2–L10 fix work

**Prompt context needed:** §7.5 Event bus pattern, §7.13 Replay engine pattern.

---

### Task 20 — Enable Shadow Mode + Review min_token_age_seconds for Graduation Market (P2-A + P2-B)

**Goal:** Per Section 7 P2-A and P2-B: once Task 19 has confirmed at least one clean
L2→L7 trace, switch `config/pipeline.yaml` execution mode to `shadow` and start a
2-week paper-trade observation window. Use shadow data to evaluate whether
`min_token_age_seconds: 900` should be lowered for the pumpfun-amm graduation market
specifically.

**Layer(s) affected:** Config / L8 (execution mode flag) / L9 (observation only).

**Files to create/modify:**

- `config/pipeline.yaml` (modify) — change `execution.mode: "live"` (or current value) to `execution.mode: "shadow"`
- `docs/PROGRESS_REPORT.md` Session History — record start date, plus the SQL monitoring query from Section 7 P2-A for tracking paper P&L:
  ```sql
  SELECT COUNT(*) AS total_shadow_trades,
         AVG(realized_pnl_bps) AS avg_pnl_bps,
         SUM(CASE WHEN realized_pnl_bps > 0 THEN 1 ELSE 0 END) AS wins,
         SUM(CASE WHEN realized_pnl_bps <= 0 THEN 1 ELSE 0 END) AS losses
  FROM execution_results
  WHERE created_at > NOW() - INTERVAL '14 days';
  ```
- After ≥ 14 days AND ≥ 30 completed shadow trades: evaluate `min_token_age_seconds` for `pumpfun-amm` cohort. If graduation tokens consistently peak in the first 3–5 minutes after Raydium listing, file a follow-up plan to add a per-family `min_token_age_seconds` override (NOT done in this plan — Section 10 explicitly defers)

**Invariant check:**

- [x] Shadow mode generates buy/sell signals but DOES NOT submit transactions — execution engine respects mode flag
- [x] No real capital at risk
- [x] Kill switch behaviour unchanged
- [x] No config field removed or modified beyond `execution.mode`

**Validation:**

- Bot restarts cleanly in shadow mode
- After 24h: `SELECT COUNT(*) FROM execution_results WHERE created_at > NOW() - INTERVAL '24 hours' AND mode = 'shadow'` returns a non-zero count if any token reached L7
- After 14 days: paper-trade P&L collected and reviewed; decision recorded on whether to advance to micro-capital live mode (per Section 8 Change 6 step 13)

**Prompt context needed:** §7.14 Operational modes, §7.15 Shadow vs live execution.

---

### Task 21 — Tests, Build Validation, and PROGRESS_REPORT.md Update

**Goal:** Final gate — run the full test suite, verify build/vet/test green, and
record this plan's completion in `docs/PROGRESS_REPORT.md` (the only writable doc).

**Layer(s) affected:** All (validation only) + Docs (`PROGRESS_REPORT.md` only).

**Files to create/modify:**

- `docs/PROGRESS_REPORT.md` (modify — the sole writable doc) — append:
  - Phase Progress row: "Production Gate Hardening — completed YYYY-MM-DD"
  - Agent Pipeline Results section: which subagents were used (module-builder, dto-guardian, integration, test-builder, security-auditor)
  - Quality Gates section: build/vet/test outcomes
  - Session History: replay validation results from Task 19; shadow mode start date from Task 20

**Invariant check:**

- [x] Only `docs/PROGRESS_REPORT.md` modified under `docs/`
- [x] No other `docs/` file touched (all read-only per project policy)

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `go test ./...`: all 38+ packages green
- `go test -race ./...`: green (race detector — for the new worker)
- Final pre-deploy invariant scan:
  - `grep -rn "math/rand" internal/` returns no new occurrences in modified files
  - `grep -rn "io.ReadAll" internal/ | grep -v LimitReader` returns no new occurrences
  - `grep -rn "INSERT OR IGNORE" database/migrations/` returns zero (only `ON CONFLICT DO NOTHING` allowed)
  - `grep -rn "math\.Rand\|rand\.Int" internal/` returns no new occurrences
  - `grep -rn "BIRDEYE_API_KEY\|HELIUS_API_KEY\|JITO_BUNDLE_URL\|TWITTER_BEARER_TOKEN" config/` returns zero (env-only invariant)

**Prompt context needed:** §7.16 Validation commands, §7.17 Definition of Done.

---

### Task 22 — Phase 3 Hotfix: Relax VERY_EXPLORATION Quality Gates to Convert SKIP → RISKY_PASS ✅ COMPLETED

**Goal:** Apply the gate-review **NEXT SINGLE ACTION** (config-only change) so that
serial-launcher tokens under `VERY_EXPLORATION` produce `RISKY_PASS +
serial_launcher_monitored` (which DOES emit `data_quality_event`) instead of silent
`SKIP` (which does NOT emit). Unblocks L2→L10 starvation observed in the 2026-05-31
gate review without touching Phase 3 code semantics.

**Why this is config-only:** Per §7.18, Phase 3 quality gates (`HasSocialLinks`,
`HolderCount`) are correctly implemented, but production pump.fun graduates almost
never satisfy them (no profile-level socials; `solana_holder_dist` probe times out).
Setting `serial_launcher_requires_social_links: false` and
`serial_launcher_min_holder_count: 0` on `very_exploration` makes the gates inert for
the most permissive mode only — `STRICT`, `BALANCED`, and `EXPLORATION` remain
unchanged.

**Layer(s) affected:** YAML config (`config/data_quality.yaml`).

**Files to create/modify:**

- `config/data_quality.yaml` (modify) — within the existing `mode_profiles.very_exploration:` block:
  - Change `serial_launcher_requires_social_links: true` → `false`
  - Change `serial_launcher_min_holder_count: 25` → `0`
  - Keep `max_creator_prev_token_count: 10` and `serial_launcher_max_risk_score: 0.45` unchanged
  - Above the changed lines, add a comment block: `# HOTFIX 2026-05-31 (Task 22): VERY_EXPLORATION temporarily relaxes quality gates to convert SKIP→RISKY_PASS for pipeline-proof. Re-tighten in Task 28 once shadow data justifies values.`
- Do NOT touch `strict:`, `balanced:`, or `exploration:` blocks
- Do NOT change the global `thresholds.max_creator_prev_token_count: 1`

**Invariant check:**

- [x] STRICT/BALANCED hard-reject (`max_creator_prev_token_count: 1` global) unchanged
- [x] EXPLORATION quality gates unchanged
- [x] No code change — invariants intact by construction
- [x] No new threshold field; only existing per-mode values flipped
- [x] Phase 3 SKIP path still fires for STRICT/BALANCED (via global) and EXPLORATION (still has socials/holder gates)
- [x] Three MANDATORY hard-rejects (serial*launcher in strict modes, no_social_links, high_total_supply) NOT bypassed — this task only changes the \_secondary* serial-launcher quality gate in VERY_EXPLORATION
- [x] Reversible by reverting the YAML change

**Validation:**

- `go build ./...`: zero errors (sanity — no Go change expected)
- `go test ./internal/modules/data_quality/...`: green; existing `serial_launcher_mode_test.go` matrix already covers the relaxed-gate path
- Restart bot in VERY_EXPLORATION mode; within 5 minutes the `data_quality_event` count > 0 with `decision = 'RISKY_PASS'` and `flags @> '["serial_launcher_monitored"]'`
- Telegram `/pipeline` shows `DQ_RISKY_PASSED > 0` and downstream `FEATURE_EVENT > 0`
- SQL spot check:
  ```sql
  SELECT decision, COUNT(*) FROM events
  WHERE event_type='data_quality_event' AND created_at > NOW() - INTERVAL '15 minutes'
  GROUP BY decision;
  ```

**Prompt context needed:** §7.9 Section-9 implementation details, §7.18 Gate-review hotfix rationale.

---

### Task 23 ✅ — Verify 106 Duplicate `event_id` Collisions Are Benign

**Goal:** Resolve BLOCKER 2 from the 2026-05-31 gate review by confirming that the 106
duplicate `event_id` collisions reported by the auto-checker are upstream re-emits
correctly deduped by `ON CONFLICT DO NOTHING` on `events(event_id)`, NOT lost events
or producer-side bugs. Either close the finding (benign) or file a follow-up plan.

**Layer(s) affected:** Observability / Workers (read-only diagnosis).

**Files to create/modify:**

- No code change unless the investigation finds a real bug
- `docs/PROGRESS_REPORT.md` (modify — sole writable doc) — append Session History
  entry: `event_id duplication audit YYYY-MM-DD: <N> distinct event_ids investigated;
<K> confirmed benign WS-reconnect replay; <M> escalated to <new plan>`

**Investigation steps:**

1. Query the auto-checker output (or `output/logs/gate_brief_*.txt`) to obtain a
   sample of the 106 duplicate IDs.
2. For each sampled `event_id`, run:

   ```sql
   SELECT event_id, event_type, COUNT(*) AS occurrences,
          MIN(created_at) AS first_seen, MAX(created_at) AS last_seen
   FROM events WHERE event_id = $1 GROUP BY event_id, event_type;
   ```

   - **Benign signature:** `occurrences = 1` (DB deduped); the auto-checker is counting
     _attempted_ INSERTs from logs, not actual rows.
   - **Real bug signature:** `occurrences > 1` for the same `event_id` (would mean a
     unique-constraint violation slipped through — should be impossible).

3. Cross-check producer code paths for content-addressable EventID computation per §7.5:
   - `internal/modules/ingestion_solana/*.go` — every emit path
   - `internal/workers/run_*.go` — every event emission
4. If any producer assembles `event_id` from a non-content source (timestamp, counter,
   uuid), that is a real bug — file a follow-up `docs/plans/YYYY-MM-DD-event-id-determinism-plan.md`
   and stop work on Task 23. Do NOT fix in this plan.

**Invariant check:**

- [x] Read-only investigation; no schema or producer change in this task
- [x] If real bug found: file separate plan (this plan does not absorb scope creep)
- [x] DB-level idempotency (`ON CONFLICT DO NOTHING`) preserved regardless of finding

**Validation:**

- All sampled IDs return `occurrences = 1` → finding closed as benign
- OR new plan file exists and is referenced in PROGRESS_REPORT.md entry
- `go build ./...`: green (sanity)

**Prompt context needed:** §7.5 Event bus pattern, §7.19 Duplicate event_id triage.

---

### Task 24 — Inject Synthetic Token to Prove L0→L10 (Re-Run of Task 19 Under New Conditions) ✅ COMPLETED

**Goal:** With Task 22 unblocking emission, execute `scripts/inject_test_token.py`
(created in Task 19) to confirm a single synthetic token now travels the full
L2→L10 chain and produces a `LearningRecordDTO`. This is the PIPELINE_PROOF exit
criterion.

**Layer(s) affected:** L2–L10 (read-only validation via replay prefix).

**Files to create/modify:**

- No source code change
- `docs/PROGRESS_REPORT.md` (modify) — append Session History row with the trace IDs
  observed at each layer (L2 feature, L3 edge, L4 probability/slippage/latency, L5
  validated_edge, L6 selection, L7 allocation, L8 execution_result (shadow), L9
  position open/close, L10 learning_record)

**Pre-flight:**

- Confirm `EXECUTION_SHADOW_MODE=true` (or `config/pipeline.yaml execution.mode: "shadow"`)
- Confirm Task 22 deployed and at least one organic `data_quality_event` with
  `RISKY_PASS` has landed in the last hour
- Confirm replay-mode worker scope is enabled (see §7.13)

**Procedure:**

1. Choose a synthetic token with: `creator_count <= 10`, `has_social_links=true`,
   `social_links_known=true`, `total_supply <= 1e9`, `holder_dist_known=true`,
   `holder_count >= 50`
2. Run: `python scripts/inject_test_token.py --chain solana --token <synth_address> --replay`
3. Observe the trace_id propagating across event types within 60 seconds
4. SQL check at each layer:
   ```sql
   SELECT event_type, COUNT(*) FROM events
   WHERE event_id LIKE 'replay:%' AND metadata->>'trace_id' = $1
   GROUP BY event_type ORDER BY event_type;
   ```
   Expect non-zero rows for: `market_data_event`, `data_quality_event`, `feature_event`,
   `edge_event`, `probability_event`, `slippage_event`, `latency_event`,
   `validated_edge_event`, `selection_event`, `allocation_event`,
   `execution_result_event`, `position_state_event`, `learning_record_event`
5. Any layer with 0 rows → file a follow-up plan; do NOT fix in this plan

**Invariant check:**

- [x] `replay:` prefix used on EventID — production workers do not consume it
- [x] No production state mutated
- [x] Idempotent — re-running the script produces no duplicates
- [x] Shadow mode confirmed before run — no on-chain submission

**Validation:**

- All 13 event_types above return ≥ 1 row for the synthetic trace_id
- One `learning_record_event` exists with non-empty `outcome_category`
- PROGRESS_REPORT.md updated with trace_id and per-layer counts

**Prompt context needed:** §7.5 Event bus pattern, §7.13 Replay engine pattern, §7.15 Shadow vs live execution.

---

### Task 25 — Pre-Cohort Filter: Drop Pump.fun Graduates with `creator_count > VERY_EXPLORATION.max` BEFORE DQ ✅ COMPLETED

**Goal:** Reduce wasted DQ + probe work on tokens that will always SKIP/REJECT under
every mode. Per gate review §3: the observed Solana cohort routinely has
`creator_count = 49`, exceeding even VERY_EXPLORATION's `max = 10`. These tokens
currently consume Helius credits, probe budget, and `creator_profile` cache lookups
before being silently dropped. A cheap pre-filter inside the ingestion guard (extends
Task 7) drops them at L0 with a structured `system_event` for observability.

**Layer(s) affected:** L0 ingestion (`internal/modules/ingestion_solana/`).

**Files to create/modify:**

- `internal/modules/ingestion_solana/ingestion_solana.go` (modify) — extend the
  normaliser-level guard added in Task 7:
  - If a `CreatorProfileReader` is available AND `profile.TotalTokens >
cfg.PreFilter.MaxCreatorPrevTokenCount` (NEW config field, default = 25, well above
    VERY_EXPLORATION's 10 so it never overrides DQ mode logic): - Do NOT emit `market_data_event` - Emit `system_event` of type `ingestion_pre_filter_drop` with payload `{token, creator, creator_total_tokens, reason: "creator_above_pre_filter_cap"}`
  - When the reader is unavailable or `known=false`: emit normally (fail-open at L0 — DQ remains the authoritative gate)
- `internal/app/config/ingestion_config.go` (modify or create section) — add:

  ```go
  PreFilter struct {
      Enabled                       bool  `yaml:"enabled"`
      MaxCreatorPrevTokenCount      int32 `yaml:"max_creator_prev_token_count"`
  } `yaml:"pre_filter"`
  ```

  - Defaults: `enabled: false`, `max_creator_prev_token_count: 25`

- `config/chains.yaml` (modify) — under `solana.ingestion:` add:
  ```yaml
  pre_filter:
    enabled: false # opt-in; flip true after Task 24 PIPELINE_PROOF passes
    max_creator_prev_token_count: 25
  ```
- Wiring: orchestrator injects the existing `CreatorProfileReader` (from Task 9) into the ingestion module — module imports `contracts/creator_profile.go` only

**Invariant check:**

- [x] No SQL or DB driver in the ingestion module — uses injected reader
- [x] No cross-module imports — reader interface defined in ingestion module, adapter-backed impl wired by orchestrator
- [x] Threshold from config; `0` or unset = filter disabled
- [x] Fail-open: probe/reader failure does NOT drop tokens — DQ remains authoritative
- [x] Does NOT bypass DQ mandatory hard-rejects — pre-filter only drops a strict super-set of what DQ would reject anyway
- [x] Determinism preserved — same creator + same threshold = same decision
- [x] Disabled by default (`enabled: false`) — operator opt-in after Task 24

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/modules/ingestion_solana/...`: green; add `TestPreFilter_DropsHighCountCreator` and `TestPreFilter_FailsOpenOnUnknownCreator` and `TestPreFilter_DisabledByDefault`
- Post-deploy with `enabled: true` for 24h: Helius credit consumption drops further; `ingestion_pre_filter_drop` count visible in observability; DQ_SKIPPED count drops correspondingly

**Prompt context needed:** §7.1 MarketDataDTO origin, §7.4 Chains config schema, §7.6 creator_profiles schema, §7.20 Pre-filter rationale.

---

### Task 26 — `solana_holder_dist` Probe: Fallback Path When `getTokenLargestAccounts` Times Out ✅ COMPLETED

**Goal:** Per gate review §4 POST_PROFITABILITY_PHASE: the `solana_holder_dist` probe
timing out is the root cause that pins `holder_count_ok=false` and forces SKIP in
EXPLORATION. Adding a fast fallback (`getTokenSupply` + single `getProgramAccounts`
slice) restores `HolderDistKnown=true` for the majority of tokens, which lets us
re-tighten the VERY_EXPLORATION gate in Task 28.

**Layer(s) affected:** L1 probe (`internal/modules/probes/solana_holder_dist.go`).

**Files to create/modify:**

- `internal/modules/probes/solana_holder_dist.go` (modify):
  - Existing path: `getTokenLargestAccounts` with current timeout — UNCHANGED as primary
  - On timeout or RPC error from primary, invoke fallback:
    1. `getTokenSupply` — 1 credit; returns total supply + decimals
    2. `getProgramAccounts` with `filters=[{dataSize: 165}, {memcmp: {offset: 0, bytes: <mint>}}]` — 10 credits; returns up to N token accounts
    3. Sort balances desc; compute `HolderCount` (non-zero accounts) and `Top5HolderPct`
  - On fallback success: set `HolderDistKnown=true`, mark `source="fallback"` in probe telemetry
  - On fallback also failing: leave `HolderDistKnown=false` (existing fail-closed behaviour preserved)
- `internal/app/config/probes_config.go` (modify) — add to `SolanaHolderDistYAML`:
  ```go
  FallbackEnabled              bool  `yaml:"fallback_enabled"`
  FallbackTimeoutMs            int32 `yaml:"fallback_timeout_ms"`
  FallbackMaxProgramAccounts   int32 `yaml:"fallback_max_program_accounts"`
  ```
- `config/data_quality.yaml` (modify — under existing `probes.solana_holder_dist:`):
  ```yaml
  fallback_enabled: true
  fallback_timeout_ms: 2500
  fallback_max_program_accounts: 200
  ```
- Truncate any RPC error message to 200 chars before returning (§7.12 invariant)

**Invariant check:**

- [x] HTTPS-only RPC endpoint preserved (existing Helius client)
- [x] No new API key
- [x] RPC error truncation to 200 chars enforced
- [x] No `io.ReadAll` without `LimitReader` — RPC client already bounds responses
- [x] Fail-closed when both primary AND fallback fail — Phase 3 SKIP behaviour preserved
- [x] No `math/rand` introduced
- [x] Determinism — same mint + same chain state = same `HolderCount` (within RPC eventual consistency)
- [x] Credit budget impact: fallback costs +11 credits per primary-failed token; bounded by primary timeout rate

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/modules/probes/...`: green; add `TestSolanaHolderDist_FallbackOnPrimaryTimeout`, `TestSolanaHolderDist_FailClosedOnBothFail`, `TestSolanaHolderDist_FallbackDisabledByConfig`
- 24h post-deploy observability: `HolderDistKnown=true` rate ≥ 90% (was ~0%); Helius credit usage delta verified within budget

**Prompt context needed:** §7.7 Helius credit reference table, §7.12 Security invariants, §7.20 Pre-filter rationale (sibling — both reduce SKIP rate).

---

### Task 27 — Expose SKIP Decisions to Rescan Eligibility (Bounded, Mode-Gated) ✅ COMPLETED

**Goal:** Per gate review BLOCKER 1 (a): when `holder_count_ok=false` is caused by
`HolderDistKnown=false` (probe timeout) rather than a real quality fail, the token
should be retried on rescan — currently impossible because
[database/engines/postgres/rescan.go](database/engines/postgres/rescan.go#L99) excludes
`SKIP` decisions. Add a mode-gated `include_skipped_for_retry` config flag so the rescan
worker can re-emit `market_data_event` for SKIP'd tokens, AS LONG AS the skip flag
indicates a probe-failure cause (not a real quality fail). After Task 26 lands and
restores `HolderDistKnown=true`, this becomes mostly inert — but it's the cleanest
structural fix for the BLOCKER 1 root cause and harmless if `include_skipped_for_retry: false`.

**Layer(s) affected:** Rescan worker (`database/engines/postgres/rescan.go` +
`internal/workers/run_rescan.go`).

**Files to create/modify:**

- `database/engines/postgres/rescan.go` (modify) — extend the rescan SQL `WHERE` clause:
  ```sql
  AND (
      dq.decision = 'REJECT'
      OR ($7 AND dq.decision IN ('PASS', 'RISKY_PASS'))
      OR ($9 AND dq.decision = 'SKIP'
              AND dq.flags @> '["serial_launcher_skipped"]'::jsonb
              AND COALESCE(md.holder_dist_known, FALSE) = FALSE)
  )
  ```
  Add `$9` as `include_skipped_for_retry bool` parameter wired through the adapter
- `database/adapter.go` (modify) — extend `RescanEligibleQuery` signature with the new bool param
- `internal/workers/run_rescan.go` (modify) — read new config `rescan.include_skipped_for_retry` (default `false`) and pass through
- `config/pipeline.yaml` (modify — under existing `rescan:` block):
  ```yaml
  include_skipped_for_retry: false # opt-in; flip true to retry SKIP'd tokens whose probes timed out
  ```
- Re-emitted EventID stays content-addressable per §7.5 (transport tag = `"rescan_<band>"`) — no duplicate emission risk

**Invariant check:**

- [x] No new event type or DTO — rescan re-emits existing `market_data_event` (per rescan-orchestration skill)
- [x] Content-addressable EventID; `ON CONFLICT DO NOTHING` dedupes
- [x] Pure DB reader + emitter — no RPC, no on-chain calls (rescan invariant preserved)
- [x] STRICT/BALANCED REJECTs not affected — they continue to use existing `dq.decision = 'REJECT'` clause
- [x] Filter narrowly targets probe-failure SKIPs (`serial_launcher_skipped` flag AND `holder_dist_known=FALSE`) — does NOT replay real quality fails
- [x] Disabled by default — operator opt-in
- [x] Open-position skip clause (current `$8`) unchanged

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./database/engines/postgres/...`: green; add `TestRescan_IncludesSkipWhenFlagSetAndProbeFailed`, `TestRescan_ExcludesSkipWhenFlagOff`, `TestRescan_ExcludesSkipWithKnownHolderDist`
- `go test ./internal/workers/...`: green
- 7-day shadow observation: with flag `true`, rescan eligible count increases by N%; downstream `data_quality_event` re-emit count > 0

**Prompt context needed:** §7.2 DataQualityDTO schema, §7.5 Event bus pattern, §7.10 token_lifecycle, §7.18 Gate-review hotfix rationale.

---

### Task 28 ✅ — Re-Tighten VERY_EXPLORATION Quality Gates After Task 26 Restores HolderDistKnown

**Goal:** Reverse the temporary relaxation from Task 22 once Task 26 has restored
`HolderDistKnown=true` for ≥ 90% of tokens. This re-instates the Phase 3 design intent
(quality gates as a real filter) without re-introducing pipeline starvation. Gated on
observed shadow-mode evidence — NOT executed blindly.

**Layer(s) affected:** YAML config + observation runbook.

**Files to create/modify:**

- `config/data_quality.yaml` (modify) — under `mode_profiles.very_exploration:`:
  - Revert `serial_launcher_requires_social_links: false` → `true`
  - Tune `serial_launcher_min_holder_count: 0` → final value (recommend `10`–`25` depending on shadow distribution)
  - Update the HOTFIX comment block to: `# Task 28: HOTFIX reverted YYYY-MM-DD after Task 26 fallback restored HolderDistKnown coverage to N%. Final values calibrated from shadow data.`
- `docs/PROGRESS_REPORT.md` (modify) — append Session History entry citing the shadow-data percentages that justified the chosen values

**Pre-flight gating (ALL must be true before executing this task):**

1. Task 26 deployed and `HolderDistKnown=true` rate ≥ 90% over a rolling 24h window
2. Task 22 hotfix has been live for ≥ 7 days with no critical issues
3. ≥ 100 organic `data_quality_event` rows in VERY_EXPLORATION mode for distribution analysis
4. Shadow-mode `learning_record_event` rows exist that can inform re-tightening (no FN spike expected from re-tightening)

**Invariant check:**

- [x] STRICT/BALANCED still unchanged
- [x] EXPLORATION still unchanged
- [x] Three MANDATORY hard-rejects (serial_launcher in strict modes, no_social_links, high_total_supply) unaffected
- [x] Reversible — flip values back if FN rate spikes
- [x] Config-only — no code change

**Validation:**

- `go build ./...`: green (sanity)
- `go test ./internal/modules/data_quality/...`: green (existing matrix tests cover both relaxed and strict gate values)
- 48h post-deploy observability: `DQ_RISKY_PASSED` and `DQ_SKIPPED` rates within expected envelope (no >50% drop in emit rate); `learning_record_event` rate unchanged

**Prompt context needed:** §7.9 Section-9 implementation details, §7.18 Gate-review hotfix rationale.

---

### Task 29 — Production Readiness: Enable Shadow-Gated Features + Fix L2–L5 Dead Workers + Resolve Duplicate event_ids

**Goal:** Promote the bot from PIPELINE_PROOF to production-ready by (a) fixing the two
BLOCKERs identified in the 2026-06-01 gate review — dead L2–L5 workers and 181 duplicate
`event_id`s — and (b) enabling all shadow-gated features that are safe for live
operation: `bottom_detection`, `dynamic_trailing`, `loss_explainer`,
`reject_unknown_holder_count`, `max_positions_per_creator`, and
`include_skipped_for_retry`. Also uncomments the calibrated market-cap/volume filters
in `data_quality.yaml` now that operational evidence exists to set safe values.

**Layer(s) affected:** L0.5 (rescan), L1 (DQ config), L2 (feature — worker wiring), L3
(edge — worker wiring), L4 (probability — worker wiring), L5 (validation — worker
wiring), L9 (position — dynamic trailing), L10 (learning — loss explainer), Config,
Platform (app startup).

**Files to create/modify:**

- `internal/app/app.go` (modify) — **BLOCKER 2 fix**
  - Audit the worker startup sequence; confirm Feature, Edge, Probability, Validation,
    and Learning workers are registered via `app.RegisterWorker(...)` or equivalent
  - If any worker is missing from the registration list, add it in the correct
    dependency order: Feature → Edge → Probability → Validation → Selection → Capital →
    Execution → Position → Learning
  - No logic change to the workers themselves — this is a wiring-only fix
  - Add a startup log line `"pipeline_workers_registered"` with a count field so future
    gate reviews can confirm all workers are live

- `internal/workers/` (investigate) — **BLOCKER 1 fix: duplicate event_ids**
  - Search every `EmitEvent` / `InsertEvent` call site for any `event_id` constructed
    from a non-content source (timestamp, UUID, counter, `time.Now()`, `rand.*`)
  - If found: replace with `SHA256(content_signature)[:16]` per §7.5 pattern
  - Confirm `ON CONFLICT DO NOTHING` is present on every `events` INSERT
  - If all existing event_ids are already content-addressable, document the finding and
    close as benign (WS-reconnect replay deduplicated at DB level)
  - Expected root cause: the 181 duplicates in the gate report are the same
    `market_data_event` tokens re-delivered by the WebSocket on reconnect — this is
    normal Helius behaviour; `ON CONFLICT DO NOTHING` should silently absorb them.
    Confirm with: `SELECT event_id, COUNT(*) FROM events GROUP BY event_id HAVING COUNT(*) > 1 LIMIT 5;`

- `config/pipeline.yaml` (modify) — **Enable shadow-gated features**
  - `edge.bottom_detection`: set `enabled: true`, `shadow_mode: false`
    — V-shape bottom detection is a profit factor for L3; gating it in shadow mode
    permanently means it never influences live decisions.
  - `position.dynamic_trailing`: set `enabled: true`, `shadow_mode: false`
    — trailing stop tiers protect realized gains; keeping them shadow-only means the
    bot always exits with a fixed SL, leaving upside on the table.
  - `position.dynamic_trailing.tiers` — keep existing tier values unchanged (they are
    conservative: 2x/3x/5x triggers with 20%/15%/10% trail widths).
  - `selection.max_positions_per_creator: 2` — prevents concentration in a single
    creator wallet; `0` (current) means unlimited concurrent positions from one creator.
  - `rescan.include_skipped_for_retry: true` — enables the rescan worker (Task 27) to
    retry probe-timeout SKIP'd tokens; Task 26 fallback now makes this low-cost.
  - `ai_enrichment.loss_explainer.enabled: true` — enables AI-powered loss
    categorisation in Learning (Layer 10); `min_records_per_batch: 5` is already set.

- `config/data_quality.yaml` (modify) — **Enable production-grade DQ gates**
  - `thresholds.reject_unknown_holder_count: true` — now that Task 26's fallback
    restores `HolderDistKnown=true` for ≥ 90% of tokens, fail-open here is a quality
    hole. Flip to `true` to reject tokens whose holder distribution genuinely cannot
    be determined after both primary and fallback probes.
  - Uncomment `min_market_cap_usd: 3000.0` — pump.fun graduation tokens list at ~$69k
    market cap; a $3k floor is safe (well below graduation threshold, rejects only
    sub-$3k micro-cap shells with zero traction). Keep `max_market_cap_usd` commented
    out — the $20k ceiling would reject graduation tokens.
  - Uncomment `min_volume_usd_1h: 100.0` — the guard pattern (`if MinVolumeUsd1h > 0
&& in.VolumeUsd1h > 0 && in.VolumeUsd1h < MinVolumeUsd1h`) means this only fires
    when DEXScreener has populated the field; tokens without 1h data are unaffected.
    $100/hr is a minimal floor that rejects ghost tokens with no real trading activity.

**Invariant check:**

- [x] BLOCKER 2 fix is wiring-only — no module logic changed, no cross-module imports
- [x] BLOCKER 1 fix uses content-addressable `SHA256(content)[:16]` event_ids — determinism preserved
- [x] `ON CONFLICT DO NOTHING` remains on all `events` INSERTs — idempotency preserved
- [x] `bottom_detection` and `dynamic_trailing` are existing implemented features — enabling them does not introduce new code paths, only lifts the shadow gate
- [x] `max_positions_per_creator: 2` is a selection-layer cap, not a DQ gate — does not bypass any Layer-1 hard reject
- [x] `reject_unknown_holder_count: true` requires Task 26 (fallback probe) to already be deployed — pre-flight check enforced below
- [x] `min_market_cap_usd: 3000.0` uses the `> 0 guard` pattern from Task 18 — tokens with `MarketCapUsd == 0` (DEXScreener not yet indexed) are unaffected
- [x] `max_market_cap_usd` stays commented out — graduation tokens at ~$69k would be rejected otherwise
- [x] `loss_explainer` calls `GroqClient.Complete()` — fail-open, no pipeline blocking, `GROQ_API_KEY` must be set in env
- [x] All config values remain in YAML — zero hardcoded thresholds in Go
- [x] Three mandatory Layer-1 hard rejects (serial_launcher STRICT/BALANCED, no_social_links, high_total_supply) unchanged
- [x] No new migrations — all schema already exists from Tasks 5–27
- [x] No Telegram direct API calls — all observability via event bus
- [x] Security invariants preserved: HTTPS-only, API keys via env vars, bounded HTTP bodies

**Pre-flight requirements (ALL must be confirmed before executing):**

1. Task 26 (`solana_holder_dist` fallback) deployed — `HolderDistKnown=true` rate ≥ 90%
2. Task 27 (`include_skipped_for_retry` rescan SQL) deployed and tested
3. Task 28 (VERY_EXPLORATION gate re-tighten) completed
4. `GROQ_API_KEY` env var is set in the deployment environment (required for `loss_explainer`)
5. Confirm last 4h gate brief shows no new structural blockers beyond the two in the 2026-06-01 review

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./internal/app/...`: green — startup worker registration test passes
- `go test ./internal/workers/...`: green
- `go test ./internal/modules/data_quality/...`: green — `reject_unknown_holder_count=true` path covered by existing tests
- Restart bot; within 5 minutes confirm in logs:
  - `"pipeline_workers_registered"` count ≥ 10
  - `features_extracted` events > 0 in `output/logs/gate_*`
  - `edge_decision` events > 0
  - `probability_scored` events > 0
  - `validation_decision` events > 0
- SQL post-deploy spot check:
  ```sql
  SELECT event_type, COUNT(*) FROM events
  WHERE created_at > NOW() - INTERVAL '30 minutes'
  GROUP BY event_type ORDER BY event_type;
  -- Expect: features_extracted > 0, edge_decision > 0, probability_scored > 0,
  --         validation_decision > 0
  ```
- Duplicate event_id audit:
  ```sql
  SELECT COUNT(*) FROM (
    SELECT event_id FROM events GROUP BY event_id HAVING COUNT(*) > 1
  ) dupes;
  -- Expect: 0 (or confirm they are WS-reconnect dedupes absorbed by ON CONFLICT)
  ```
- `docs/PROGRESS_REPORT.md` (modify) — append Session History entry: `Task 29 — Production readiness YYYY-MM-DD: L2–L5 worker wiring confirmed; duplicate event_ids resolved; bottom_detection/dynamic_trailing/loss_explainer enabled; reject_unknown_holder_count=true; min_market_cap_usd=3000/min_volume_usd_1h=100 uncommented; max_positions_per_creator=2; include_skipped_for_retry=true`

**Prompt context needed:** §7.5 Event bus pattern, §7.9 Section-9 serial-launcher, §7.18 Gate-review hotfix rationale, §7.19 Duplicate event_id triage.

---

## 5. Task Summary

| Task | Name                                                                                                        | Files (primary)                                                                                                           | Depends On | Est. Complexity |
| ---- | ----------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | ---------- | --------------- |
| 1    | Fix wrong cost comments (chains.yaml + DAS audit)                                                           | `config/chains.yaml`                                                                                                      | —          | Low             |
| 2    | Chains config struct: add `disabled` flag                                                                   | `internal/app/config/chains_config.go`, `internal/modules/ingestion_solana/`                                              | Task 1     | Low             |
| 3    | Disable raw pump.fun + Raydium V4 transactionSubscribe                                                      | `config/chains.yaml`, `internal/app/config/chains_config.go`, ingestion_solana                                            | Task 2     | Medium          |
| 4    | Runbook: verify pump.fun-AMM events                                                                         | `docs/PROGRESS_REPORT.md` (entry only)                                                                                    | Task 3     | Low             |
| 5    | Migration: `creator_profiles` table                                                                         | `database/migrations/20260101000NNN_creator_profiles.sql`                                                                 | Task 4     | Low             |
| 6    | Contracts additive: SKIP decision + MarketCap/Volume fields                                                 | `contracts/data_quality.go`, `contracts/market_data.go`                                                                   | Task 5     | Low             |
| 7    | Ingestion guard: reject factory-program creator identity                                                    | `internal/modules/ingestion_solana/ingestion_solana.go`                                                                   | Task 6     | Medium          |
| 8    | Creator profile aggregator worker                                                                           | `internal/workers/creator_profile_aggregator.go`, `database/adapter.go`, `contracts/creator_profile.go`                   | Task 7     | High            |
| 9    | DQ uses creator_profiles via injected reader                                                                | `internal/modules/data_quality/data_quality.go`, orchestrator wiring                                                      | Task 8     | High            |
| 10   | Telegram operator command `/devstats` via event bus                                                         | `internal/telegram/dispatcher.go`, `internal/workers/creator_stats_responder.go`                                          | Task 9     | Medium          |
| 11   | Config struct: per-mode serial-launcher fields                                                              | `internal/app/config/data_quality_runtime_config.go`                                                                      | Task 10    | Low             |
| 12   | data_quality.yaml: per-mode profiles                                                                        | `config/data_quality.yaml`                                                                                                | Task 11    | Low             |
| 13   | ProcessForMode: mode-aware serial launcher + buildSkipResult                                                | `internal/modules/data_quality/data_quality.go`, orchestrator                                                             | Task 12    | High            |
| 14   | decision.go: canonicalProfile fallback                                                                      | `internal/modules/data_quality/decision.go`                                                                               | Task 13    | Medium          |
| 15   | Config struct: market-cap/volume threshold fields                                                           | `internal/app/config/data_quality_runtime_config.go`                                                                      | Task 14    | Low             |
| 16   | data_quality.yaml: commented-out market-cap/volume thresholds                                               | `config/data_quality.yaml`                                                                                                | Task 15    | Low             |
| 17   | Expand DEXScreener parser + probe populates MarketDataDTO                                                   | `internal/rpc/price_fetcher.go`, `internal/modules/probes/dexscreener_probe.go`                                           | Task 16    | Medium          |
| 18   | DQ structural rejects: market_cap_too_low/high, volume_too_low                                              | `internal/modules/data_quality/data_quality.go`                                                                           | Task 17    | Medium          |
| 19   | End-to-end pipeline validation: inject test token (replay prefix)                                           | `scripts/inject_test_token.py`, `docs/PROGRESS_REPORT.md` entry                                                           | Task 18    | High            |
| 20   | Enable shadow mode + review min_token_age_seconds for graduation                                            | `config/pipeline.yaml`, `docs/PROGRESS_REPORT.md` entry                                                                   | Task 19    | Medium          |
| 21   | Tests + build/vet/test + PROGRESS_REPORT.md update                                                          | `docs/PROGRESS_REPORT.md`                                                                                                 | Task 20    | Medium          |
| 22   | Phase 6 HOTFIX: relax VERY_EXPLORATION quality gates (SKIP→RISKY_PASS)                                      | `config/data_quality.yaml`                                                                                                | Task 21    | Low             |
| 23   | Verify 106 duplicate event_id is benign WS-replay (diagnose only)                                           | `docs/PROGRESS_REPORT.md` entry (+ follow-up plan if real bug)                                                            | Task 22    | Low             |
| 24   | Re-run inject_test_token.py — confirm L0→L10 → LearningRecordDTO                                            | `docs/PROGRESS_REPORT.md` entry                                                                                           | Task 23    | Medium          |
| 25   | Pre-cohort filter: drop creator_count > 25 before DQ                                                        | `internal/modules/ingestion_solana/ingestion_solana.go`, `internal/app/config/ingestion_config.go`, `config/chains.yaml`  | Task 24    | Medium          |
| 26   | solana_holder_dist fallback: getTokenSupply + getProgramAccounts                                            | `internal/modules/probes/solana_holder_dist.go`, `internal/app/config/probes_config.go`, `config/data_quality.yaml`       | Task 25    | High            |
| 27   | Rescan eligibility: include probe-failed SKIP tokens (mode-gated)                                           | `database/engines/postgres/rescan.go`, `database/adapter.go`, `internal/workers/run_rescan.go`, `config/pipeline.yaml`    | Task 26    | Medium          |
| 28   | Re-tighten VERY_EXPLORATION gates after probe coverage ≥ 90%                                                | `config/data_quality.yaml`, `docs/PROGRESS_REPORT.md` entry                                                               | Task 27    | Low             |
| 29   | Production readiness: fix dead L2–L5 workers, resolve duplicate event_ids, enable all shadow-gated features | `internal/app/app.go`, `internal/workers/`, `config/pipeline.yaml`, `config/data_quality.yaml`, `docs/PROGRESS_REPORT.md` | Task 28    | High            |

---

## 6. How to Use This Plan

1. **Start each task in a fresh chat session** — share this PLAN.md + the relevant §7
   sub-sections listed under "Prompt context needed" for that task
2. **Validate after each task** — run `go build ./...` + `go vet ./...` + `go test ./...`
   before moving to the next task. Fix any issue before proceeding.
3. **Do not skip tasks** — the dependency graph enforces ordering; Phase 3 in particular
   MUST follow Phase 2 (per Section 9 Sequencing Requirement — without wallet-level
   creator identity, the per-mode thresholds of 5/10 are inert for pump.fun tokens)
4. **One task at a time** — do not attempt multiple tasks in a single session, especially
   for High-complexity tasks (8, 9, 13, 19)
5. **Source of truth** — always refer to [docs/PRODUCTION_GATE_ANALYSIS.md](../PRODUCTION_GATE_ANALYSIS.md)
   for exact design decisions. This PLAN.md is the breakdown strategy; the analysis
   document is the specification. Section 9 (mode-aware serial launcher) and Section 10
   (market-cap/volume filters) contain the exact code snippets to paste.
6. **Invariants are non-negotiable** — if an implementation step seems to require
   violating an invariant (e.g., importing a DB driver into a module, calling Telegram
   directly, hardcoding a threshold), stop and flag it for design review. Do NOT work
   around invariants silently. In particular:
   - Phase 3 must NOT bypass the STRICT/BALANCED hard gate (`max_creator_prev_token_count: 1`)
   - Phase 4 thresholds must remain commented out in YAML until shadow-mode data justifies turning them on
   - Phase 1 must NOT switch to `transactionSubscribe` on pumpfun-AMM — only Raydium V4
7. **Update PROGRESS_REPORT.md in Task 21** — this is the sole writable doc file. Add
   a row to Phase Progress and Session History; do NOT modify `architecture.md`,
   `dto_contracts.md`, or any other doc.
8. **Replay scope for Task 19** — when injecting test events, ALWAYS use the `replay:`
   prefix. Forgetting the prefix will cause production workers to consume the synthetic
   event and corrupt live state.
9. **Shadow mode gating** — do NOT advance to live trading from Task 20 until: ≥ 14 days
   of shadow mode AND ≥ 30 completed shadow trades AND positive aggregate expectancy.

---

## 7. Deep Knowledge Reference

This section contains complete schemas, business rules, algorithms, and data flows
needed by each task session. Include the specific §7.N sub-sections listed under
"Prompt context needed" for the task you are implementing.

---

### 7.1 MarketDataDTO Origin (ingestion → DQ)

`MarketDataDTO` is produced by Layer 0 (`internal/modules/ingestion_solana/`) from
on-chain events. For pump.fun families, two normalisers populate `CreatorAddress`:

- `NormalizePumpFunCreateFromLogs` — sets `CreatorAddress = event.User` (real wallet from CreateEvent logs)
- `NormalizePumpFunAMMCreatePool` — sets `CreatorAddress = event.Creator` (graduation pool creator)

The probe `solana_creator_reputation` later populates `CreatorPrevTokenCount` and
`CreatorPrevTokenCountKnown` by querying creator history via Helius RPC. When the
probe fails (timeout, rate limit, API error), `CreatorPrevTokenCountKnown=false` and
DQ fail-closes via `unknown_creator_count`.

**The Pump.fun factory problem (Section 3 Problem B):** if the normaliser path is not
strict, `CreatorAddress` can drift to the program ID `6EF8rrec...` (bonding curve) or
`pAMMBay6...` (AMM) instead of the human wallet. Task 7 adds a hard guard preventing
this drift.

---

### 7.2 DataQualityDTO Schema (`contracts/data_quality.go`)

Canonical decision values (after Task 6 — additive):

```
PASS           — token meets all quality gates; emit data_quality_event with full DTO
RISKY_PASS     — token passes but with elevated risk; capital engine applies smaller allocation
REJECT         — token fails one or more gates; emit data_quality_event; contributes to reject-rate
SKIP (NEW)     — token silently dropped (mode-aware serial launcher); do NOT emit data_quality_event;
                 do NOT contribute to reject-rate; token_lifecycle transitions to `skipped`
```

Required fields preserved by every plan: `TokenAddress`, `Chain`, `Decision`,
`RejectionReasons []string`, `Flags []string`, `Timestamp`, `Version`, plus the four
traceability fields (`TraceID`, `CorrelationID`, `CausationID`, `VersionID`).

**Canonical flag values used by Phases 3 + 4:**

- `serial_launcher_monitored` — emitted with `RISKY_PASS` decision; L7 applies smaller allocation; L9 applies tighter TP1, tighter trailing stop, kill-switch priority
- `serial_launcher_skipped` — emitted with `SKIP` decision; informational only; not used by downstream layers (token is dropped silently)

---

### 7.3 MarketDataDTO Schema After Task 6 (`contracts/market_data.go`)

New additive fields (zero-value-safe, never crash existing consumers):

```go
MarketCapUsd  float64  // DEXScreener marketCap; 0 = not indexed yet → filter inert
VolumeUsd5m   float64  // DEXScreener volume.m5
VolumeUsd1h   float64  // DEXScreener volume.h1
VolumeUsd24h  float64  // DEXScreener volume.h24
```

All four are populated by the Layer-1 DEXScreener probe (Task 17). Position monitoring
(`internal/rpc/price_fetcher.go`) may optionally parse them for observability but does
NOT use them for exit decisions in this plan.

---

### 7.4 Chains Config Schema (`config/chains.yaml` + `internal/app/config/chains_config.go`)

Each entry under `solana.programs[]` has these fields (after Tasks 2 + 3):

| Field                       | Type   | Required | Default           | Purpose                                                                                    |
| --------------------------- | ------ | -------- | ----------------- | ------------------------------------------------------------------------------------------ |
| `program_id`                | string | ✅       | —                 | Solana program account                                                                     |
| `family`                    | string | ✅       | —                 | Logical grouping: `raydium-v4`, `pumpfun`, `pumpfun-amm`, ...                              |
| `disabled` (NEW)            | bool   |          | `false`           | When `true`, no subscription is opened                                                     |
| `subscription_method` (NEW) | string |          | `"logsSubscribe"` | One of: `"logsSubscribe"`, `"transactionSubscribe"`                                        |
| `account_filter` (NEW)      | string |          | `""`              | Required only when `subscription_method == "transactionSubscribe"`; the account to include |

Per Task 3, only Raydium V4 uses `transactionSubscribe` with `account_filter` =
`5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1` (Raydium V4 authority — required signer
for pool creation, not for swaps). Pump.fun-AMM continues to use `logsSubscribe` because
the existing `pumpfun_decode_from_logs` path is well-tuned and graduation volume is low.

---

### 7.5 Event Bus Pattern

All cross-layer communication flows through the PostgreSQL `events` table:

```sql
INSERT INTO events (event_id, event_type, payload, metadata, created_at)
VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
ON CONFLICT (event_id) DO NOTHING;
```

- `event_id` = `SHA256(canonical_payload)[:16]` — content-addressable, idempotent
- Workers consume via:
  ```sql
  SELECT id, event_type, payload, metadata
  FROM events
  WHERE id > (SELECT last_offset FROM consumer_offsets WHERE consumer_name = $1)
  ORDER BY id
  FOR UPDATE SKIP LOCKED
  LIMIT $2;
  ```
- After processing: `UPDATE consumer_offsets SET last_offset = $1 WHERE consumer_name = $2`
- Replay events are isolated by prefix: `event_id = "replay:" + SHA256(content)[:16]` —
  production workers filter them out; replay-mode workers consume them
- **`SKIP` outcome (Phase 3)** does NOT emit a `data_quality_event` — the orchestrator
  drops the token silently and transitions `token_lifecycle` (see §7.10) to `skipped`

---

### 7.6 creator_profiles Schema (Task 5)

```sql
CREATE TABLE creator_profiles (
    chain              TEXT        NOT NULL,
    creator_address    TEXT        NOT NULL,
    total_tokens       BIGINT      NOT NULL DEFAULT 0,
    rug_tokens         BIGINT      NOT NULL DEFAULT 0,
    migrated_tokens    BIGINT      NOT NULL DEFAULT 0,
    golden_gem_tokens  BIGINT      NOT NULL DEFAULT 0,
    win_tokens         BIGINT      NOT NULL DEFAULT 0,
    loss_tokens        BIGINT      NOT NULL DEFAULT 0,
    first_seen_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (chain, creator_address)
);
```

**Update semantics (Task 8 aggregator):**

```sql
INSERT INTO creator_profiles (chain, creator_address, total_tokens, last_seen_at)
VALUES ($1, $2, 1, CURRENT_TIMESTAMP)
ON CONFLICT (chain, creator_address)
DO UPDATE SET
    total_tokens    = creator_profiles.total_tokens + EXCLUDED.total_tokens,
    last_seen_at    = EXCLUDED.last_seen_at,
    last_updated_at = CURRENT_TIMESTAMP;
```

Monotonic counters → idempotent under event-replay (re-emitting the same source event
does not double-count because `event_id` content-addressability prevents re-consumption
in production scope).

**Derived percentages (read-only — Task 10 responder):**

```
rug_pull_pct   = rug_tokens      / total_tokens
migrated_pct   = migrated_tokens / total_tokens
golden_gem_pct = golden_gem_tokens / total_tokens
win_rate       = win_tokens      / total_tokens
```

---

### 7.7 Helius Credit Reference Table (Source: helius.dev/docs/billing/credits, verified 2026-05-20)

| API Method                                | Credits per call / per unit | Notes                                            |
| ----------------------------------------- | --------------------------- | ------------------------------------------------ |
| `getTransaction` (standard RPC)           | 1 credit                    | NOT 100 — that's Enhanced TX API                 |
| `getAccountInfo`                          | 1 credit                    |                                                  |
| `getSignaturesForAddress`                 | 1 credit                    |                                                  |
| `getProgramAccounts`                      | 10 credits                  |                                                  |
| `getAsset` + DAS API                      | 10 credits                  | DAS is 10/call — NOT 1                           |
| `getTransactionsForAddress` (Enhanced TX) | 100 credits                 | Helius-proprietary; bot does NOT use this        |
| `logsSubscribe` (WS)                      | 2 credits per 0.1 MB        | **streaming — every byte counts**                |
| `transactionSubscribe` (WS, Helius ext)   | 2 credits per 0.1 MB        | With `accountInclude`, dramatically lower volume |
| Webhooks (event delivery)                 | 1 credit per event          | Push, not streaming                              |
| Webhook management ops                    | 100 credits per op          | One-time admin cost                              |

**Plan limits:** Free 1M/month, Developer 10M/month ($49, current), Business 100M/month
($499), Professional 200M/month ($999).

---

### 7.8 Section-9 Sequencing Requirement

Per PRODUCTION_GATE_ANALYSIS § 9 "Sequencing requirement":

> P1-A creator attribution (wallet-level identity, not factory-level) must be complete
> before this section is implemented. If creator identity still resolves to the pump.fun
> factory program at 49 launches, the per-mode thresholds of 5 and 10 would not change
> any outcomes for pump.fun tokens (49 > 10 always).

This is why Phase 2 (Tasks 5–10) is strictly before Phase 3 (Tasks 11–14) in the
dependency graph.

---

### 7.9 Section-9 Implementation Details (Mode-Aware Serial Launcher)

**Per-mode thresholds:**

| Mode               | `max_creator_prev_token_count` | Behaviour when exceeded                                                                      |
| ------------------ | ------------------------------ | -------------------------------------------------------------------------------------------- |
| `STRICT`           | `0` (use global: `1`)          | HARD REJECT → log `serial_launcher`                                                          |
| `BALANCED`         | `0` (use global: `1`)          | HARD REJECT → log `serial_launcher`                                                          |
| `EXPLORATION`      | `5`                            | Quality gates → `RISKY_PASS + serial_launcher_monitored` OR `SKIP + serial_launcher_skipped` |
| `VERY_EXPLORATION` | `10`                           | Same as EXPLORATION with looser quality-gate values                                          |

**Quality gates (EXPLORATION / VERY_EXPLORATION only):**

| Gate                                  | EXPLORATION     | VERY_EXPLORATION |
| ------------------------------------- | --------------- | ---------------- |
| `HasSocialLinks` + `SocialLinksKnown` | `true` required | `true` required  |
| `HolderCount`                         | ≥ 50            | ≥ 25             |
| Risk score                            | < 0.40          | < 0.45           |
| Position monitoring active            | required        | required         |

All gates pass → `RISKY_PASS + serial_launcher_monitored`. Any gate fails → `SKIP`.

**SKIP vs REJECT contract:**

- `REJECT` emits `data_quality_event` with rejection reasons; contributes to reject-rate
  stats consumed by Layer 10 learning engine
- `SKIP` emits NO `data_quality_event`; updates `token_lifecycle` to terminal `skipped`
  state; does NOT contribute to reject-rate stats — prevents polluting Layer 10's
  signal with what are effectively "risk-budget-exceeded" tokens, not "bad quality"
  tokens

**Layer 7 / Layer 9 reaction to `serial_launcher_monitored`:**

- L7 Capital Engine: applies smaller allocation (existing `RISKY_PASS` behaviour) — no
  code change needed; the existing capital sizing already attenuates `RISKY_PASS`
- L9 Position Engine: when `serial_launcher_monitored` flag is present, applies tighter
  TP1 (25–30% instead of normal), tighter trailing stop, and kill-switch priority
  ordering. Implementation deferred to a separate follow-up plan IF L9 needs explicit
  changes — Task 13 only emits the flag

---

### 7.10 token_lifecycle State Machine

The `token_lifecycle` table tracks each token through the pipeline via CAS-style
state transitions managed by `internal/modules/state_machine/`. Valid states (relevant
to this plan):

```
created → queued → dq_processing → dq_passed | dq_risky_passed | dq_rejected | dq_skipped (NEW, Task 13)
dq_passed | dq_risky_passed → l2_features → ... → l8_executed → l9_open → l9_closed
```

Adding `dq_skipped` as a terminal state preserves the audit trail for `SKIP` outcomes
even though no `data_quality_event` is emitted to the event bus.

---

### 7.11 Section-10 Implementation Details (Market Cap & Volume Filter)

**Data source:** DEXScreener public API (already integrated). Response shape:

```json
{
  "pairs": [
    {
      "priceNative": "0.00000042",
      "priceUsd": "0.000067",
      "marketCap": 6700,
      "liquidity": { "usd": 15000 },
      "volume": { "m5": 210.5, "h1": 4850.0, "h6": 12300.0, "h24": 18400.0 }
    }
  ]
}
```

**Inertness guards (must be both true to trigger filter):**

```go
if thresholds.MinMarketCapUsd > 0 && in.MarketCapUsd > 0 && in.MarketCapUsd < thresholds.MinMarketCapUsd {
    rejectReasons = append(rejectReasons, "market_cap_too_low")
}
```

The `in.MarketCapUsd > 0` guard means: brand-new tokens not yet indexed by DEXScreener
are NEVER rejected by this filter (they fall through to other checks). Same pattern for
max cap and 1h volume.

**Default values (commented out in YAML):**

| Field                | Starting value | Rationale                                                        |
| -------------------- | -------------- | ---------------------------------------------------------------- |
| `min_market_cap_usd` | 3000.0         | Below $3k = insufficient real capital in the pool                |
| `max_market_cap_usd` | 20000.0        | Above $20k = entry window has passed for most sniping strategies |
| `min_volume_usd_1h`  | 100.0          | Below $100/h = no real trading activity                          |

**Important caveat (from Section 10):** pump.fun graduation tokens transition to
Raydium at ~$69k market cap. Immediately after graduation, market cap may exceed the
$20k max. The thresholds MUST stay commented out until shadow-mode data confirms the
distribution of graduation market caps.

---

### 7.12 Security Invariants Affected by This Plan

These rules MUST be preserved by every task; violation is a blocker:

| Rule                                         | This plan's compliance                                                                       |
| -------------------------------------------- | -------------------------------------------------------------------------------------------- |
| HTTPS-only external URLs                     | No new external endpoint; existing Helius WS / DEXScreener HTTPS unchanged                   |
| API keys via `os.Getenv` only                | `HELIUS_API_KEY` continues to be env-only; NO new env var introduced                         |
| Bounded HTTP bodies (DEXScreener 128 KiB)    | Task 17 expands parser fields only — body cap unchanged                                      |
| RPC error truncation to 200 chars            | No change to error-handling pathways                                                         |
| gRPC auth from env vars only                 | No change; `SOLANA_GRPC_TOKEN` env-only invariant preserved                                  |
| No Telegram direct API calls from modules    | Task 10 strictly uses event-bus dispatcher pattern                                           |
| `io.ReadAll` always wrapped in `LimitReader` | Task 17 modifies an existing parse path that is already inside a `LimitReader` — preserve it |
| No `math/rand` or non-deterministic patterns | Every new code path is deterministic                                                         |

---

### 7.13 Replay Engine Pattern (Task 19)

To re-process historical events without contaminating production state:

```
Production EventID:  SHA256(content)[:16]                         e.g., "a1b2c3d4e5f60718"
Replay EventID:      "replay:" + SHA256(content)[:16]             e.g., "replay:a1b2c3d4e5f60718"
```

Production workers `WHERE event_id NOT LIKE 'replay:%'` — they never see replay events.
Replay workers `WHERE event_id LIKE 'replay:%'` — they only see replay events. Same
table, same schema, deterministic isolation by string prefix.

Task 19 injects a synthetic `market_data_event` with `event_id` carrying the
`replay:` prefix so the production pipeline never sees it. A replay-mode worker
scope consumes it and exercises L2–L10.

---

### 7.14 Operational Modes Quick Reference

Four mutually exclusive modes; bot is in exactly one at any moment:

| Mode               | When entered                         | Threshold profile                                       |
| ------------------ | ------------------------------------ | ------------------------------------------------------- |
| `STRICT`           | Default; rug/FP spike auto-trigger   | Conservative; explore budget ≤ 1%                       |
| `BALANCED`         | Default operating mode               | Standard thresholds                                     |
| `EXPLORATION`      | Starvation recovery (manual or auto) | Relaxed; explore budget 3–5%; Task 12 per-mode override |
| `VERY_EXPLORATION` | Persistent starvation in EXPLORATION | Maximum relaxation; Task 12 per-mode override           |

Transitions are bounded (one transition per window). Manual override via `/mode` is
logged and reversible.

---

### 7.15 Shadow vs Live Execution (Task 20)

`config/pipeline.yaml`:

```yaml
execution:
  mode: "shadow" # generates buy/sell signals; does NOT submit on-chain
  # mode: "live"   # real money, real submissions
```

Execution engine reads `execution.mode` at startup AND on hot-reload. In shadow mode:

- Allocations are computed exactly as in live mode
- An `execution_results` row is INSERTed with `mode = 'shadow'` and a simulated fill
  price derived from the live DEXScreener price at decision time
- `PositionStateDTO` is produced; Layer 9 monitoring loop runs against the simulated
  position
- No wallet keys are ever loaded; no transactions are signed

**Gating before live (per Section 8 Change 6 step 13):** ≥ 30 completed shadow trades
AND positive aggregate `realized_pnl_bps` over the most recent 14-day window.

---

### 7.16 Validation Commands

Run after every task (Task 21 runs the full suite):

```bash
go build ./...                                  # zero errors required
go vet ./...                                    # zero issues required
go test ./...                                   # all packages green
go test -race ./...                             # race detector clean (Task 8 worker)
```

Pre-deploy invariant scans (Task 21):

```bash
grep -rn "math/rand" internal/                  # no new occurrences in modified files
grep -rn "io.ReadAll" internal/ | grep -v LimitReader
grep -rn "INSERT OR IGNORE" database/migrations/
grep -rn "BIRDEYE_API_KEY\|HELIUS_API_KEY\|JITO_BUNDLE_URL\|TWITTER_BEARER_TOKEN" config/
```

Post-deploy verification queries are inlined in Tasks 3, 4, 9, 18, 20 and in
Section 9 / Section 10 of PRODUCTION_GATE_ANALYSIS.md.

---

### 7.17 Definition of Done

This plan is complete only when ALL of the following are true:

1. ✅ Phase 1: Helius Dashboard credit consumption dropped ≥ 50% within 2 hours of Task 3 deploy
2. ✅ Phase 1: `pumpfun` family event count = 0 over 1-hour window post-deploy
3. ✅ Phase 1: `raydium-v4` family event count > 0 within seconds of any new pool
4. ✅ Phase 2: `creator_profiles` table populated; at least one creator has `total_tokens > 1`
5. ✅ Phase 2: `/devstats <creator>` Telegram command returns formatted stats
6. ✅ Phase 3: Mode-aware DQ test matrix passes (4 modes × known/unknown × pass/fail = 16 cases)
7. ✅ Phase 3: At least one token in EXPLORATION mode receives `RISKY_PASS + serial_launcher_monitored` AND at least one receives `SKIP + serial_launcher_skipped` over a 7-day observation
8. ✅ Phase 4: DEXScreener parser captures market cap + volume on every successful probe call
9. ✅ Phase 4: Market-cap and volume thresholds remain commented out in YAML (operator opt-in)
10. ✅ Phase 5: Test token injection (Task 19) produces non-empty L2 + L3 + L5 + L7 events
11. ✅ Phase 5: Shadow mode running for ≥ 14 days with ≥ 30 completed paper trades
12. ✅ Task 21: `go build` + `go vet` + `go test -race` all green
13. ✅ Task 21: `docs/PROGRESS_REPORT.md` updated with phase outcomes and shadow-mode start date
14. ✅ All invariant scans return zero violations
15. ✅ Phase 6 (Task 22): VERY_EXPLORATION emits `data_quality_event` with `RISKY_PASS` within 5 minutes of deploy
16. ✅ Phase 6 (Task 23): All sampled duplicate event_ids confirmed deduped (`occurrences = 1`) OR follow-up plan filed
17. ✅ Phase 6 (Task 24): Synthetic test token traces L0→L10 and produces ≥ 1 `learning_record_event`
18. ✅ Phase 6 (Task 26): `HolderDistKnown=true` rate ≥ 90% over 24h with fallback enabled
19. ✅ Phase 6 (Task 28): VERY_EXPLORATION gates re-tightened with shadow-justified values; no FN spike observed in following 48h

When all 19 are true, this plan is **Completed** and the bot is ready for the
micro-capital live-trading decision (separate plan, gated on positive shadow expectancy).

---

### 7.18 Gate-Review Hotfix Rationale (Tasks 22, 23, 27, 28)

**Source of finding:** Auto gate review brief `output/logs/gate_brief_20260531_*.txt`
identifying two BLOCKER findings:

1. **100% DQ_SKIPPED storm:** Every Solana pump.fun graduation produces
   `social_links_ok=false` (no metadata URI on raw graduations), `holder_count_ok=false`
   (`solana_holder_dist` probe systematically times out at Helius
   `getTokenLargestAccounts`), and `CreatorPrevTokenCountKnown=false` (new creators
   not yet aggregated into `creator_profiles`). All three trigger the Phase 3
   `buildSkipResult` path → `data_quality_event` is NOT emitted → L2..L10 starvation.
2. **Rescan worker emits 0 events:** Downstream consequence of (1). The rescan SQL
   in [database/engines/postgres/rescan.go](../../database/engines/postgres/rescan.go#L99)
   explicitly excludes `SKIP` decisions from the eligibility set.

**Architectural correctness vs operational reality.** Phase 3's SKIP semantics are
architecturally correct (see §7.9): SKIP means "we lack evidence; do not consume
downstream budget on this token". But when probe failures are systemic (Helius
free-tier `getTokenLargestAccounts` reliability for pump.fun mints in the first
60 seconds of life), every token looks like "missing evidence". The pipeline cannot
distinguish a real quality fail from a transient probe failure.

**Why Task 22 (relaxation) before Task 26 (probe fix):** Task 22 is a config-only
change that takes effect on restart; Task 26 is a probe code change with a 24h
observation window. Doing Task 22 first unblocks the L2..L10 PIPELINE_PROOF
needed before any other Phase 6 work can be validated. Task 28 reverses the
relaxation after Task 26 ships and observability proves the probe fallback works.

**Why not bypass MANDATORY hard-rejects.** The three MANDATORY hard-rejects
(serial*launcher in STRICT/BALANCED, no_social_links, max_total_supply>1B) are
architecture invariants that cannot be relaxed under any operational mode. Task 22
only flips the \_secondary* serial-launcher quality gate in VERY_EXPLORATION
(`requires_social_links` and `min_holder_count`) — these are mode-profile
overrides, NOT the mandatory hard-rejects. STRICT and BALANCED tokens with
`creator_count >= 1` still REJECT (not SKIP) via the global threshold.

**Why Task 27 separately exposes SKIPs to rescan:** Even with Task 22, some
narrowly-scoped SKIP paths (e.g., probe failure on holder distribution while social
links pass and creator count is unknown) will still produce SKIP. Task 27 lets
the rescan worker retry these on the standard age bands (15m..48h), gated on
`include_skipped_for_retry: false` by default — operator opt-in.

---

### 7.19 Duplicate event_id Triage (Task 23)

**Expected source of duplicates:** WebSocket subscription reconnects in
`internal/modules/ingestion_solana/ws_session.go` cause the upstream Helius/QuickNode
stream to replay the last N events on reconnect. Each replayed event computes the
same content-addressable `event_id = SHA256(content)[:16]` per §7.5 and is correctly
rejected at the DB layer by `ON CONFLICT DO NOTHING` on the `events(event_id)`
primary key.

**Triage decision tree:**

```
For each duplicate event_id in the auto-checker report:
  SELECT COUNT(*) FROM events WHERE event_id = $1;
  ├── = 1 → benign (DB deduped; auto-checker counted INSERT attempts from logs, not rows)
  ├── > 1 → REAL BUG: unique constraint violated. Investigate event_id derivation
  │         in the producer; file follow-up plan; STOP work on Task 23
  └── = 0 → schema inconsistency: event_id appeared in logs but never landed.
            Likely INSERT failed silently. Check `system_event` of type
            `event_emit_failed` and adapter error logs.
```

**Producer audit checklist (only required if any duplicate has `COUNT > 1`):**

| Producer location                                 | EventID derivation                 | OK?                                 |
| ------------------------------------------------- | ---------------------------------- | ----------------------------------- |
| `internal/modules/ingestion_solana/normaliser.go` | SHA256(canonical JSON)             | ✅                                  |
| `internal/workers/run_market_probes.go`           | SHA256(content)                    | ✅                                  |
| `internal/workers/run_data_quality.go`            | SHA256(content)                    | ✅                                  |
| `internal/workers/run_rescan.go`                  | SHA256(chain+token+band+bucket_ts) | ✅ (per rescan-orchestration skill) |
| `internal/workers/run_features.go`                | SHA256(content)                    | ✅                                  |
| ... (any other emitter)                           | SHA256(content)                    | ✅                                  |

If any producer derives event_id from `time.Now()`, a counter, or `uuid` — that is
the bug. File `docs/plans/YYYY-MM-DD-event-id-determinism-plan.md` and stop work
on Task 23.

---

### 7.20 Pre-Filter and Probe Fallback Rationale (Tasks 25, 26)

**Task 25 — Pre-cohort filter:** The gate review §3 observed that the routinely
incoming pump.fun graduate cohort has `creator_count = 49`, exceeding even
VERY_EXPLORATION's `max_creator_prev_token_count = 10`. These tokens consume:

- 1 Helius credit for the `transactionSubscribe` notification (cannot be avoided)
- 1–20 Helius credits per L1 probe call
- 1 `creator_profiles` cache lookup
- 1 DQ evaluation + `buildSkipResult` call
- 1 `system_event` write

…only to terminate in `dq_skipped` lifecycle. A pre-DQ filter at L0 saves the L1
probe budget and the DQ compute cycle. Threshold `25` is intentionally above
VERY*EXPLORATION's `10` so this filter never \_overrides* a DQ mode decision —
it only drops the strict super-set that would always SKIP/REJECT under every mode.

**Task 26 — Probe fallback:** `getTokenLargestAccounts` on Helius for pump.fun
mints in the first 60s of life is unreliable (timeouts observed in production).
Fallback path:

| Call                 | Cost       | Returns                                                             |
| -------------------- | ---------- | ------------------------------------------------------------------- |
| `getTokenSupply`     | 1 credit   | `total_supply`, `decimals`                                          |
| `getProgramAccounts` | 10 credits | up to N token accounts (bounded by `fallback_max_program_accounts`) |

Total fallback cost: **+11 credits** per primary-failed token. With Task 25 dropping
the high-creator-count cohort, the population subject to fallback is bounded.
Expected steady-state: < 5% of tokens require fallback; budget impact negligible.

**Determinism note:** `getProgramAccounts` returns are eventually consistent across
Solana RPC providers; two consecutive calls may return slightly different account
sets. This is acceptable for an L1 probe — DQ thresholds are coarse-grained
(holder count buckets), not exact-equality checks. The deterministic post-condition
is: `same on-chain state + same chain reorgs → same HolderCount bucket`.

---
