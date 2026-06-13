# Agents and Skills System

> Defines the agent/skill composition system for AI-assisted parallel development.

---

## 0. Documentation map

Before reading agents/skills, use [`../README.md`](../README.md) as the entry point.

| Need | Path |
| ---- | ---- |
| System design | `docs/reference/architecture.md` |
| Active implementation plan | `docs/plans/` (e.g. `2026-06-13-operator-dashboard-plan.md`) |
| Completed plans | `docs/plans/2026-06-10-profit-restoration-plan.md`, `2026-05-10-rescan-plan.md` |
| Dated investigations | `docs/analysis/` |
| Layer-focused extracts (supplementary) | `docs/archive/architecture-context/` |

---

## 1. Overview

The skeleton-parallel framework uses a two-tier AI assistance system:

- **Agents** — Autonomous execution roles that perform multi-step tasks
- **Skills** — Focused knowledge modules that provide domain-specific rules and patterns

Agents consume skills to minimize token usage while maintaining constraint enforcement.

---

## 2. Agent Registry

### Core Agents

| Agent             | File                                        | Purpose                                        |
| ----------------- | ------------------------------------------- | ---------------------------------------------- |
| phase-builder     | `.github/agents/phase-builder.agent.md`     | Implement any phase from the roadmap           |
| dto-guardian      | `.github/agents/dto-guardian.agent.md`      | Validate DTO contracts in `contracts/`         |
| integration       | `.github/agents/integration.agent.md`       | Wire modules together, detect coupling         |
| refactor          | `.github/agents/refactor.agent.md`          | Improve code structure without behavior change |
| orchestrator      | `.github/agents/orchestrator.agent.md`      | Build and validate the pipeline orchestrator   |
| module-builder    | `.github/agents/module-builder.agent.md`    | Implement individual modules from specs        |
| conflict-resolver | `.github/agents/conflict-resolver.agent.md` | Resolve Git merge conflicts (union strategy)   |
| merge-reviewer    | `.github/agents/merge-reviewer.agent.md`    | Post-merge validation and quality review       |
| task-sync         | `.github/agents/task-sync.agent.md`         | Structured task execution workflow             |

### Framework Agents

| Agent            | File                                       | Purpose                                        |
| ---------------- | ------------------------------------------ | ---------------------------------------------- |
| scaffold         | `.github/agents/scaffold.agent.md`         | Initialize new projects with correct structure |
| security-auditor | `.github/agents/security-auditor.agent.md` | OWASP-aware security review and CVSS scoring   |
| test-builder     | `.github/agents/test-builder.agent.md`     | Generate unit and integration tests            |
| upgrade-manager  | `.github/agents/upgrade-manager.agent.md`  | Upgrade existing repos to use the framework    |
| doctor           | `.github/agents/doctor.agent.md`           | Project health check and validation            |

### Agent Pipeline (Execution Order)

Every phase runs through this mandatory agent chain:

```text
phase-builder → dto-guardian → integration → security-auditor → test-builder → refactor (remediation only)
```

1. **phase-builder** implements the assigned phase
2. **dto-guardian** validates all DTO contracts
3. **integration** validates cross-module wiring
4. **security-auditor** performs OWASP review
5. **test-builder** generates unit + integration tests
6. **refactor** runs only if quality gates fail (remediation)

### Agent Capabilities

| Agent             | Can Read | Can Edit | Can Run Tests | Can Call DB | Can Call Other Agents                            |
| ----------------- | -------- | -------- | ------------- | ----------- | ------------------------------------------------ |
| phase-builder     | ✅       | ✅       | ✅            | ❌          | ✅ (module-builder, integration)                 |
| dto-guardian      | ✅       | ❌       | ❌            | ❌          | ❌                                               |
| integration       | ✅       | ✅       | ✅            | ❌          | ✅ (dto-guardian)                                |
| refactor          | ✅       | ✅       | ✅            | ❌          | ❌                                               |
| orchestrator      | ✅       | ✅       | ✅            | ❌          | ❌                                               |
| module-builder    | ✅       | ✅       | ✅            | ❌          | ❌                                               |
| conflict-resolver | ✅       | ✅       | ❌            | ❌          | ❌                                               |
| merge-reviewer    | ✅       | ❌       | ✅            | ❌          | ✅ (dto-guardian, integration)                   |
| task-sync         | ✅       | ✅       | ✅            | ❌          | ✅ (subagents)                                   |
| scaffold          | ✅       | ✅       | ✅            | ❌          | ✅ (dto-guardian, doctor)                        |
| security-auditor  | ✅       | ❌       | ❌            | ❌          | ✅ (test-builder)                                |
| test-builder      | ✅       | ✅       | ✅            | ❌          | ✅ (Explore)                                     |
| upgrade-manager   | ✅       | ✅       | ❌            | ❌          | ✅ (scaffold, doctor)                            |
| doctor            | ✅       | ❌       | ❌            | ❌          | ✅ (dto-guardian, integration, security-auditor) |

