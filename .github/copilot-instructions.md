# Skeleton Parallel — Copilot Instructions

> These instructions enforce the architectural constraints for any project built on this framework.
> Violations are not acceptable and must not be introduced, even partially.

---

## Reference Documents

| Document                         | Purpose                                                                                                                                                                           |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/architecture.md`           | **Single source of truth.** Unified architecture — control system, 10-layer pipeline, backbone, meta systems, KPIs, operational modes. All other docs must be consistent with it. |
| `docs/implementation_roadmap.md` | Phase-based implementation roadmap with schemas, algorithms, exit criteria, priority layers                                                                                       |
| `docs/orchestrator_spec.md`      | Orchestrator specification — execution model, checkpointing, resume, idempotency, failure handling                                                                                |
| `docs/dto_contracts.md`          | DTO definitions with all fields/types/constraints, cross-module dependency matrix, validation rules                                                                               |
| `docs/db_adapter_spec.md`        | Database abstraction layer — adapter interface, SQL compatibility, migration strategy, engine portability                                                                         |
| `docs/PARALLEL_DEV.md`           | Parallel development orchestration guide — 3-mode execution system, phase grouping, token optimization                                                                            |
| `docs/AGENTS_AND_SKILLS.md`      | Agent/skill system — agents, skills, composition matrices, token optimization, parallel dev integration                                                                           |
| `docs/STARTER_GUIDE.md`          | Getting started playbook — setup, architecture generation, roadmap generation, parallel system usage                                                                              |
| `docs/PROGRESS_REPORT.md`        | Implementation status — completed work, test results, remaining items, phase-by-phase progress tracking                                                                           |
| `contracts/`                     | Immutable DTO definitions — all modules MUST use these, not upstream sources or raw dicts/objects                                                                                 |
| `config/`                        | YAML configuration files — all thresholds, paths, and tunable parameters live here                                                                                                |

When generating code, refer to these documents for exact schemas, DTO definitions, interfaces, and algorithms. Do not invent new structures that contradict them.

---

## Architecture Invariants

### Modular Monolith

- Single process, single repo, single database
- Entry point: `app/main.*` (language-specific, e.g., `main.py`, `main.ts`, `main.go`)
- No microservices, no inter-process communication, no network calls between modules

### Module Communication

- Modules communicate **only** through immutable DTO types defined in `contracts/`
- No direct imports between module internals — only public contracts
- No raw dicts/maps/objects, no untyped data crossing module boundaries
- See `docs/dto_contracts.md` for DTO definitions and validation rules

### Pipeline Architecture

Stages execute in **strict sequential order** — never reorder, skip, or parallelize stages at runtime.

**Canonical pipeline** (per `docs/architecture.md` § 1):

```
DETECT → FILTER → SCORE → SELECT → EXECUTE → EXIT → EVALUATE → ADJUST
```

Mapped to the 10 layers (`docs/architecture.md` § 3):

```
Layer 1  Data Quality Engine        (reject manipulation, honeypots, rugs)
Layer 2  Feature Extraction         (normalized FeatureDTO + FeatureConfidence)
Layer 3  Signal & Edge Discovery    (NEW_LAUNCH_EDGE, adaptive momentum threshold)
Layer 4  Probability/Slippage/Latency Models
Layer 5  Edge Validation            (EV gate, adaptive thresholds)
Layer 6  Selection Engine           (Top-K greedy + diversity + exploration band)
Layer 7  Capital Engine             (size ∝ Score × P × Confidence, cohort multipliers)
Layer 8  Execution Engine           (wallet sharding, prebuilt calldata, bounded parallelism)
Layer 9  Position Engine            (TP1/TP2/SL/TIME, adaptive per cohort)
Layer 10 Learning Engine            (FP/FN, cohort analysis, bounded updates)
```

### Core Invariant (do not violate)

```
Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality
```

If any factor → 0, profit → 0. Every change must preserve every factor.

### Determinism

- Same input + same config = identical output. Always.
- No `random`, no non-deterministic model inference, no network-dependent behavior
- All IDs are content-addressable (derived from content, not timestamps or random values)

### Idempotency

- Running the pipeline twice on the same input produces no duplicates and no corruption
- All IDs are content-addressable:
  - `entity_id = SHA256(content_signature)[:16]`
- All SQL uses portable `INSERT ... ON CONFLICT DO NOTHING` semantics

### State Authority

- **The database is the single source of truth** for all pipeline state
- Define tables per domain in `docs/architecture.md`
- Pipeline run states: `started → processing → completed | partial | failed`
- Entity states: `created → queued → processed → completed | failed`
- No in-memory-only state that isn't backed by the database

### Database Adapter

- **All database access goes through `database/adapter.*`** — the single entry point
- Modules under `app/modules/` **MUST NOT** import any database driver directly
- Modules **MUST NOT** contain SQL strings or execute queries
- The adapter accepts and returns immutable DTOs — no raw rows, no dicts/maps
- Only the orchestrator calls the adapter — modules never touch the database
- All SQL uses portable syntax (`ON CONFLICT DO NOTHING`, not `INSERT OR IGNORE`)
- See `docs/db_adapter_spec.md` for the full adapter interface and migration strategy

### Orchestrator Rules

- The orchestrator is the **only** component that calls modules — modules never call each other
- Checkpoint after every stage completion (write to database)
- Resume from last successful checkpoint on restart
- See `docs/orchestrator_spec.md` for the full execution model

### Orchestrator Authority Rule

The orchestrator is the **ONLY** component that:

- Calls modules (modules never call each other)
- Manages execution order (the pipeline stage sequence)
- Performs checkpointing (writes `last_completed_stage` after each stage)
- Writes to the database (via `database/adapter.*`)
- Routes DTOs between modules (passes output of stage N as input to stage N+1)
- Handles failures (decides retry, skip, or abort)

Modules MUST:

- Be **pure functions** — accept DTOs, return DTOs, no side effects on shared state
- **Not call the database** — no imports from `database/`, no SQL, no adapter calls
- **Not call other modules** — no imports from other modules (only `contracts/`)
- **Not manage their own state** — all state lives in the database, managed by the orchestrator
- **Not perform checkpointing** — only the orchestrator decides when to persist progress

---

## Sniper-Specific Architecture Invariants

These rules extend the skeleton-parallel framework with the specific architecture defined in `docs/architecture.md`. All code generation MUST comply.

### Event-Sourced Backbone (per `docs/architecture.md` § 2)

- **Append-only event bus** in Postgres (`events` table) is the authoritative log of all DTO transitions
- Modules **publish events** (INSERT); they never mutate past events
- **Workers consume via `SELECT ... FOR UPDATE SKIP LOCKED`** with `consumer_offsets` tracking — no polling queues, no in-memory queues
- **Full state is reconstructible** from the event log alone (replay guarantee)
- See `docs/architecture.md` § 2.2–2.3 for SQL and worker loop

### Per-Market Isolation (per `docs/architecture.md` § 2.4)

- The pipeline runs **one independent instance per market** (`eth-uniswap-v2`, `bsc-pancake-v2`, etc.)
- No cross-market coupling — each market has isolated configs, workers, checkpoints
- Horizontal scalability = add more market workers; no shared mutable state

### Telegram via Event Bus Only (per `docs/architecture.md` § 2.5, § 4.4)

- Modules **MUST NOT** call Telegram APIs directly
- All user-facing events emit to `events` → dedicated **Telegram dispatcher service** reads from the bus and sends messages
- Operator commands (`/status`, `/mode`, `/pnl`, `/positions`, `/kill`, `/resume`, `/version`) are logged and require confirmation for destructive actions
- No remote code execution via Telegram — ever

### Strategy Versioning & Replay (per `docs/architecture.md` § 4.1–4.2)

- Every configuration update creates an **immutable `StrategyVersion`** — thresholds, feature weights, model params, cohort multipliers
- Every trade logs `strategy_version_id` for attribution
- **A/B promotion is bounded**: promote only if `expectancy(V2) > expectancy(V1) × 1.05` AND `drawdown(V2) ≤ drawdown(V1)` AND `N ≥ 30–50` samples
- **Replay must be bit-for-bit deterministic**: no wall-clock dependencies, no randomness, no external nondeterministic calls — use event timestamps only

### Operational Modes (per `docs/architecture.md` § 7)

The system runs in exactly one of four modes at any time:

- `STRICT` — conservative thresholds, low explore budget (≤1%)
- `BALANCED` — default operating mode
- `EXPLORATION` — relaxed thresholds, higher explore budget (3–5%), used for starvation recovery
- `VERY_EXPLORATION` — maximum relaxation; auto-entered when starvation persists in EXPLORATION

Mode transitions are **bounded**: one transition per window, auto-downgrade on starvation, auto-upgrade on rug/FP spike, manual override via `/mode` (logged, reversible). Values live in `config/` YAML.

### Learning Safety (per `docs/architecture.md` § 3.10.12, § 5.3)

All adaptive updates are non-negotiably:

- **Bounded** — `Δparameter ≤ 5–10% per cycle`
- **Sample-gated** — require `N ≥ 30–50` before update
- **Versioned** — every change bumps `config_version` with snapshot
- **Rollback-able** — revert if performance degrades
- **Single-family per cycle** — never tune multiple parameter families simultaneously (prevents oscillation)
- **Must store rejected shadow trades** in `LearningRecord` — without them false negatives cannot be computed

### Execution Engine Rules (per `docs/architecture.md` § 3.8)

- **Wallet sharding is mandatory** — `hash(TokenAddress) % n` or round-robin; one in-flight tx per wallet; strictly increasing nonce per wallet
- **Prebuilt calldata** on hot path — no recomputation during submission
- **Bounded parallelism** — global semaphore, concurrency_limit ∈ [5, 20], adaptive on failure rate
- **Idempotency keys** — each `AllocationDTO` has unique `execution_id`; duplicate submissions are dropped
- **Multi-endpoint RPC fallback** with circuit breaker; fee bumps on stuck tx use same nonce, δ ≈ 10–20%, max 2–3 retries

### Security Invariants (enforced, never relax)

- **HTTPS only for Jito bundle URLs** — `NewJitoClient` rejects any non-HTTPS URL unless `shadow_mode: true` or the URL is a loopback address (`http://127.` / `http://localhost`) for test servers. Never disable this check in production code.
- **Chain allowlist for DEXScreener** — `CopyTradeProvider` accepts only `ethereum`/`eth`, `bsc`/`bnb`, `solana`/`sol`, `base`. Unknown chains return an error (fail-closed). No passthrough allowed.
- **gRPC auth tokens from env vars only** — `SOLANA_GRPC_TOKEN` is read exclusively via `os.Getenv`. The field `GrpcAuthToken` is intentionally absent from `TransportConfig`, `IngestionTransportConfig`, and `config/chains.yaml`. Never add it back.
- **API keys never in YAML** — all external API keys (`BIRDEYE_API_KEY`, `TWITTER_BEARER_TOKEN`, `COPY_TRADE_WALLETS`, `JITO_BUNDLE_URL`, `JITO_TIP_ACCOUNT`, etc.) are read via `os.Getenv` at constructor only. Never log, never config-file.
- **Response bodies are bounded** — Jito HTTP response: 64 KiB cap. DEXScreener copy-trade: 128 KiB cap. Never use `io.ReadAll` without a `LimitReader`.
- **RPC error messages are truncated** — `truncate(msg, 200)` before surfacing in returned errors or logs. Never expose raw RPC error strings of arbitrary length.

