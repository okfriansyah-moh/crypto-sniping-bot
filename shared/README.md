# Shared kernel

Cross-deployable packages used by **sniper-bot**, **backend-dashboard**, and database tooling.

| Package | Import path | Purpose |
|---------|-------------|---------|
| `contracts/` | `crypto-sniping-bot/shared/contracts` | Immutable DTOs — event bus payloads, operator API shapes |
| `database/` | `crypto-sniping-bot/shared/database` | DB adapter, migrations, engine implementations |
| `config/` | *(filesystem only)* | YAML thresholds and tunables — sniper source of truth |

**Not duplicated** into each deploy unit: one migration chain, one DTO registry, one adapter boundary.

Go code shared across apps (operator queries, bootstrap, health) lives at repo root in `internal/`.

Docker images copy `shared/config/` → `/app/config/` so `CONFIG_PATH=/app/config/pipeline.yaml` is unchanged.