---

## 3. Skill Registry

### Core Skills

| Skill                | File                                           | Purpose                                    |
| -------------------- | ---------------------------------------------- | ------------------------------------------ |
| dto                  | `.github/skills/dto/SKILL.md`                  | DTO registry, validation, anti-patterns    |
| pipeline             | `.github/skills/pipeline/SKILL.md`             | Stage ordering, DTO flow, parallelism      |
| modularity           | `.github/skills/modularity/SKILL.md`           | Module boundaries, import rules            |
| determinism          | `.github/skills/determinism/SKILL.md`          | No-randomness enforcement                  |
| idempotency          | `.github/skills/idempotency/SKILL.md`          | Content-addressable IDs, ON CONFLICT       |
| failure              | `.github/skills/failure/SKILL.md`              | Retry policies, degradation, thresholds    |
| token-optimization   | `.github/skills/token-optimization/SKILL.md`   | Context compression, progressive loading   |
| config-validation    | `.github/skills/config-validation/SKILL.md`    | Config-driven parameters, YAML enforcement |
| code-quality         | `.github/skills/code-quality/SKILL.md`         | Type annotations, logging, code standards  |
| coding-standards     | `.github/skills/coding-standards/SKILL.md`     | Naming, function design, language idioms   |
| conflict-resolution  | `.github/skills/conflict-resolution/SKILL.md`  | Git merge conflict resolution              |
| docs-sync            | `.github/skills/docs-sync/SKILL.md`            | Documentation drift detection              |
| database-portability | `.github/skills/database-portability/SKILL.md` | Engine-agnostic SQL, adapter patterns      |
| running-prompt       | `.github/skills/running-prompt/SKILL.md`       | Structured task execution workflow         |

### Framework Skills

| Skill                       | File                                                  | Purpose                                     |
| --------------------------- | ----------------------------------------------------- | ------------------------------------------- |
| security-audit              | `.github/skills/security-audit/SKILL.md`              | OWASP security auditing, CVSS scoring       |
| test-generation             | `.github/skills/test-generation/SKILL.md`             | Test patterns, coverage, AAA structure      |
| vertical-slice              | `.github/skills/vertical-slice/SKILL.md`              | Feature-per-folder architecture             |
| api-design                  | `.github/skills/api-design/SKILL.md`                  | REST/gRPC API patterns, error formats       |
| project-scaffold            | `.github/skills/project-scaffold/SKILL.md`            | Project initialization and validation       |
| dependency-analysis         | `.github/skills/dependency-analysis/SKILL.md`         | Import graph and coupling analysis          |
| migration-management        | `.github/skills/migration-management/SKILL.md`        | Database migration best practices           |
| performance-optimization    | `.github/skills/performance-optimization/SKILL.md`    | Performance profiling and optimization      |
| caveman                     | `.github/skills/caveman/SKILL.md`                     | Ultra-compressed output (~75% fewer tokens) |
| brainstorming               | `.github/skills/brainstorming/SKILL.md`               | Design-first gate before any implementation |
| writing-plans               | `.github/skills/writing-plans/SKILL.md`               | Break work into bite-sized tasks            |
| subagent-driven-development | `.github/skills/subagent-driven-development/SKILL.md` | Fresh subagent per task + 2-stage review    |
| test-driven-development     | `.github/skills/test-driven-development/SKILL.md`     | RED-GREEN-REFACTOR cycle enforcement        |
| rtk                         | `.github/skills/rtk/SKILL.md`                         | Token-efficient CLI proxy (60-90% savings)  |