### DTO Contract Rules (per `docs/architecture.md` § 2.1, § 4.5)

The canonical DTO registry — no ad-hoc types allowed:

```
DataQualityDTO, FeatureDTO, FeatureConfidence, EdgeDTO,
ProbabilityEstimateDTO, SlippageEstimateDTO, LatencyProfileDTO,
ValidatedEdgeDTO, SelectionOutput, AllocationDTO,
ExecutionResultDTO, PositionState, LearningRecord,
StrategyConfig, StrategyVersion
```

All DTOs: immutable, versioned (`Version` field), schema-validated, `Timestamp` field required, no untyped payloads.

---

## Forbidden Technologies

Do not introduce any of these unless the project explicitly requires them:

| Category     | Default Forbidden                                                                                            | Override             |
| ------------ | ------------------------------------------------------------------------------------------------------------ | -------------------- |
| Architecture | Microservices, Kafka, RabbitMQ, Kubernetes, Docker orchestration                                             | Unless project needs |
| Databases    | MongoDB, Redis, any distributed database                                                                     | Unless project needs |
| AI/ML        | OpenAI API, Anthropic API, LangChain, AutoGPT, CrewAI, any paid LLM                                          | Unless project needs |
| Cloud        | AWS, GCP, Azure, any cloud compute or storage                                                                | Unless project needs |
| Runtime      | Agent loops, autonomous planners, async message brokers (Kafka/RabbitMQ/NATS) outside the Postgres event bus | Unless project needs |

