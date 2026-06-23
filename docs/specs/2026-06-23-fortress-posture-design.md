# Fortress Posture Design (A + B + C)

> **Date:** 2026-06-23  
> **Status:** Approved for implementation  
> **Canonical for:** Fortress sniper operating posture, phase gates, config change policy  
> **Related:** [`docs/plans/2026-06-10-profit-restoration-plan.md`](../plans/2026-06-10-profit-restoration-plan.md), [`docs/guides/HELIUS_WEBHOOK_SETUP.md`](../guides/HELIUS_WEBHOOK_SETUP.md)

---

## 1. Posture matrix

| Layer | A Fortress (hold firm) | B Selective speed | C Platform |
|-------|------------------------|-------------------|------------|
| L0 | Valid-mint ratio, no WSOL poison | Hybrid pumpfun webhook + stream | Per-program heartbeat in dashboard |
| L1 | Mandatory hard-rejects, fail-closed probes | EXPLORATION serial-launcher shadow widen | DQ + probe completeness UI |
| L2 | Cold-start confidence honesty | Rescan/graduation liquidity scoring fix | Feature score distribution |
| L3 | `min_liquidity_score: 0.55` at birth | GRADUATION_EDGE + rescan alpha; floor 0.58 for non-birth | Edge reject-reason breakdown |
| L5–L7 | EV gate + capital caps | Tip-aware EV (shadow first) | Unified readiness banner |
| L8 | Wallet sharding, idempotency | Jito bundles exit shadow after gate pass | Executions trail view |
| L10 | 5–10% bounded updates | Shadow FN observer | Gate collect from dashboard |

---

## 2. Non-goals

- EVM multi-market expansion until Solana L0→L10 shadow trace is green
- Live AI narrative on hot path
- Global `delivery: webhook` for all programs (requires gap recovery)
- Birth-time liquidity score inflation (identical 30 SOL virtual reserves stay blocked)

---

## 3. Config change policy — never relax

1. L1 mandatory hard-rejects: serial launcher, no social, high supply, fail-closed unknown probes
2. Birth-time L3 liquidity block (`liquidity_score ≤ 0.55` on identical virtual reserves)
3. `execution.jito.shadow_mode: true` until gate `SHADOW_READY`
4. Bounded L10 learning (Δ ≤ 5–10%, N ≥ 30–50)

---

## 4. Phase gates

See [`fortress_posture_integration` plan](../../.cursor/plans/) for full phase breakdown. Exit criteria reference profit-restoration plan § Phase 2 success criteria where applicable.

| Phase | Exit |
|-------|------|
| 1 | `edge_worker emitted ≥ 1` from rescan or graduation; `validation_worker emitted ≥ 1` |
| 2 | `ingestion_delivery_mode=hybrid`, webhook + stream emits, `wsol_token_address_emitted=0` |
| 3 | `traces_completed ≥ 1`, `shadow_observer_failed=0`, Jito tip shadow logged |
| 4 | Dashboard posture banner + ingestion + executions without log tail |

### Phase 3 operator runbook (shadow trace)

Before any live capital flip:

```bash
make gate-collect MINS=120 MODE=SHADOW_TRADING SVC=sniper-bot
```

Exit when evidence shows `traces_completed ≥ 1`, `shadow_observer_failed=0`, `learning_records ≥ 1`, and `jito_bundle_shadow` events in logs. Keep `execution.jito.shadow_mode: true` until `PRODUCTION_DECISION=SHADOW_READY` or better.

---

## 5. June 2026 market context

- Pump.fun graduation rate ~0.26% — primary alpha is graduation + rescan survivors, not birth snipes
- Macro risk-off (hawkish Fed, ETF outflows) — optional `MACRO_REGIME=risk_off` capital throttle
- Jito tip auction dominates Solana inclusion — EV gate must include tip + priority fee
