- **Load skills before raw docs** — skills are pre-digested, cheaper than full documents
- **Reference, don't repeat** — say "per dto skill" instead of re-stating rules
- **Progressive disclosure** — skill → doc section → full doc (only when needed)
- Each skill has standardized format: frontmatter (`name`, `type`, `description`) + Purpose, Rules, Inputs, Outputs, Examples, Checklist

## Always-Active Skills

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

## Agent–Skill Composition

Each agent declares its skills in a `## Skills Used` section.

## Core Pipeline Agents

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

## Framework Agents

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

## SubAgent Delegation Map

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

## PROGRESS_REPORT.md Exception

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