> **Override policy:** If your project legitimately requires a forbidden technology (e.g., Redis for caching, Docker for deployment, OpenAI for an LLM-powered feature), document the justification in `docs/architecture.md` and proceed. The defaults exist to prevent accidental complexity, not to block valid requirements.

### Database Engine Policy

- **The database engine is project-specific.** Choose the appropriate engine when setting up a new project.
- **Supported engines are configured via `database/adapter.*`.** See `docs/db_adapter_spec.md`.
- **Modules MUST remain database-agnostic.** No module may reference any specific database engine.
- Direct use of any database driver in `app/modules/` is forbidden.
- The adapter is the **sole abstraction boundary** — switching engines requires changes only in `database/`.

---

## Repository Structure

```
skeleton-parallel/
├── app/
│   ├── main.*               # Single entry point (language-specific)
│   ├── modules/             # Domain modules (one package per stage)
│   │   ├── module_a/
│   │   ├── module_b/
│   │   └── ...
│   └── orchestrator/        # Pipeline orchestration + checkpointing
├── contracts/               # Immutable DTO definitions
├── database/                # DB adapter + engine implementations + migrations
├── config/                  # YAML configuration
├── tests/                   # Unit + integration tests
├── output/                  # Generated artifacts (gitignored)
├── docs/                    # Architecture + specs
├── scripts/                 # Automation scripts
└── .github/                 # Agent + skill + prompt definitions
```

