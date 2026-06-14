# Integration tests

## Gate review regression fixture (2026-06-01)

`gate_review_collect_test.go` replays `scripts/gate_review_collect.sh --analyze` against the
production gate-review capture:

- `output/logs/gate_raw_20260601_161344.log`

That file is a **real operational log window** from the June 1, 2026 production gate review
(not a synthetic sample). It is stored under `output/` (gitignored); copy the capture locally
to run the regression.

The test asserts **corrected evidence semantics** after the gate-evidence reconciliation plan:

- No fake L2–L5 “dead worker” blockers from legacy message names (`features_extracted`, etc.)
- Downstream `stage_completed` counts stay zero when `dq_worker` emitted=0
- `traces_completed` is anchored on `learning_record_emitted` (L10), not partial lifecycle hints
- Production auto-decision does not advance to shadow/micro/live on false evidence

`DQ_SKIPPED` SQL funnel semantics are covered in `database/engines/postgres/lifecycle_stats_test.go`.
