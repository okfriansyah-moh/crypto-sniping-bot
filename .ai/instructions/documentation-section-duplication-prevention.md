Each concept or specification MUST have **one canonical location** across all `docs/` files. When multiple documents need to reference the same concept, use cross-references instead of duplicating content.

**MUST NOT:**

- Duplicate section content across `docs/` files — if two documents describe the same thing, one must cross-reference the other
- Repeat examples, tables, or ASCII diagrams that already exist in another document section
- Create parallel sections with overlapping scope (e.g., two "Status Display" sections covering the same output)
- Copy state definitions, pipeline stages, or agent pipeline descriptions across documents verbatim

**MUST:**

- Identify the **canonical document** for each concept using the Reference Documents table above
- Use cross-references: "See `docs/orchestrator_spec.md` § Failure Handling" instead of restating the rules
- When a document needs summarized context from another, keep it to a one-line summary + cross-reference
- Before adding a new section to any `docs/` file, verify no existing document already covers that topic
- Each document owns its domain — `architecture.md` owns system design, `orchestrator_spec.md` owns execution model, `PARALLEL_DEV.md` owns parallel development, etc.

**Canonical ownership:**

| Topic                        | Canonical Document               |
| ---------------------------- | -------------------------------- |
| System architecture & design | `docs/architecture.md`           |
| Pipeline execution model     | `docs/orchestrator_spec.md`      |
| DTO definitions & rules      | `docs/dto_contracts.md`          |
| Database adapter interface   | `docs/db_adapter_spec.md`        |
| Parallel development         | `docs/PARALLEL_DEV.md`           |
| Agent/skill system           | `docs/AGENTS_AND_SKILLS.md`      |
| Implementation phases        | `docs/implementation_roadmap.md` |
| Getting started              | `docs/STARTER_GUIDE.md`          |
| Progress tracking            | `docs/PROGRESS_REPORT.md`        |

---