**Placement rules:**

- New module logic goes in the appropriate `app/modules/` subdirectory
- New DTO definitions go in `contracts/` — never duplicate in a module
- Database migrations go in `database/migrations/`
- Tests mirror the `app/modules/` structure under `tests/`
- Configuration defaults go in `config/` YAML files — never hardcode
- Never put module-specific logic in `app/orchestrator/` or `contracts/`

---

## Development Rules

1. **Language & runtime** — Use the project's chosen language and version. Use type annotations on all public interfaces
2. **Immutable DTOs** for all contracts — no mutable state crossing module boundaries
3. **Each module** gets its own package under `app/modules/` with a public entry point exposing only the public contract
4. **No module may import another module's internals** — only `contracts/` types
5. **Database access** through `database/adapter.*` only — no raw SQL in modules, no ORM
6. **Tests** must be runnable without GPU, without network, and without real data files
7. **Config** via YAML files — no hardcoded paths, thresholds, or magic numbers
8. **Logging** via structured logging (language-appropriate library) — leveled, no unstructured console output

---

## File Duplication Prevention

**MUST NOT:**

- Create duplicate files with similar names (e.g., `utils.py` and `helpers.py` with overlapping functions)
- Create new utility modules when existing ones already cover the functionality
- Duplicate DTO definitions — all DTOs live in `contracts/` and are defined exactly once
- Copy SQL schemas between migration files — reference the existing table, don't redefine it
- Duplicate configuration defaults — all defaults live in `config.yaml`, not scattered in code
- Create wrapper modules that simply re-export another module's functions

**MUST:**

- Check existing files before creating new ones — use the project structure as the source of truth
- Reuse existing utility functions from `contracts/`, `core/`, and shared helpers
- Place new code in the correct existing module rather than creating a parallel file
- When adding a new module, verify no existing module already handles that responsibility
- Keep one canonical location for each piece of logic — no copies, no forks, no alternatives

---

## Documentation Section Duplication Prevention

Each concept or specification MUST have **one canonical location** across all `docs/` files. When multiple documents need to reference the same concept, use cross-references instead of duplicating content.

**MUST NOT:**