### Domain Skills (Sniper Pipeline)

| Skill                      | File                                                 | Layer | Purpose                                                    |
| -------------------------- | ---------------------------------------------------- | ----- | ---------------------------------------------------------- |
| dex-scanning               | `.github/skills/dex-scanning/SKILL.md`               | 0     | DEX scanning, MarketDataDTO, event emission                |
| event-bus                  | `.github/skills/event-bus/SKILL.md`                  | All   | PostgreSQL append-only event bus, SKIP LOCKED workers      |
| rpc-management             | `.github/skills/rpc-management/SKILL.md`             | 0/8   | Multi-endpoint RPC, circuit breaker, fee bump              |
| token-lifecycle            | `.github/skills/token-lifecycle/SKILL.md`            | All   | Token state machine, CAS transitions, expiry               |
| data-quality-engine        | `.github/skills/data-quality-engine/SKILL.md`        | 1     | Adaptive firewall: rug/honeypot/wash detection             |
| anti-manipulation          | `.github/skills/anti-manipulation/SKILL.md`          | 1     | Wash/rug/honeypot/fakeliq/tax detection algorithms         |
| edge-detection             | `.github/skills/edge-detection/SKILL.md`             | 3     | NEW_LAUNCH_EDGE, adaptive momentum threshold               |
| momentum-detector          | `.github/skills/momentum-detector/SKILL.md`          | 3     | Trend strength, RSI/volume confirmation (Gate 5)           |
| signal-normalizer          | `.github/skills/signal-normalizer/SKILL.md`          | 3/4   | Z-score + sigmoid two-stage normalization to [-1,+1]       |
| feature-stability-checker  | `.github/skills/feature-stability-checker/SKILL.md`  | 2/3   | 60% directional consistency gate, weight redistribution    |
| liquidity-event-detector   | `.github/skills/liquidity-event-detector/SKILL.md`   | 1/5   | DEX volume spikes, cascade detection, DQ filter gate       |
| probability-modeling       | `.github/skills/probability-modeling/SKILL.md`       | 4     | P(success)/slippage/latency models, EV computation         |
| overfit-detector           | `.github/skills/overfit-detector/SKILL.md`           | 4     | Max 5 indicators, max 3 params, min 100 samples gate       |
| replay-engine-pattern      | `.github/skills/replay-engine-pattern/SKILL.md`      | All   | Deterministic replay with `replay:` prefix isolation       |
| capital-sizing             | `.github/skills/capital-sizing/SKILL.md`             | 7     | Kelly-adjacent sizing, cohort multipliers, AllocationDTO   |
| execution-engine           | `.github/skills/execution-engine/SKILL.md`           | 8     | Multi-wallet execution, wallet sharding, prebuilt calldata |
| execution-quality-analyzer | `.github/skills/execution-quality-analyzer/SKILL.md` | 8     | Slippage/fill/latency/cost-as-edge execution audit         |
| drawdown-protection        | `.github/skills/drawdown-protection/SKILL.md`        | 7/9   | HWM-based tiered drawdown, kill switch, session floor      |
| exposure-monitor           | `.github/skills/exposure-monitor/SKILL.md`           | 7     | 80% portfolio cap, 20 positions, 0.5% single limit gate    |
| position-management        | `.github/skills/position-management/SKILL.md`        | 9     | TP1/TP2/SL/TIME exit logic, trailing stop, PositionState   |
| monitoring-loop-engine     | `.github/skills/monitoring-loop-engine/SKILL.md`     | 9     | Price-driven position poll loop, kill switch first         |
| learning-engine            | `.github/skills/learning-engine/SKILL.md`            | 10    | Adaptive thresholds, FP/FN computation, LearningRecord     |
| loss-pattern-analyzer      | `.github/skills/loss-pattern-analyzer/SKILL.md`      | 10    | 7-bucket loss classification, systemic pattern alerts      |
| strategy-decay-detector    | `.github/skills/strategy-decay-detector/SKILL.md`    | 10    | 5-metric decay scoring, auto-disable thresholds            |
| strategy-auto-disable      | `.github/skills/strategy-auto-disable/SKILL.md`      | 10    | 5-trigger lifecycle: probation→active→disabled→review      |
| strategy-versioning        | `.github/skills/strategy-versioning/SKILL.md`        | All   | Immutable versioning, A/B promotion, rollback              |
| observability              | `.github/skills/observability/SKILL.md`              | All   | KPI tracking, structured logging, health monitoring        |
| operational-modes          | `.github/skills/operational-modes/SKILL.md`          | All   | STRICT/BALANCED/EXPLORATION mode transitions               |
| telegram-dispatcher        | `.github/skills/telegram-dispatcher/SKILL.md`        | All   | Event-bus-only Telegram, operator commands                 |
| traceability               | `.github/skills/traceability/SKILL.md`               | All   | TraceID/CorrelationID/CausationID/VersionID contract       |
| profit-first               | `.github/skills/profit-first/SKILL.md`               | All   | Profit factor design framework, feature evaluation gate    |

