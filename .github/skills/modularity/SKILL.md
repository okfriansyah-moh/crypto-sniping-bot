---
name: modularity
description: "Module boundary enforcement. Use when creating modules, reviewing imports, or validating the modular monolith architecture. Prevents cross-module imports, enforces package structure, and defines file ownership per phase."
---

# Module Boundary Skill

## Purpose

Enforce strict module isolation in the modular monolith. Prevents cross-module imports, ensures correct package structure, and validates file ownership during parallel development.

## Rules

### Module Package Structure

Every module follows this pattern:

```
app/modules/{module_name}/
├── __init__.py          # Public API — exports ONLY the entry function
├── {module_name}.py     # Core implementation
└── (internal helpers)   # Private, never imported externally
```

### Import Rules

```python
# ✅ ALLOWED — contracts are shared
from contracts.entity import EntityResult
from contracts.processed import ProcessedData

# ✅ ALLOWED — stdlib
import logging, subprocess, hashlib, os, json

# ❌ FORBIDDEN — cross-module import
from app.modules.stage_a.internal import process_data

# ❌ FORBIDDEN — database access in modules
import sqlite3
from database.adapter import DatabaseAdapter
```

### Public API Contract

Each module exposes exactly ONE entry function:

```python
# app/modules/processing/__init__.py
from .processing import process

def process(input_dto: InputDTO, config: dict) -> OutputDTO:
    ...
```

- Input: immutable DTOs from `contracts/`
- Output: immutable DTO from `contracts/`
- Config: dict from YAML (passed by orchestrator)
- No side effects visible to other modules
- No shared mutable state

## Inputs

- Module source code under `app/modules/`
- Import statements across the codebase

## Outputs

- Validated module boundaries with no cross-module violations
- Correct `__init__.py` exports

### What Can Be Shared

| Package             | Who Can Import     | Contains                            |
| ------------------- | ------------------ | ----------------------------------- |
| `contracts/`        | All modules        | Frozen dataclass DTOs only          |
| `config/`           | Orchestrator only  | YAML config files                   |
| `database/`         | Orchestrator only  | DB adapter + engine implementations |
| `app/orchestrator/` | `app/main.py` only | Pipeline sequencing                 |

**Modules may NOT import from:**

- Other `app/modules/*` packages
- `app/orchestrator/`
- `database/`
- Any database driver directly

## Examples

### File Ownership Pattern

| Phase   | Owned Directories                     | DO NOT TOUCH   |
| ------- | ------------------------------------- | -------------- |
| Phase 0 | `database/`, `config/`, `app/main.py` | `app/modules/` |
| Phase N | `app/modules/{stage_name}/`           | Other modules  |

### Anti-Patterns

```python
# ❌ Module reads another module's output file directly
with open(f"output/{entity_id}/data.json") as f:
    data = json.load(f)

# ✅ Module receives data via DTO from orchestrator
def process(input_dto: InputDTO, config: dict) -> OutputDTO:
    ...

# ❌ Module calls another module
from app.modules.stage_a import process as stage_a_process
result = stage_a_process(data, config)
```

## Checklist

- [ ] No imports from `app.modules.*` in any module (only `contracts.*`)
- [ ] Module `__init__.py` exports only the public entry function
- [ ] No global mutable state
- [ ] No direct file reads from another module's output directory
- [ ] Config values passed in, not read directly from YAML inside module
- [ ] Database access is NOT performed inside modules