- Duplicate section content across `docs/` files — if two documents describe the same thing, one must cross-reference the other
- Repeat examples, tables, or ASCII diagrams that already exist in another document section
- Create parallel sections with overlapping scope (e.g., two "Status Display" sections covering the same output)
- Copy state definitions, pipeline stages, or agent pipeline descriptions across documents verbatim

**MUST:**

- Identify the **canonical document** for each concept using the Reference Documents table above
- Use cross-references: "See `docs/orchestrator_spec.md` § Failure Handling" instead of restating the rules
- When a document needs summarized context from another, keep it to a one-line summary + cross-reference
- Before adding a new section to any `docs/` file, verify no existing document already covers that topic
- Each document owns its domain — `architecture.md` owns system design, `orchestrator_spec.md` owns execution model, `PARALLEL_DEV.md` owns parallel development, etc.

**Canonical ownership:**

| Topic                        | Canonical Document               |
| ---------------------------- | -------------------------------- |
| System architecture & design | `docs/architecture.md`           |
| Pipeline execution model     | `docs/orchestrator_spec.md`      |
| DTO definitions & rules      | `docs/dto_contracts.md`          |
| Database adapter interface   | `docs/db_adapter_spec.md`        |
| Parallel development         | `docs/PARALLEL_DEV.md`           |
| Agent/skill system           | `docs/AGENTS_AND_SKILLS.md`      |
| Implementation phases        | `docs/implementation_roadmap.md` |
| Getting started              | `docs/STARTER_GUIDE.md`          |
| Progress tracking            | `docs/PROGRESS_REPORT.md`        |

---

## File Naming Standards

Every source file name must describe the functionality it contains — not a task code, sprint ticket, phase label, or internal shorthand.

**MUST NOT:**

- Use opaque task codes or phase references as file names (e.g., `b3_test.go`, `phase4.go`, `task_impl.go`)
- Use single-letter or abbreviated names that require context to interpret (e.g., `h.go`, `dq.go`, `wt.go`)
- Name a test file after the ticket that created it — name it after the behavior it verifies
- Reuse the same name across different packages when the functionality differs

**MUST:**

- Name source files after the domain concept or behavior they implement (e.g., `confidence_gate_test.go`, `wash_trading.go`, `honeypot.go`)
- Follow the `<concept>_test.go` pattern for test files, where `<concept>` is the behavior under test
- When a file tests multiple related behaviors, use the broadest accurate concept (e.g., `process_with_estimates_test.go` for all `ProcessWithEstimates` variants)
- When renaming a file, update the package-level comment at the top of the file to match

**Examples:**

| Bad (ambiguous) | Good (functional)         | Reason                                  |
| --------------- | ------------------------- | --------------------------------------- |
| `b3_test.go`    | `confidence_gate_test.go` | Names the behavior, not the sprint task |
| `phase4.go`     | `slippage_model.go`       | Names the domain concept                |
| `helpers.go`    | `token_math.go`           | Disambiguates the specific helpers      |
| `wt.go`         | `wash_trading.go`         | Full word, no abbreviation              |
| `task_impl.go`  | `feature_extraction.go`   | Describes what the code does            |

---

## Skill System

Skills are pre-digested knowledge packages that agents load on-demand. They live in `.github/skills/<name>/SKILL.md`.

### Skill Structure

