---
name: docs-sync
description: "Documentation synchronization. Use when verifying that code implementations match their documentation specifications. Detects drift between docs/ specifications and actual code."
---

# Documentation Sync Skill

## Purpose

Detect and report drift between specification documents in `docs/` and actual code implementation. Ensures the codebase matches its documented design at all times.

## Rules

- `docs/` files are **read-only** during normal development — agents must not modify them
- Code must match the specifications in docs, not the other way around
- When drift is detected, the code is wrong (docs are the source of truth for design)
- DTO definitions in code must match `docs/dto_contracts.md`
- Pipeline stage order in code must match `docs/architecture.md`
- Orchestrator behavior must match `docs/orchestrator_spec.md`
- Database adapter interface must match `docs/db_adapter_spec.md`

## Inputs

- Documentation in `docs/`
- Source code in `app/`, `contracts/`, `database/`

## Outputs

- Drift report listing mismatches between docs and code
- Categorized by severity: blocking (must fix) vs advisory (warn only)

## Examples

### Checking DTO Drift

```
Spec (docs/dto_contracts.md):
  EntityResult: entity_id (str), name (str), status (str), score (float)

Code (contracts/entity.py):
  EntityResult: entity_id (str), name (str), status (str)
  ❌ DRIFT: missing field 'score' (float)
```

### Checking Pipeline Stage Order

```
Spec (docs/architecture.md):
  stage_1 → stage_2 → stage_3

Code (app/orchestrator/pipeline.py):
  STAGES = ['stage_1', 'stage_3', 'stage_2']
  ❌ DRIFT: stage_2 and stage_3 are in wrong order
```

### Checking Adapter Interface

```
Spec (docs/db_adapter_spec.md):
  def create_run(self, run: PipelineRunDTO) -> None

Code (database/adapter.*):
  def create_run(self, run_id: str, input_path: str) -> None
  ❌ DRIFT: adapter should accept PipelineRunDTO, not raw args
```

## Checklist

- [ ] All DTOs in `contracts/` match `docs/dto_contracts.md`
- [ ] Pipeline stage order matches `docs/architecture.md`
- [ ] Orchestrator behavior matches `docs/orchestrator_spec.md`
- [ ] Database adapter interface matches `docs/db_adapter_spec.md`
- [ ] No undocumented stages, DTOs, or interfaces exist in code
- [ ] No `docs/` files have been modified by implementation agents
