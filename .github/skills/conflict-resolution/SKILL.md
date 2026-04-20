---
name: conflict-resolution
description: "Git merge conflict resolution for parallel development. Use when resolving merge conflicts between parallel phase branches. Provides union-merge strategy, file ownership rules, and resolution patterns."
---

# Conflict Resolution Skill

## Purpose

Resolve merge conflicts that arise from parallel development branches. Uses a union-merge strategy that preserves all implementations from each branch while respecting file ownership boundaries.

## Rules

### Resolution Strategy

- **Union merge** — combine code from both sides, never discard work
- **File ownership** — each phase owns specific directories; conflicts in owned files favor the owner
- **contracts/** — combine all DTO definitions from both branches (additive only)
- **app/modules/** — each module directory belongs to one phase; no overlap expected
- **app/orchestrator/** — later phase wins for wiring changes (imports + stage registration)
- **tests/** — combine all test files from both sides

### Protected Files During Merge

| Path          | Merge Rule                                    |
| ------------- | --------------------------------------------- |
| `contracts/*` | Union: keep all DTOs from both branches       |
| `database/*`  | Phase 0 branch wins — only Phase 0 may modify |
| `docs/*`      | Keep unchanged — neither branch should modify |
| `config/*`    | Union: combine new keys from both branches    |

### Conflict Markers

After resolution, **no conflict markers** may remain in any file:

- `<<<<<<<`
- `=======`
- `>>>>>>>`

## Inputs

- Two or more branches with conflicting changes
- File ownership matrix from `docs/implementation_roadmap.md`

## Outputs

- Clean merged branch with no conflict markers
- All implementations preserved from each branch
- Passing compilation and tests

## Examples

### Resolving contracts/ Conflicts

```python
# Branch A added:
@dataclass(frozen=True)
class StageAOutput:
    entity_id: str
    score: float

# Branch B added:
@dataclass(frozen=True)
class StageBOutput:
    entity_id: str
    result: str

# Resolution: Keep BOTH DTOs (both are new, no conflict)
```

### Resolving orchestrator/ Conflicts

```python
# Branch A wires:
from app.modules.stage_a import process as stage_a_process

# Branch B wires:
from app.modules.stage_b import process as stage_b_process

# Resolution: Keep BOTH imports, add both to pipeline sequence
```

## Checklist

- [ ] No conflict markers remain in any file
- [ ] All DTO definitions from both branches preserved
- [ ] Module directories have no cross-ownership conflicts
- [ ] Orchestrator imports all newly-wired stages
- [ ] All tests pass after merge
- [ ] `python3 -m py_compile` passes for all `.py` files