```
.github/skills/
├── dto/SKILL.md                     # DTO validation and registry
├── pipeline/SKILL.md                # Stage ordering and dependencies
├── modularity/SKILL.md              # Module boundary enforcement
├── determinism/SKILL.md             # No-randomness enforcement
├── idempotency/SKILL.md             # Content-addressable IDs, ON CONFLICT
├── failure/SKILL.md                 # Retry, abort, degradation
├── token-optimization/SKILL.md      # Context loading optimization
├── config-validation/SKILL.md       # Config-driven parameters
├── code-quality/SKILL.md            # Type hints, logging, standards
├── coding-standards/SKILL.md        # Naming, function design, language idioms
├── conflict-resolution/SKILL.md     # Git merge conflict resolution
├── docs-sync/SKILL.md               # Documentation drift detection
├── database-portability/SKILL.md    # Engine-agnostic SQL
├── running-prompt/SKILL.md          # Structured task execution workflow
├── security-audit/SKILL.md          # OWASP security auditing
├── test-generation/SKILL.md         # Test patterns and coverage
├── vertical-slice/SKILL.md          # Feature-per-folder architecture
├── api-design/SKILL.md              # REST/gRPC API patterns
├── project-scaffold/SKILL.md        # Project initialization validation
├── dependency-analysis/SKILL.md     # Import graph and coupling analysis
├── migration-management/SKILL.md    # Database migration best practices
├── performance-optimization/SKILL.md # Performance profiling patterns
├── caveman/SKILL.md                 # Ultra-compressed output mode (~75% fewer tokens)
├── brainstorming/SKILL.md           # Design-first gate before any implementation
├── writing-plans/SKILL.md           # Break work into bite-sized implementation tasks
├── subagent-driven-development/SKILL.md # Fresh subagent per task + 2-stage review
├── test-driven-development/SKILL.md # RED-GREEN-REFACTOR cycle enforcement
├── rtk/SKILL.md                     # Token-efficient CLI proxy (60-90% savings)
├── parallel-dev-docs/SKILL.md       # PARALLEL_DEV.md documentation standard
├── dex-scanning/SKILL.md            # DEX market scanning, on-chain event ingestion (Layer 0)
├── data-quality-engine/SKILL.md     # Adaptive firewall: rug/honeypot/wash detection (Layer 1)
├── edge-detection/SKILL.md          # Signal & edge discovery, NEW_LAUNCH_EDGE (Layer 3)
├── probability-modeling/SKILL.md    # P(success)/slippage/latency models (Layer 4)
├── execution-engine/SKILL.md        # Multi-wallet execution, wallet sharding (Layer 8)
├── capital-sizing/SKILL.md          # Capital allocation, Kelly-adjacent sizing (Layer 7)
├── position-management/SKILL.md     # Position exit management, TP/SL/trailing (Layer 9)
├── learning-engine/SKILL.md         # Adaptive learning, bounded updates, LearningRecord (Layer 10)
├── strategy-versioning/SKILL.md     # Immutable versioning, A/B promotion, rollback
├── telegram-dispatcher/SKILL.md     # Event-bus-only Telegram, operator commands
├── token-lifecycle/SKILL.md         # Token state machine, CAS transitions, expiry
├── event-bus/SKILL.md               # PostgreSQL append-only event bus, SKIP LOCKED workers
├── rpc-management/SKILL.md          # Multi-endpoint RPC, circuit breaker, fee bump
├── anti-manipulation/SKILL.md       # Wash/rug/honeypot/fakeliq/tax detection algorithms
├── observability/SKILL.md           # KPI tracking, structured logging, health monitoring
├── operational-modes/SKILL.md       # STRICT/BALANCED/EXPLORATION mode transitions
├── traceability/SKILL.md            # Four-field trace contract: TraceID/CorrelationID/CausationID/VersionID
├── profit-first/SKILL.md            # Profit factor design framework, feature evaluation gate
├── drawdown-protection/SKILL.md     # HWM-based tiered drawdown, kill switch, session floor
├── liquidity-event-detector/SKILL.md # DEX volume spikes, cascade detection, DQ filter gate
├── momentum-detector/SKILL.md       # Trend strength, RSI/volume confirmation (Layer 3, Gate 5)
├── loss-pattern-analyzer/SKILL.md   # 7-bucket loss classification, systemic pattern alerts
├── execution-quality-analyzer/SKILL.md # Slippage/fill/latency/cost-as-edge execution audit
├── overfit-detector/SKILL.md        # Max 5 indicators, max 3 params, min 100 samples gate
├── replay-engine-pattern/SKILL.md   # Deterministic replay with replay: prefix isolation
├── feature-stability-checker/SKILL.md # 60% directional consistency gate, weight redistribution
├── strategy-decay-detector/SKILL.md  # 5-metric decay scoring, auto-disable thresholds
├── strategy-auto-disable/SKILL.md    # 5-trigger lifecycle: probation→active→disabled→review
├── monitoring-loop-engine/SKILL.md   # Price-driven position poll loop, kill switch first
├── exposure-monitor/SKILL.md         # 80% portfolio cap, 20 positions, 0.5% single limit gate
├── signal-normalizer/SKILL.md        # Z-score + sigmoid two-stage normalization to [-1,+1]
├── price-feed-integration/SKILL.md   # Live price feed (EVM getAmountsOut + Solana AMM decode), GAP-02
└── production-gate-reviewer/SKILL.md # Production readiness gate — BLOCKER vs SAFE_TO_IGNORE, shadow/micro-capital/live progression
```

