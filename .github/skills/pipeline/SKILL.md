---
name: pipeline
description: "Pipeline reasoning. Use when validating stage ordering, understanding dependencies between modules, checking checkpoint behavior, or planning parallel development. Provides the stage sequence pattern, DTO flow map, and parallelism matrix."
---

# Pipeline Reasoning Skill

## Purpose

Enforce correct pipeline stage ordering, validate DTO flow between stages, and support parallel development planning. Ensures the immutable stage sequence is never broken.

## Rules

- Never reorder stages
- Never skip stages
- Never parallelize stages at runtime
- Some stages may be per-entity (run once per item in a batch)
- All checkpoint writes go through `database/adapter.*`

## Inputs

- `docs/architecture.md` — pipeline stage definitions and ordering
- `docs/implementation_roadmap.md` — phase grouping and parallelism matrix

## Outputs

- Validated pipeline wiring in `app/orchestrator/`
- Integration tests in `tests/integration/`
- Stage dependency analysis

## Stage Sequence Pattern

Define your pipeline stages in `docs/architecture.md`:

```
Stage 0: stage_name_0
Stage 1: stage_name_1
Stage 2: stage_name_2
...
Stage N: stage_name_N
```

## DTO Flow Map Pattern

```
                   input_data
                       │
                 ┌─────▼──────┐
                 │   stage_0   │
                 └─────┬──────┘
                   OutputDTO_0
                       │
                 ┌─────▼──────┐
                 │   stage_1   │
                 └─────┬──────┘
                   OutputDTO_1
                       │
                      ...
                       │
                 ┌─────▼──────┐
                 │   stage_N   │
                 └─────┬──────┘
                   FinalOutput
```

Multiple stages may fan-out from a single input or fan-in multiple outputs.

## Checkpoint Behavior

| Stage    | Checkpoint Target                                | Resume Strategy              |
| -------- | ------------------------------------------------ | ---------------------------- |
| stage_0  | `pipeline_runs.last_completed_stage = 'stage_0'` | Re-read from DB              |
| stage_1  | `last_completed_stage = 'stage_1'`               | Re-read from DB              |
| per-item | Entity status per item                           | Skip items already processed |

## Development Parallelism Pattern

```
Phase 0  ──→ [core infrastructure]         ← must complete first
Phase 1  ──→ [stage_a] [stage_b]           ← PARALLEL if independent
Phase 2  ──→ [stage_c]                     ← depends on Phase 1
Phase 3  ──→ [stage_d] [stage_e]           ← PARALLEL if independent
```

Define your specific parallelism matrix in `docs/implementation_roadmap.md`.

## Examples

### Stage Dependencies Template

| Stage   | Requires         | Cannot Run Without |
| ------- | ---------------- | ------------------ |
| stage_1 | Stage0Output     | Phase 0            |
| stage_2 | Stage1Output     | Phase 1            |
| stage_3 | Stage1Output + X | Phase 1 + Phase 2  |

## Checklist

- [ ] Pipeline stages are in the order defined in `docs/architecture.md`
- [ ] No stage is skipped or reordered
- [ ] Every stage boundary has compatible DTO types
- [ ] Checkpoints are written after every stage completion
- [ ] Resume logic reconstructs DTOs from database
