All adaptive updates are non-negotiably:

- **Bounded** — `Δparameter ≤ 5–10% per cycle`
- **Sample-gated** — require `N ≥ 30–50` before update
- **Versioned** — every change bumps `config_version` with snapshot
- **Rollback-able** — revert if performance degrades
- **Single-family per cycle** — never tune multiple parameter families simultaneously (prevents oscillation)
- **Must store rejected shadow trades** in `LearningRecord` — without them false negatives cannot be computed