# crypto-sniping-bot

A deterministic, modular monolith pipeline built on the skeleton-parallel framework.

## Quick Start

```bash
# Build
go build ./...

# Run tests
go test ./...

# Start server
go run ./cmd serve

# Run migrations
go run ./cmd migrate up
```

## Structure

```
crypto-sniping-bot/
├── cmd/                    # Entry points (serve, migrate)
├── contracts/              # Immutable DTO definitions (inter-module contracts)
├── database/               # DB adapter interface + migrations
├── internal/
│   ├── app/                # Application wiring (config, web server)
│   └── modules/            # Domain modules (vertical slices)
│       └── health/         # Reference health module
├── config/                 # YAML configuration (phases.yaml)
├── scripts/                # run_parallel.sh — parallel development orchestrator
├── docs/                   # Architecture specs and implementation roadmap
└── output/                 # Generated artifacts (gitignored)
```

## Parallel Development

```bash
# Sequential mode (default)
./scripts/run_parallel.sh --mode sequential

# Parallel mode (multiple agents)
./scripts/run_parallel.sh --mode parallel

# Single phase
./scripts/run_parallel.sh --mode single --phase 0
```

## Architecture

See [`docs/architecture.md`](docs/architecture.md) for the system architecture and pipeline design.

## Configuration

All parameters live in `config/`. Edit `config/phases.yaml` to define implementation phases.

## Module Rules

- Modules communicate only through immutable DTOs in `contracts/`
- No module imports another module's internals
- All database access goes through `database.Adapter` — no direct driver imports in modules
- See `docs/architecture.md` for full architectural constraints
