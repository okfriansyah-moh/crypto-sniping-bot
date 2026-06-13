# Operator Dashboard — Frontend Design (Brainstorm)

> **Status:** Mockup approved for review · **Date:** 2026-06-10  
> **Artifact:** `docs/mockups/operator-dashboard.html` (static HTML)

## Problem

Operators today use Telegram (`/status`, `/pnl`, `/pipeline`, `/dq`) and shell scripts (`gate_review_collect.sh`, `validate_phase2_acceptance.sh`). That works for power users but is opaque for beginners and splits monitoring across tools.

## Recommended approach: Read-only dashboard first (Approach A)

| Approach                           | Description                                       | Pros                                                             | Cons                                               |
| ---------------------------------- | ------------------------------------------------- | ---------------------------------------------------------------- | -------------------------------------------------- |
| **A — Read-only dashboard**        | Single-page app polling existing HTTP/DB surfaces | Fast to ship; no new write paths; matches Telegram read commands | Mode/kill still via Telegram initially             |
| B — Full control plane             | Dashboard replaces Telegram for all commands      | One UI for everything                                            | Higher risk; needs auth, audit, confirmation flows |
| C — Embedded Grafana/custom charts | Metrics only, no domain concepts                  | Great for ops teams                                              | Poor beginner UX; no pipeline funnel semantics     |

**Recommendation:** Start with **A** — mirror what `/status`, `/health`, `/pipeline`, and gate scripts already expose. Add write actions (mode, kill) only after read path is stable.

## Information architecture (single page, anchor nav)

1. **Overview** — mode, shadow/live, PnL, exposure, gate banner
2. **Pipeline L0–L10** — funnel counts (from `GetPipelineStats`)
3. **Positions** — open trades with TP/SL context
4. **Recent activity** — tail of structured log events
5. **Data quality** — reject breakdown (from `/dq`)
6. **Gate review** — §1.1 criteria + throughput verdict
7. **Mode & safety** — visual controls (phase 2; confirm via Telegram first)

## Technical fit (skeleton-parallel rules)

- **No module imports** — frontend is outside `app/modules/`; talks to orchestrator HTTP layer only
- **DTO-shaped API** — responses map to existing adapter queries, not raw SQL rows
- **Read-only v1** — no bypass of Telegram confirmation for destructive actions
- **Determinism display** — show `trace_id`, `strategy_version_id`, gate evidence timestamps

## Data sources (existing)

| UI section      | Backend source today                                    |
| --------------- | ------------------------------------------------------- |
| Overview        | `GetSystemState`, shadow gate evaluator, `/health`      |
| Pipeline funnel | `GetPipelineStats`, worker `stage_completed` heartbeats |
| Positions       | Position adapter (Telegram `/positions`)                |
| Gate review     | `gate_evidence_*.json` fields from collector script     |
| DQ              | `GetPipelineStats` / DQ decision aggregates             |

## Out of scope (YAGNI)

- Wallet key management in browser
- Live charting / TradingView embeds
- Multi-user RBAC (single operator v1)
- WebSocket log streaming (poll every 30s is enough for v1)

## Next step

Implemented: [`docs/plans/2026-06-13-operator-dashboard-plan.md`](../plans/2026-06-13-operator-dashboard-plan.md).
