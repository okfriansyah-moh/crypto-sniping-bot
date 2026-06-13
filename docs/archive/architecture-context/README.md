# Architecture Context Chunks (Archived)

> **Status:** Archived supplementary extracts · **Canonical:** [`docs/reference/architecture.md`](../../reference/architecture.md)
>
> These files are layer-focused slices (0–13) from an earlier architecture generation.
> They are useful for **narrow agent sessions** (e.g. “read only Layer 3 edge detection”)
> but must not override `architecture.md` when the two disagree.

## File index

| File | Topic | Maps to `architecture.md` |
| ---- | ----- | ------------------------- |
| `0_system_definition.md` | Profit invariant, control framing | §0 |
| `1_global_control_loop.md` | Control loop, operational modes | §1, §7 |
| `2_system_backbone.md` | Event bus, workers | §2 |
| `3_data_quality_engine.md` | Layer 1 DQ | §3.1 |
| `4_feature_extraction.md` | Layer 2 features | §3.2 |
| `5_sniper_mode.md` | Edge discovery, sniper modes | §3.3 |
| `6_slippage_models.md` | L4 probability/slippage/latency | §3.4 |
| `7_edge_validation.md` | Layer 5 validation | §3.5 |
| `8_selection_engine.md` | Layer 6 selection | §3.6 |
| `9_capital_engine.md` | Layer 7 capital | §3.7 |
| `10_execution_engine.md` | Layer 8 execution | §3.8 |
| `11_position_engine.md` | Layer 9 positions | §3.9 |
| `12_learning_engine.md` | Layer 10 learning | §3.10 |
| `13_observability_finalization.md` | KPIs, observability | §4–5 |

## Former path

Moved from `docs/archive/architecture-context/` on 2026-06-13. Skills and agents reference the new path:
`docs/archive/architecture-context/<file>.md`.
