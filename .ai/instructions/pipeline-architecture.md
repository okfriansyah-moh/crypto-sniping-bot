Stages execute in **strict sequential order** — never reorder, skip, or parallelize stages at runtime.

**Canonical pipeline** (per `docs/architecture.md` § 1):

```
DETECT → FILTER → SCORE → SELECT → EXECUTE → EXIT → EVALUATE → ADJUST
```

Mapped to the 10 layers (`docs/architecture.md` § 3):

```
Layer 0   Data Ingestion            (DEX events, new pool detection, MarketDataDTO)
Layer 0.5 Rescan Worker             (re-emit market_data_event at 14 age bands: 15m→48h; see § Rescan Worker)
Layer 1   Data Quality Engine       (reject manipulation, honeypots, rugs)
Layer 2   Feature Extraction        (normalized FeatureDTO + FeatureConfidence)
Layer 3   Signal & Edge Discovery   (NEW_LAUNCH_EDGE, adaptive momentum threshold)
Layer 4   Probability/Slippage/Latency Models
Layer 5   Edge Validation           (EV gate, adaptive thresholds)
Layer 6   Selection Engine          (Top-K greedy + diversity + exploration band)
Layer 7   Capital Engine            (size ∝ Score × P × Confidence, cohort multipliers)
Layer 8   Execution Engine          (wallet sharding, prebuilt calldata, bounded parallelism)
Layer 9   Position Engine           (TP1/TP2/SL/TIME, adaptive per cohort)
Layer 10  Learning Engine           (FP/FN, cohort analysis, bounded updates)
```

AI Enrichment cross-cuts layers 0/1/3/10 via `internal/ai/GroqClient` (1-shot, fail-open, model configurable via `AI_ENRICH_MODEL` env var — default `llama-3.3-70b-versatile`):

- **L0/1** `AINarrativeProbe` — narrative scoring 0–10, copy-paste / impersonation detection
- **L1 DQ** — soft risk bump (+0.30 copy-paste, +0.20 impersonation) via `NarrativeKnown` gate
- **L3** `applyNarrativeMultiplier` — ±10% `EdgeConfidence` from `NarrativeScore`
- **L10** `LossExplainer` — AI category + natural-language reason on `LearningRecordDTO`