### AI Enrichment Skills (cross-layer via `internal/ai/GroqClient`)

AI Enrichment is a cross-cutting layer that uses the **Groq API** (`GROQ_API_KEY` env var) to add narrative intelligence. All calls are **1-shot, fail-open, and autonomous** — the pipeline is never blocked. Model is configurable via `AI_ENRICH_MODEL` env var (default: `llama-3.3-70b-versatile`), following the same pattern as `MODEL_HEAVY` in `scripts/run_parallel.sh`.

| Skill                | Path                                            | Layer | Purpose                                                                    |
| -------------------- | ----------------------------------------------- | ----- | -------------------------------------------------------------------------- |
| `ai-narrative-probe` | `internal/modules/probes/ai_narrative_probe.go` | 0/1   | Narrative scoring 0–10; copy-paste/impersonation detection via Groq API    |
| `loss-explainer`     | `internal/modules/learning/loss_explainer.go`   | 10    | AI loss category + natural-language reason appended to `LearningRecordDTO` |

**Cross-layer effects:**

| Layer | Component                  | Effect                                                                                                         |
| ----- | -------------------------- | -------------------------------------------------------------------------------------------------------------- |
| L0/1  | `AINarrativeProbe`         | Populates `NarrativeScore`, `IsCopyPasteDesc`, `IsImpersonation` on `MarketDataDTO`                            |
| L1 DQ | Copy-paste soft bump       | +0.30 `AggregateRiskScore` for `IsCopyPasteDesc`; +0.20 for `IsImpersonation` (never overrides 3 hard-rejects) |
| L3    | `applyNarrativeMultiplier` | ±10% adjustment to `EdgeConfidence` from `NarrativeScore`                                                      |
| L10   | `LossExplainer`            | Sets `AIExplanationKnown`, `AILossCategory`, `AIExplanation` on `LearningRecordDTO`                            |

### Skill Structure

Each skill is a folder at `.github/skills/<kebab-case-name>/SKILL.md` with standardized format:

```markdown
---
name: <skill-name>
type: skill
description: <one-line description>
---

## Purpose

What this skill provides.

## Rules

Specific constraints and patterns to follow.

## Inputs

What context the skill needs (docs, code, config).

## Outputs

What the skill produces (validation results, patterns, checklists).

## Examples

Correct and incorrect code patterns.

## Checklist

Pre-commit verification items.
```

---

## 4. Agent ↔ Skill Composition Matrix

### Core Pipeline Agents

| Agent             | Always Loads                                           | Loads On-Demand                                                             |
| ----------------- | ------------------------------------------------------ | --------------------------------------------------------------------------- |
| phase-builder     | dto, modularity, pipeline, traceability                | determinism, idempotency, config-validation, profit-first, [phase-specific] |
| dto-guardian      | dto, modularity                                        | determinism, docs-sync, traceability                                        |
| integration       | pipeline, dto, database-portability, traceability      | idempotency, failure, docs-sync, event-bus                                  |
| refactor          | modularity, code-quality                               | determinism, coding-standards                                               |
| orchestrator      | pipeline, idempotency, database-portability, event-bus | failure, token-lifecycle, traceability                                      |
| module-builder    | dto, modularity, code-quality, traceability            | determinism, idempotency, config-validation, [phase-specific]               |
| conflict-resolver | conflict-resolution                                    | dto, modularity                                                             |
| merge-reviewer    | dto, pipeline, modularity                              | docs-sync, traceability                                                     |
| task-sync         | running-prompt, modularity                             | dto, pipeline, code-quality, profit-first                                   |

