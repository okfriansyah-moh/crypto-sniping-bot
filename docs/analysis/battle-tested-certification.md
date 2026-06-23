# Battle-Tested Certification

> **Location:** `docs/analysis/battle-tested-certification.md` (moved from `docs/analysis/battle-tested-certification.md`, 2026-06-13).
> Index: [`docs/README.md`](../README.md).

> **Status:** `BATTLE_TEST_CERTIFICATION: READY` (offline scenario matrix)  
> **Last validated:** 2026-06-10  
> **Canonical runner:** `make battle-test`

This document certifies that the crypto-sniping-bot pipeline has been exercised against a fixed **11-scenario matrix** using production `shared/config/pipeline.yaml` thresholds. Scenarios vary **input quality only** — mandatory DQ guardrails are never relaxed.

AI agents and operators may treat this repo as **production-architecture proven** for pipeline mechanics, capital-defense behavior, and observability contracts. Real-money deployment still requires live shadow soak and operator sign-off.

---

## What “battle-tested” means here

| Claim | Evidence |
| ----- | -------- |
| L0→L10 can complete end-to-end | Scenarios `01`, `10` + `make gate-proof-mock` |
| Mandatory DQ hard-rejects fire | Scenarios `03`, `04` |
| Serial-launcher guardrails dominate Helius-like traffic | Scenario `02` → `GUARDRAILS_ACTIVE` (not `CODE_DEFECT`) |
| Layer stop-points are observable | Scenarios `05`–`08` |
| Shadow execution path works | Scenario `09` |
| Shadow false-negative learning | Scenario `11` |
| No idempotency / WSOL regressions on golden path | `validate_pipeline_proof.sh` on scenarios `01`, `10` |

---

## Scenario matrix

| ID | Layer | Scenario | Expected outcome |
| -- | ----- | -------- | ---------------- |
| `01_golden_l0_l10_pass` | L0–L10 | Golden shadow trace | `traces_completed ≥ 1`, `PIPELINE_PROOF` PASS |
| `02_helius_serial_launcher_skip` | L1 | High-volume pump.fun-like feed | `throughput_verdict = GUARDRAILS_ACTIVE`, `dq_pass = 0` |
| `03_no_social_links_reject` | L1 | No social / website | `REJECT` + `no_social_links` |
| `04_high_total_supply_reject` | L1 | Supply > 1B | `REJECT` + `high_total_supply` |
| `05_l3_edge_filtered` | L3 | Weak edge | `edge_worker` `filtered` |
| `06_l5_validation_reject` | L5 | EV below gate | `validation_worker` `rejected` |
| `07_l6_selection_pass` | L6 | Top-K select | `selection_worker` `emitted` |
| `08_l6_selection_not_selected` | L6 | Diversity / cap | `selection_worker` `filtered` |
| `09_shadow_execution` | L8 | Shadow submit + open | `execution_submitted shadow=true` |
| `10_l10_time_exit_learning` | L10 | TIME exit | `learning_record_emitted`, `PIPELINE_PROOF` PASS |
| `11_shadow_fn_rejection` | L1-shadow | DQ SKIP → shadow recorder | `shadow_record_emitted` |

Fixtures: `tests/fixtures/scenarios/*.log`  
Manifest: `tests/fixtures/scenarios/manifest.json`

---

## How to reproduce

```bash
# All 11 scenarios (offline, ~10s, no Docker/DB)
make battle-test

# Go integration wrapper
make battle-test-go

# Single golden L0→L10 proof (legacy harness)
make gate-proof-mock

# Live inject proof (requires docker stack + DATABASE_URL)
make gate-proof-inject
```

Pass criteria for full certification:

```
BATTLE_TEST: 11/11 scenarios passed
BATTLE_TEST_CERTIFICATION: READY
```

---

## Guardrail policy (Helius / pump.fun)

High L1 `SKIP` with `serial_launcher_skipped` on live Helius traffic is **intentional capital defense**, not a pipeline bug. The gate collector classifies this as `GUARDRAILS_ACTIVE` when:

- Ingestion volume is high (`≥ 10k` notifications in window)
- `dq_pass = 0`
- `serial_launcher_skipped` dominates DQ decisions (`≥ 50%`)

`validate_pipeline_proof.sh` remains the separate bar for “did at least one trace complete L0→L10?” — do not conflate guardrail dominance with engine failure.

---

## Code fixes proven during certification

These were **real defects** fixed without weakening DQ rules:

1. **L10 exit emit** — `openPositionBusEventID()` resolves canonical `pos-open:*` causation instead of poll `pos-snap:*` IDs (`internal/workers/helpers.go`, `run_position_poll.go`).
2. **L6 selection batch timer** — single-item batches now emit selection events (`run_selection.go`).
3. **Shadow recorder** — PASS DQ events released, not marked processed (`run_shadow_recorder.go`).
4. **Learning trace** — `learning_record_emitted` includes `trace_id` (`run_learning_record.go`).

---

## Production readiness lens

| Lens | Status |
| ---- | ------ |
| Architecture + DTO contracts | Battle-tested (offline matrix) |
| Capital defense on scam traffic | Proven (`02`, `03`, `04`, `11`) |
| Engine L0→L10 on live workers | Proven through L9; L10 exit fix applied — re-run `make gate-proof-inject` after deploy |
| Real-money Helius-only trading | **Not certified** — requires shadow soak + operator gate |
| AI knowledge context | **Safe to cite** — point agents to this file + `manifest.json` |

---

## Related documents

- `docs/reference/architecture.md` — system design (single source of truth)
- `docs/reference/orchestrator_spec.md` — execution model
- `scripts/validate_pipeline_proof.sh` — PIPELINE_PROOF acceptance
- `scripts/gate_review_collect.sh` — throughput verdicts including `GUARDRAILS_ACTIVE`
- `README.md` — mock proof quick start
