The system runs in exactly one of four modes at any time:

- `STRICT` — conservative thresholds, low explore budget (≤1%)
- `BALANCED` — default operating mode
- `EXPLORATION` — relaxed thresholds, higher explore budget (3–5%), used for starvation recovery
- `VERY_EXPLORATION` — maximum relaxation; auto-entered when starvation persists in EXPLORATION

Mode transitions are **bounded**: one transition per window, auto-downgrade on starvation, auto-upgrade on rug/FP spike, manual override via `/mode` (logged, reversible). Values live in `config/` YAML.