# Implementation Plans

Executable, task-numbered breakdowns produced by the **plan-management** skill.
Each plan has a dependency graph, invariant checklist, and per-task validation steps.

## Active plans

| Plan | Status | Source spec |
| ---- | ------ | ----------- |
| [`2026-06-15-dq-probe-pending-plan.md`](2026-06-15-dq-probe-pending-plan.md) | Implemented | Production gate review (PIPELINE_PROOF) |
| [`2026-06-13-operator-dashboard-plan.md`](2026-06-13-operator-dashboard-plan.md) | Ready for implementation | [`specs/2026-06-10-operator-dashboard-design.md`](../specs/2026-06-10-operator-dashboard-design.md) |

## Completed plans

| Plan | Status | Notes |
| ---- | ------ | ----- |
| [`2026-06-10-profit-restoration-plan.md`](2026-06-10-profit-restoration-plan.md) | Completed | Tasks 1–19; pipeline proof + profit restoration |
| [`2026-06-15-dq-probe-pending-plan.md`](2026-06-15-dq-probe-pending-plan.md) | Completed | Probe pending queue, rescan tuning, momentum override scaffold |
| [`2026-05-10-rescan-plan.md`](2026-05-10-rescan-plan.md) | Implemented | 14-band rescan worker (Layer 0.5) |

## Conventions

- **Naming:** `YYYY-MM-DD-<topic>-plan.md` (kebab-case)
- **Format:** See `.github/skills/plan-management/reference/reference.md`
- **Progress:** Log completion in `docs/ops/PROGRESS_REPORT.md` after each task
- **Do not renumber tasks** when appending — append only

## Former paths

See [`../REDIRECTS.md`](../REDIRECTS.md) for the full migration table.
