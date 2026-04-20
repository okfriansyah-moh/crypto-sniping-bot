# Skeleton Parallel — Copilot Instructions

> These instructions enforce the architectural constraints for any project built on this framework.
> Violations are not acceptable and must not be introduced, even partially.

---

## Reference Documents

| Document                         | Purpose                                                                                                   |
| -------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `docs/architecture.md`           | Master reference — system architecture, module breakdown, pipeline flow, data model                       |
| `docs/implementation_roadmap.md` | Phase-based implementation roadmap with schemas, algorithms, exit criteria, priority layers               |
| `docs/orchestrator_spec.md`      | Orchestrator specification — execution model, checkpointing, resume, idempotency, failure handling        |
| `docs/dto_contracts.md`          | DTO definitions with all fields/types/constraints, cross-module dependency matrix, validation rules       |
| `docs/db_adapter_spec.md`        | Database abstraction layer — adapter interface, SQL compatibility, migration strategy, engine portability |
| `docs/PARALLEL_DEV.md`           | Parallel development orchestration guide — 3-mode execution system, phase grouping, token optimization    |
| `docs/AGENTS_AND_SKILLS.md`      | Agent/skill system — agents, skills, composition matrices, token optimization, parallel dev integration   |
| `docs/STARTER_GUIDE.md`          | Getting started playbook — setup, architecture generation, roadmap generation, parallel system usage      |
| `docs/PROGRESS_REPORT.md`        | Implementation status — completed work, test results, remaining items, phase-by-phase progress tracking   |
| `contracts/`                     | Immutable DTO definitions — all modules MUST use these, not upstream sources or raw dicts/objects         |
| `config/`                        | YAML configuration files — all thresholds, paths, and tunable parameters live here                        |

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

Stages execute in **strict sequential order** — never reorder, skip, or parallelize stages at runtime:

```
[Define your pipeline stages in docs/architecture.md]
stage_1 → stage_2 → stage_3 → ... → stage_N
```

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

## Forbidden Technologies

Do not introduce any of these unless the project explicitly requires them:

| Category     | Default Forbidden                                                   | Override             |
| ------------ | ------------------------------------------------------------------- | -------------------- |
| Architecture | Microservices, Kafka, RabbitMQ, Kubernetes, Docker orchestration    | Unless project needs |
| Databases    | MongoDB, Redis, any distributed database                            | Unless project needs |
| AI/ML        | OpenAI API, Anthropic API, LangChain, AutoGPT, CrewAI, any paid LLM | Unless project needs |
| Cloud        | AWS, GCP, Azure, any cloud compute or storage                       | Unless project needs |
| Runtime      | Agent loops, autonomous planners, event-driven architectures        | Unless project needs |

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
└── rtk/SKILL.md                     # Token-efficient CLI proxy (60-90% savings)
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

| Path          | Rule                                                                 |
| ------------- | -------------------------------------------------------------------- |
| `contracts/*` | **Additive only** — new DTOs allowed, existing fields never modified |
| `database/*`  | **Phase 0 only** — only infrastructure phase may modify              |
| `docs/*`      | **Read-only** — no agent may modify documentation                    |
| `config/*`    | **Append-only** — new keys allowed, existing keys never removed      |

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
