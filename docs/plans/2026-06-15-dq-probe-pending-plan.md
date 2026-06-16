# DQ Throughput, Probe Pending Queue & Serial-Launcher Profit Path

**Status:** Implemented (2026-06-15)  
**Canonical Cursor plan:** `.cursor/plans/dq_probe_pending_plan_65b407d0.plan.md`

## Summary

Eliminates false DQ REJECTs from probe rate limiting by deferring tokens to a DB-backed `probe_pending_queue`, reduces rescan SKIP amplification, and scaffolds the serial-launcher momentum profit path for shadow pipeline proof.

## Tasks

| # | Task | Status |
|---|------|--------|
| 1 | `probe_pending_queue` migration | Done |
| 2 | Adapter enqueue/claim/complete | Done |
| 3 | Config (2400 credits/hr, rescan tuning) | Done |
| 4 | MarketProbesWorker defer on budget exhaustion | Done |
| 5 | ProbePendingWorker drain loop | Done |
| 6 | Rescan policy + granular skip flags | Done |
| 7 | Momentum override + shadow Task 22 relaxation | Done |
| 8 | Shadow SKIP false-negative tracker | Done |
| 9 | Dashboard probe pending metrics | Done |
| 10 | Integration tests + PROGRESS_REPORT | Done |

## Exit criteria (shadow gate)

| Metric | Target |
|--------|--------|
| `pre_probe_rate_limit_skipped` → DQ REJECT | 0 |
| `probe_pending_enqueued` when market active | > 0 |
| `dq_pass_or_risky_pass` | ≥ 1 |
| `traces_completed` (shadow) | ≥ 1 |

See the canonical plan for full design rationale and file map.
