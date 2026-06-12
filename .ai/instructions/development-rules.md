1. **Language & runtime** — Use the project's chosen language and version. Use type annotations on all public interfaces
2. **Immutable DTOs** for all contracts — no mutable state crossing module boundaries
3. **Each module** gets its own package under `app/modules/` with a public entry point exposing only the public contract
4. **No module may import another module's internals** — only `contracts/` types
5. **Database access** through `database/adapter.*` only — no raw SQL in modules, no ORM
6. **Tests** must be runnable without GPU, without network, and without real data files
7. **Config** via YAML files — no hardcoded paths, thresholds, or magic numbers
8. **Logging** via structured logging (language-appropriate library) — leveled, no unstructured console output

---