### Framework Agents

| Agent            | Always Loads                                        | Loads On-Demand      |
| ---------------- | --------------------------------------------------- | -------------------- |
| scaffold         | project-scaffold, vertical-slice, config-validation | code-quality         |
| security-auditor | security-audit, code-quality                        | dependency-analysis  |
| test-builder     | test-generation, code-quality                       | modularity, dto      |
| upgrade-manager  | project-scaffold, config-validation                 | pipeline, modularity |
| doctor           | config-validation, modularity, dependency-analysis  | docs-sync            |

### SubAgent Delegation Map

| Caller Agent     | Delegates To                                | Purpose                                       |
| ---------------- | ------------------------------------------- | --------------------------------------------- |
| scaffold         | dto-guardian, doctor                        | Validate contracts, post-init health check    |
| security-auditor | test-builder                                | Generate tests for identified vulnerabilities |
| test-builder     | Explore                                     | Find untested code paths                      |
| upgrade-manager  | scaffold, doctor                            | Generate missing structure, validate result   |
| doctor           | dto-guardian, integration, security-auditor | Deep DTO/coupling/security checks             |
| phase-builder    | module-builder, integration                 | Build modules, wire pipeline                  |

### Loading Priority

1. **Always load** — Required for the agent to function correctly
2. **On-demand** — Loaded only when the task touches that domain
3. **Never load** — Not relevant to this agent's responsibilities

---

## 5. Token Optimization Strategy

### Skill-First Approach

Skills compress documentation into focused rules:

```
Full doc (5000 tokens) → Skill (400 tokens) = 92% savings
```

### Loading Order

```text
Level 1: Skill name + description (~100 tokens) → decide relevance
Level 2: Skill body (~300–500 tokens) → get focused rules
Level 3: Doc section (~500–2000 tokens) → deep-dive if skill insufficient
Level 4: Full doc (~5000+ tokens) → ONLY for implementing from scratch
```

### Rules

1. Load skills first, docs second
2. Never re-read a skill already in context
3. Use subagents for multi-doc research
4. Reference skills instead of re-stating rules

---

## 6. Parallel Development Integration

### `run_parallel.sh` Agent Injection

The parallel development script automatically:

1. Injects core skill references into every Copilot call
2. Generates `PHASE_TASK.md` with phase-specific skill requirements
3. Chains agents in the correct order (build → validate → integrate → fix)
4. Enforces bounded retries per agent stage
5. Rolls back to checkpoint on agent failure

### Phase-Specific Skill Loading

Each phase loads only the skills it needs:

| Phase | Name                   | Required Skills (domain-specific)                                                                                                                                                                                |
| ----- | ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0     | core-infrastructure    | event-bus, token-lifecycle, traceability, migration-management, database-portability                                                                                                                             |
| 1     | dex-ingestion          | dex-scanning, event-bus, rpc-management, token-lifecycle, traceability                                                                                                                                           |
| 2     | first-trade-pipeline   | data-quality-engine, anti-manipulation, edge-detection, signal-normalizer, capital-sizing, execution-engine, position-management, drawdown-protection, exposure-monitor, rpc-management                          |
| 3     | evaluation-correctness | learning-engine, loss-pattern-analyzer, strategy-versioning, replay-engine-pattern, overfit-detector                                                                                                             |
| 4     | signal-quality         | probability-modeling, overfit-detector, signal-normalizer, feature-stability-checker, liquidity-event-detector, momentum-detector, data-quality-engine, anti-manipulation, edge-detection, replay-engine-pattern |
| 5     | learning-engine        | learning-engine, loss-pattern-analyzer, strategy-decay-detector, strategy-auto-disable, strategy-versioning, feature-stability-checker, monitoring-loop-engine, position-management, overfit-detector            |
| 6     | production-hardening   | observability, operational-modes, telegram-dispatcher, traceability, execution-quality-analyzer, drawdown-protection, exposure-monitor, rpc-management, performance-optimization                                 |

Core skills loaded by **every** phase: `dto`, `modularity`, `determinism`, `traceability`, `idempotency`, `code-quality`, `coding-standards`, `config-validation`, `test-generation`, `test-driven-development`.
