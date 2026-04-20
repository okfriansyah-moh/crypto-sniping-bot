# DTO Contracts Prompt

You are a Staff+ backend architect. Generate a complete `docs/dto_contracts.md`.

## Instructions

1. Read `docs/architecture.md` — pipeline stages and DTO registry
2. Read `.github/copilot-instructions.md` — DTO enforcement rules

## Document Structure

Generate `docs/dto_contracts.md` with:

### 1. DTO Design Rules

- All DTOs are `@dataclass(frozen=True)`
- No methods, no properties, no `__post_init__` logic
- All fields typed (PEP 484)
- JSON-serializable only: `str`, `int`, `float`, `bool`, `None`, `list`, `tuple`, nested DTOs
- Forbidden types: `datetime`, `Path`, `bytes`, `set`, `complex`

### 2. Versioning Rules

- Additive only: new fields may be added (with defaults)
- Never remove or rename existing fields
- DTO changes merge to `main` BEFORE module changes that depend on them

### 3. DTO Definitions

For each DTO in the system:

```markdown
#### DTOName

| Field     | Type | Constraints           |
| --------- | ---- | --------------------- |
| entity_id | str  | 16 hex chars, SHA-256 |
| ...       | ...  | ...                   |

- **Source file:** `contracts/module_name.py`
- **Producer:** module_name
- **Consumers:** module_a, module_b
```

### 4. Cross-Module Dependency Matrix

Table showing which module produces/consumes which DTO.

### 5. Validation Rules

Per-DTO constraint enforcement:

- ID formats
- Value ranges
- Required vs optional fields
- Relationship constraints

### 6. Anti-Patterns

Common mistakes with examples:

- Raw dicts instead of DTOs
- Mutable dataclasses
- Logic inside DTOs
- Cross-module type imports

## Output

Write the completed contracts to `docs/dto_contracts.md`.
