**MUST NOT:**

- Create duplicate files with similar names (e.g., `utils.py` and `helpers.py` with overlapping functions)
- Create new utility modules when existing ones already cover the functionality
- Duplicate DTO definitions — all DTOs live in `contracts/` and are defined exactly once
- Copy SQL schemas between migration files — reference the existing table, don't redefine it
- Duplicate configuration defaults — all defaults live in `config.yaml`, not scattered in code
- Create wrapper modules that simply re-export another module's functions

**MUST:**

- Check existing files before creating new ones — use the project structure as the source of truth
- Reuse existing utility functions from `contracts/`, `core/`, and shared helpers
- Place new code in the correct existing module rather than creating a parallel file
- When adding a new module, verify no existing module already handles that responsibility
- Keep one canonical location for each piece of logic — no copies, no forks, no alternatives

---