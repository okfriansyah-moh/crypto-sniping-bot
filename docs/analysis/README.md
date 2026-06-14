# Analysis & Certifications

Point-in-time investigations, gap inventories, and operational certifications.
These documents preserve **why** decisions were made; they are not live system specifications.

For current implementation status, see [`PROGRESS_REPORT.md`](../ops/PROGRESS_REPORT.md).
For canonical design, see [`architecture.md`](../reference/architecture.md).

## Documents

| Document | Date | Type | Superseded by |
| -------- | ---- | ---- | ------------- |
| [`2026-05-20-production-gate-analysis.md`](2026-05-20-production-gate-analysis.md) | 2026-05-20 | Health report + fix guides | [`plans/2026-06-10-profit-restoration-plan.md`](../plans/2026-06-10-profit-restoration-plan.md) Tasks 13–19 |
| [`profitability-gaps.md`](profitability-gaps.md) | 2026-04 | Gap inventory (GAP-01–17) | Phase 9 in `implementation_roadmap.md`; partial fixes in profit-restoration plan |
| [`rpc-provider-analysis.md`](rpc-provider-analysis.md) | 2026-05 | RPC budget / provider comparison | Operational reference — check against current `shared/config/chains.yaml` |
| [`battle-tested-certification.md`](battle-tested-certification.md) | 2026-06-10 | 11-scenario regression matrix | Run `make battle-test` to re-validate |

## Conventions

- Add a **Historical snapshot** banner when archiving new analysis
- Link forward to the plan or PROGRESS_REPORT entry that incorporated findings
- Do not duplicate content from `architecture.md` — cross-reference § sections instead

## Former paths

See [`../REDIRECTS.md`](../REDIRECTS.md) for the full migration table.
