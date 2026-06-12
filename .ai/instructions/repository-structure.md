```
skeleton-parallel/
├── app/
│   ├── main.*               # Single entry point (language-specific)
│   ├── modules/             # Domain modules (one package per stage)
│   │   ├── module_a/
│   │   ├── module_b/
│   │   └── ...
│   └── orchestrator/        # Pipeline orchestration + checkpointing
├── contracts/               # Immutable DTO definitions
├── database/                # DB adapter + engine implementations + migrations
├── config/                  # YAML configuration
├── tests/                   # Unit + integration tests
├── output/                  # Generated artifacts (gitignored)
├── docs/                    # Architecture + specs
├── scripts/                 # Automation scripts
└── .github/                 # Agent + skill + prompt definitions
```

**Placement rules:**

- New module logic goes in the appropriate `app/modules/` subdirectory
- New DTO definitions go in `contracts/` — never duplicate in a module
- Database migrations go in `database/migrations/`
- Tests mirror the `app/modules/` structure under `tests/`
- Configuration defaults go in `config/` YAML files — never hardcode
- Never put module-specific logic in `app/orchestrator/` or `contracts/`

---