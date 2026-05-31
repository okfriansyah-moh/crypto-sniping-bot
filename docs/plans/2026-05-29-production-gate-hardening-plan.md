# PLAN.md — Production Gate Hardening (Credit Burn + Creator Attribution + Mode-Aware DQ + Market-Cap Filters + Shadow Trading)

> **Version:** 1.0
> **Date:** 2026-05-29
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

## 5. Task Summary

| Task | Name                                                              | Files (primary)                                                                                         | Depends On | Est. Complexity |
| ---- | ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- | ---------- | --------------- |
| 1    | Fix wrong cost comments (chains.yaml + DAS audit)                 | `config/chains.yaml`                                                                                    | —          | Low             |
| 2    | Chains config struct: add `disabled` flag                         | `internal/app/config/chains_config.go`, `internal/modules/ingestion_solana/`                            | Task 1     | Low             |
| 3    | Disable raw pump.fun + Raydium V4 transactionSubscribe            | `config/chains.yaml`, `internal/app/config/chains_config.go`, ingestion_solana                          | Task 2     | Medium          |
| 4    | Runbook: verify pump.fun-AMM events                               | `docs/PROGRESS_REPORT.md` (entry only)                                                                  | Task 3     | Low             |
| 5    | Migration: `creator_profiles` table                               | `database/migrations/20260101000NNN_creator_profiles.sql`                                               | Task 4     | Low             |
| 6    | Contracts additive: SKIP decision + MarketCap/Volume fields       | `contracts/data_quality.go`, `contracts/market_data.go`                                                 | Task 5     | Low             |
| 7    | Ingestion guard: reject factory-program creator identity          | `internal/modules/ingestion_solana/ingestion_solana.go`                                                 | Task 6     | Medium          |
| 8    | Creator profile aggregator worker                                 | `internal/workers/creator_profile_aggregator.go`, `database/adapter.go`, `contracts/creator_profile.go` | Task 7     | High            |
| 9    | DQ uses creator_profiles via injected reader                      | `internal/modules/data_quality/data_quality.go`, orchestrator wiring                                    | Task 8     | High            |
| 10   | Telegram operator command `/devstats` via event bus               | `internal/telegram/dispatcher.go`, `internal/workers/creator_stats_responder.go`                        | Task 9     | Medium          |
| 11   | Config struct: per-mode serial-launcher fields                    | `internal/app/config/data_quality_runtime_config.go`                                                    | Task 10    | Low             |
| 12   | data_quality.yaml: per-mode profiles                              | `config/data_quality.yaml`                                                                              | Task 11    | Low             |
| 13   | ProcessForMode: mode-aware serial launcher + buildSkipResult      | `internal/modules/data_quality/data_quality.go`, orchestrator                                           | Task 12    | High            |
| 14   | decision.go: canonicalProfile fallback                            | `internal/modules/data_quality/decision.go`                                                             | Task 13    | Medium          |
| 15   | Config struct: market-cap/volume threshold fields                 | `internal/app/config/data_quality_runtime_config.go`                                                    | Task 14    | Low             |
| 16   | data_quality.yaml: commented-out market-cap/volume thresholds     | `config/data_quality.yaml`                                                                              | Task 15    | Low             |
| 17   | Expand DEXScreener parser + probe populates MarketDataDTO         | `internal/rpc/price_fetcher.go`, `internal/modules/probes/dexscreener_probe.go`                         | Task 16    | Medium          |
| 18   | DQ structural rejects: market_cap_too_low/high, volume_too_low    | `internal/modules/data_quality/data_quality.go`                                                         | Task 17    | Medium          |
| 19   | End-to-end pipeline validation: inject test token (replay prefix) | `scripts/inject_test_token.py`, `docs/PROGRESS_REPORT.md` entry                                         | Task 18    | High            |
| 20   | Enable shadow mode + review min_token_age_seconds for graduation  | `config/pipeline.yaml`, `docs/PROGRESS_REPORT.md` entry                                                 | Task 19    | Medium          |
| 21   | Tests + build/vet/test + PROGRESS_REPORT.md update                | `docs/PROGRESS_REPORT.md`                                                                               | Task 20    | Medium          |

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

When all 14 are true, this plan is **Completed** and the bot is ready for the
micro-capital live-trading decision (separate plan, gated on positive shadow expectancy).