### Skill Loading Rules

- **Load skills before raw docs** — skills are pre-digested, cheaper than full documents
- **Reference, don't repeat** — say "per dto skill" instead of re-stating rules
- **Progressive disclosure** — skill → doc section → full doc (only when needed)
- Each skill has standardized format: frontmatter (`name`, `type`, `description`) + Purpose, Rules, Inputs, Outputs, Examples, Checklist

### Always-Active Skills

These skills apply to **every agent and every task** without explicit loading:

| Skill                         | Always On | Purpose                                                               |
| ----------------------------- | --------- | --------------------------------------------------------------------- |
| `caveman`                     | ✅        | Compress output ~75% when user requests it — no filler, full accuracy |
| `brainstorming`               | ✅        | Design-first gate — NEVER write code before presenting a design       |
| `writing-plans`               | ✅        | After design approval, break into 2-5 min tasks before implementing   |
| `subagent-driven-development` | ✅        | Dispatch fresh subagent per task with 2-stage spec + quality review   |
| `test-driven-development`     | ✅        | No production code without a failing test first                       |
| `rtk`                         | ✅        | Use `rtk <cmd>` for terminal output compression (60-90% savings)      |

> **Superpowers shorthand:** `brainstorming` + `writing-plans` + `subagent-driven-development` + `test-driven-development` are collectively called **superpowers** and are always active.

### Agent–Skill Composition

Each agent declares its skills in a `## Skills Used` section.

#### Core Pipeline Agents

| Skill                       | dto-guardian | integration | orchestrator | phase-builder | module-builder | refactor | conflict-resolver | merge-reviewer |
| --------------------------- | ------------ | ----------- | ------------ | ------------- | -------------- | -------- | ----------------- | -------------- | --- | ---------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | -------------------- | --- | --- | --- | --- | --- | --- | --- | --- |
| dto                         | ✅           | ✅          |              | ✅            | ✅             |          | ✅                | ✅             |
| pipeline                    |              | ✅          | ✅           | ✅            |                |          | ✅                | ✅             |
| modularity                  | ✅           | ✅          |              | ✅            | ✅             | ✅       | ✅                | ✅             |
| determinism                 | ✅           |             |              | ✅            | ✅             | ✅       |                   |                |
| idempotency                 |              | ✅          | ✅           | ✅            | ✅             |          |                   | ✅             |
| failure                     |              | ✅          | ✅           | ✅            |                |          |                   |                |
| config-validation           |              |             |              | ✅            | ✅             |          |                   |                |
| code-quality                |              |             |              | ✅            | ✅             | ✅       |                   | ✅             |
| coding-standards            |              |             |              | ✅            | ✅             | ✅       |                   | ✅             |     | coding-standards |     |     |     | ✅  | ✅  | ✅  |     | ✅  |     | database-portability |     | ✅  | ✅  | ✅  |     |     |     | ✅  |
| token-optimization          |              |             |              | ✅            |                |          |                   |                |
| brainstorming               |              |             | ✅           | ✅            | ✅             |          |                   |                |
| writing-plans               |              |             | ✅           | ✅            |                |          |                   |                |
| subagent-driven-development |              |             | ✅           | ✅            |                |          |                   | ✅             |
| test-driven-development     |              |             |              |               | ✅             | ✅       |                   |                |
| docs-sync                   | ✅           | ✅          |              |               |                |          |                   | ✅             |
| conflict-resolution         |              |             |              |               |                |          | ✅                |                |

#### Framework Agents

