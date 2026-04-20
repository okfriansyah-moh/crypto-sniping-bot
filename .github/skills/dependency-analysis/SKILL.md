---
name: dependency-analysis
type: skill
description: "Dependency graph validation. Use when reviewing imports, detecting circular dependencies, measuring coupling metrics, or enforcing module boundary rules."
---

## Purpose

Analyze and enforce dependency rules across the codebase. Detect circular dependencies, forbidden imports, and coupling violations that break the modular monolith architecture.

---

## Rules

### Import Rules

1. **Modules MUST NOT import other modules' internals** — only `contracts/` types
2. **Modules MUST NOT import database drivers** — all DB access through `database/adapter.*`
3. **No circular dependencies** — if A imports B, B must not import A (even transitively)
4. **Features MUST NOT import other features** — use port interfaces for cross-feature communication
5. **Only the orchestrator imports modules** — modules are leaf nodes in the dependency graph

### Allowed Import Graph

```
orchestrator → modules → contracts
orchestrator → database/adapter
orchestrator → config

modules → contracts (ONLY)
modules ✗ modules
modules ✗ database
modules ✗ orchestrator
```

### Coupling Metrics

| Metric                   | Threshold | Action                               |
| ------------------------ | --------- | ------------------------------------ |
| Afferent coupling (Ca)   | ≤ 5       | Module has too many dependents       |
| Efferent coupling (Ce)   | ≤ 3       | Module depends on too many others    |
| Instability (Ce/(Ca+Ce)) | 0.3-0.7   | Balanced between stable and flexible |
| Circular dependencies    | 0         | Must be zero, always                 |

### Detection Commands

**Python:**

```bash
# Check for forbidden imports
grep -rn "from app.modules.*import" app/modules/ | grep -v "from app.modules.<current_module>"
grep -rn "import database\|from database" app/modules/
```

**Go:**

```bash
# Check for forbidden imports
grep -rn "\"<project>/app/internal/modules/" app/internal/modules/ | grep -v "<current_module>"
```

**TypeScript:**

```bash
# Check for forbidden imports
grep -rn "from.*modules/" src/modules/ | grep -v "<current_module>"
```

---

## Checklist

```
[ ] No cross-module imports (only contracts/)
[ ] No database imports in modules
[ ] No circular dependencies
[ ] Orchestrator is the only module caller
[ ] Port interfaces used for inter-module communication
[ ] Features don't import other features
[ ] Dependency graph is a DAG (directed acyclic graph)
[ ] Coupling metrics within thresholds
```

---

## Anti-Patterns

| Pattern                                  | Problem                | Fix                                |
| ---------------------------------------- | ---------------------- | ---------------------------------- |
| `from app.modules.other_module import X` | Cross-module coupling  | Use contracts/ DTOs                |
| `import sqlite3` in a module             | Direct DB access       | Use database/adapter               |
| Module A → B → C → A                     | Circular dependency    | Extract shared logic to contracts/ |
| Importing implementation types           | Tight coupling         | Import interfaces/protocols only   |
| God module that imports everything       | Central coupling point | Split into focused modules         |
