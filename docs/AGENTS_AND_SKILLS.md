# Agents and Skills System

> Defines the agent/skill composition system for AI-assisted parallel development.

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
phase-builder → dto-guardian → integration → refactor (conditional)
```

1. **phase-builder** implements the assigned phase
2. **dto-guardian** validates all DTO contracts
3. **integration** validates cross-module wiring
4. **refactor** runs only if quality gates fail

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

| Agent             | Always Loads                                | Loads On-Demand                             |
| ----------------- | ------------------------------------------- | ------------------------------------------- |
| phase-builder     | dto, modularity, pipeline                   | determinism, idempotency, config-validation |
| dto-guardian      | dto, modularity                             | determinism, docs-sync                      |
| integration       | pipeline, dto, database-portability         | idempotency, failure, docs-sync             |
| refactor          | modularity, code-quality                    | determinism                                 |
| orchestrator      | pipeline, idempotency, database-portability | failure                                     |
| module-builder    | dto, modularity, code-quality               | determinism, idempotency, config-validation |
| conflict-resolver | conflict-resolution                         | dto, modularity                             |
| merge-reviewer    | dto, pipeline, modularity                   | docs-sync                                   |
| task-sync         | running-prompt, modularity                  | dto, pipeline, code-quality                 |

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

| Phase   | Required Skills                               |
| ------- | --------------------------------------------- |
| Phase 0 | idempotency, failure                          |
| Phase N | dto, modularity, determinism + phase-specific |

Core skills loaded by **every** phase: `dto`, `modularity`, `determinism`.
