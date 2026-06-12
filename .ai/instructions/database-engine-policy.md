- **The database engine is project-specific.** Choose the appropriate engine when setting up a new project.
- **Supported engines are configured via `database/adapter.*`.** See `docs/db_adapter_spec.md`.
- **Modules MUST remain database-agnostic.** No module may reference any specific database engine.
- Direct use of any database driver in `app/modules/` is forbidden.
- The adapter is the **sole abstraction boundary** — switching engines requires changes only in `database/`.

---