| Skill                       | scaffold | security-auditor | test-builder | upgrade-manager | doctor |
| --------------------------- | -------- | ---------------- | ------------ | --------------- | ------ |
| project-scaffold            | ✅       |                  |              | ✅              |        |
| vertical-slice              | ✅       |                  |              |                 |        |
| config-validation           | ✅       |                  |              | ✅              | ✅     |
| code-quality                | ✅       | ✅               | ✅           |                 |        |
| coding-standards            | ✅       | ✅               | ✅           |                 |        |
| modularity                  |          |                  | ✅           | ✅              | ✅     |
| security-audit              |          | ✅               |              |                 |        |
| dependency-analysis         |          | ✅               |              |                 | ✅     |
| test-generation             |          |                  | ✅           |                 |        |
| test-driven-development     |          |                  | ✅           |                 |        |
| dto                         |          |                  | ✅           |                 |        |
| pipeline                    |          |                  |              | ✅              |        |
| brainstorming               | ✅       |                  |              | ✅              |        |
| writing-plans               | ✅       |                  |              | ✅              |        |
| subagent-driven-development | ✅       |                  |              | ✅              |        |
| caveman                     | ✅       | ✅               | ✅           | ✅              | ✅     |
| rtk                         | ✅       | ✅               | ✅           | ✅              | ✅     |
| docs-sync                   |          |                  |              |                 | ✅     |

#### SubAgent Delegation Map

Agents delegate to specialized subagents via `runSubagent`:

| Caller Agent     | Delegates To                                | Purpose                                       |
| ---------------- | ------------------------------------------- | --------------------------------------------- |
| scaffold         | dto-guardian, doctor                        | Validate contracts, post-init health check    |
| security-auditor | test-builder                                | Generate tests for identified vulnerabilities |
| test-builder     | Explore                                     | Find untested code paths                      |
| upgrade-manager  | scaffold, doctor                            | Generate missing structure, validate result   |
| doctor           | dto-guardian, integration, security-auditor | Deep DTO/coupling/security checks             |
| phase-builder    | module-builder, integration                 | Build modules, wire pipeline                  |

---

## Protected Files

These files/directories have strict modification rules during parallel development:

| Path                      | Rule                                                                                                                                                                              |
| ------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `contracts/*`             | **Additive only** — new DTOs allowed, existing fields never modified                                                                                                              |
| `database/*`              | **Migrations immutable** — new migrations and adapter/engine additions allowed in any phase; existing migration files in `database/migrations/` must never be modified or deleted |
| `docs/*`                  | **Read-only** — no agent may modify documentation                                                                                                                                 |
| `docs/PROGRESS_REPORT.md` | **Exception** — must be updated after each phase completion (see below)                                                                                                           |
| `config/*`                | **Append-only** — new keys allowed, existing keys never removed                                                                                                                   |

### PROGRESS_REPORT.md Exception

`docs/PROGRESS_REPORT.md` is the **sole writable file** under `docs/`. It tracks implementation
status and must be kept current:

- **Automated:** `run_parallel.sh` updates it automatically on pipeline success/failure/rollback.
- **Manual:** After any manual implementation session, update Phase Progress, Agent Pipeline
  Results, Quality Gates, and Session History tables in `docs/PROGRESS_REPORT.md`.
- **Agents:** The `phase-builder` agent updates it after completing a phase.
- **All other `docs/` files remain strictly read-only.** Never modify `architecture.md`,
  `dto_contracts.md`, `orchestrator_spec.md`, `db_adapter_spec.md`, `implementation_roadmap.md`,
  `PARALLEL_DEV.md`, `AGENTS_AND_SKILLS.md`, `STARTER_GUIDE.md`, or any file in `docs/architecture-context/`.

---

## Migration-Safe Database Rules

1. Migration files follow naming: `YYYYMMDD000NNN_description.sql`
2. Migrations are **append-only** — never modify existing migration files
3. All schema changes go through migrations — no ad-hoc ALTER TABLE
4. All SQL uses **portable syntax** compatible with all supported engines
5. Use `ON CONFLICT DO NOTHING` (not engine-specific variants like `INSERT OR IGNORE`)
6. Use parameterized queries only — no string interpolation in SQL
7. Use `CURRENT_TIMESTAMP` for defaults — no engine-specific date/time functions
8. Engine-specific settings (e.g., WAL mode, connection pooling) belong in `database/engines/` only
