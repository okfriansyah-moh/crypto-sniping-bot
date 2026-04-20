# DTO Contracts

> Generated using `.github/prompts/dto.prompt.md`.
> Defines all DTOs with fields, types, constraints, and cross-module dependency mapping.

---

## 1. DTO Design Rules

All DTOs in this system MUST follow these rules:

### Structure

- All DTOs are **immutable** (language-specific: frozen dataclass, readonly interface, record class, etc.)
- No methods, no properties, no post-initialization logic
- All fields must have **type annotations**
- DTOs live exclusively in `contracts/` — never in modules

### Serialization

- JSON-serializable only: `str`, `int`, `float`, `bool`, `None`, `list`, `tuple`, nested DTOs
- **Forbidden types:** `datetime` (use ISO 8601 string), `Path` (use string), `bytes`, `set`, `complex`

### IDs

- All entity IDs are content-addressable: `SHA256(content_signature)[:16]`
- 16 hexadecimal characters
- Deterministic: same content = same ID

---

## 2. Versioning Rules

- **Additive only:** New fields may be added (with defaults for backward compatibility)
- **Never remove or rename** existing fields
- If a field must be renamed: add new field, deprecate old (make optional with `None` default)
- DTO changes merge to `main` BEFORE module changes that depend on them

---

## 3. DTO Definitions

<!-- Define your project's DTOs below. Copy this template for each DTO. -->

### PipelineRunDTO

| Field                | Type | Constraints                                             |
| -------------------- | ---- | ------------------------------------------------------- | ------------------ |
| run_id               | str  | UUID format, logging only                               |
| entity_id            | str  | 16 hex chars, SHA-256 derived                           |
| status               | str  | One of: started, processing, completed, partial, failed |
| last_completed_stage | str  | None                                                    | Stage name or None |
| created_at           | str  | ISO 8601 timestamp                                      |
| updated_at           | str  | ISO 8601 timestamp                                      |

- **Source file:** `contracts/pipeline.py`
- **Producer:** orchestrator
- **Consumers:** orchestrator (internal state tracking)

---

### EntityDTO

| Field      | Type | Constraints                                           |
| ---------- | ---- | ----------------------------------------------------- |
| entity_id  | str  | 16 hex chars, SHA-256 derived                         |
| name       | str  | Non-empty                                             |
| status     | str  | One of: created, queued, processed, completed, failed |
| created_at | str  | ISO 8601 timestamp                                    |

- **Source file:** `contracts/entity.py`
- **Producer:** stage_1 (initial processing)
- **Consumers:** all downstream stages

---

<!-- Add more DTOs following this pattern:

### YourDTOName

| Field       | Type          | Constraints              |
| ----------- | ------------- | ------------------------ |
| field_name  | str           | description              |

- **Source file:** `contracts/module_name.py`
- **Producer:** module_name
- **Consumers:** consumer_a, consumer_b

-->

---

## 4. Cross-Module Dependency Matrix

| DTO            | Producer  | Consumers             |
| -------------- | --------- | --------------------- |
| PipelineRunDTO | orchestr. | orchestrator          |
| EntityDTO      | stage_1   | stage_2, stage_3, ... |
| Stage1Result   | stage_1   | stage_2               |
| Stage2Result   | stage_2   | stage_3               |

---

## 5. Validation Rules

### Per-DTO Constraints

All constraints are enforced by the **dto-guardian agent** during validation:

- IDs: 16 hex characters, SHA-256 derived
- Status fields: must be one of the defined enum values
- Numeric ranges: scores in [0.0–1.0], durations positive
- String lengths: per-field maximum lengths
- Required fields: must not be None unless explicitly optional

### Cross-DTO Constraints

- Output DTO of stage N must type-match the input of stage N+1
- Entity IDs must be consistent across all DTOs for the same entity
- Timestamps must be ISO 8601 format

---

## 6. Anti-Patterns

```python
# ❌ Raw dict instead of DTO
result = {"entity_id": "abc123", "status": "done"}

# ❌ Mutable dataclass
@dataclass  # Missing frozen=True
class MyDTO: ...

# ❌ Logic in DTO
@dataclass(frozen=True)
class MyDTO:
    def validate(self): ...  # No methods allowed

    def __post_init__(self):
        # No validation logic here
        ...

# ❌ Cross-module type import
from app.modules.processing.internal import ProcessResult  # Forbidden

# ❌ Forbidden types
from pathlib import Path
from datetime import datetime

@dataclass(frozen=True)
class BadDTO:
    path: Path          # Use str instead
    created: datetime   # Use str (ISO 8601) instead
    data: bytes         # Forbidden
    tags: set           # Use tuple instead

# ✅ Correct DTO
from dataclasses import dataclass

@dataclass(frozen=True)
class GoodDTO:
    entity_id: str      # 16 hex chars
    name: str
    score: float        # [0.0–1.0]
    tags: tuple[str, ...]
    created_at: str     # ISO 8601
```
