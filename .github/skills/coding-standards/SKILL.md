---
name: coding-standards
description: "Enforce clean code standards: naming conventions, function/module structure, formatting, error handling, documentation, language-specific idioms. Use when writing new code, reviewing code quality, or auditing modules for readability and maintainability."
---

# Coding Standards

## When to Use

- Writing any new module, adapter, or service code
- Reviewing code for readability, consistency, and maintainability
- Auditing existing modules for coding standard violations
- Onboarding new patterns into the codebase

---

## 1 — Naming Conventions

### Rules

| Element         | Convention                                            | Example                                  |
| --------------- | ----------------------------------------------------- | ---------------------------------------- |
| Variables       | `snake_case` (Python/Rust), `camelCase` (Go/TS/Java)  | `entity_count`, `entityCount`            |
| Functions       | `snake_case` verb (Py/Rust), `camelCase` (Go/TS/Java) | `compute_score()`, `computeScore()`      |
| Classes/Structs | `PascalCase`                                          | `PipelineRunner`, `HealthService`        |
| Constants       | `UPPER_SNAKE_CASE`                                    | `MAX_RETRIES`, `DEFAULT_PORT`            |
| DTOs            | `PascalCase` (+ suffix per convention)                | `PipelineRunDTO`, `EntityResult`         |
| Modules/Files   | `snake_case`                                          | `edge_validator.py`, `health_service.go` |
| Private members | Language convention (`_prefix` Py, `unexported` Go)   | `_compute_internal()`, `computeInternal` |
| Type aliases    | `PascalCase`                                          | `EntityList`, `ConfigMap`                |
| Interfaces      | `PascalCase` (+ `I` prefix or `-er` suffix in Go)     | `HealthPort`, `Processor`                |

### Naming Anti-Patterns (FORBIDDEN)

- Single-letter variables (except `i`, `j`, `k` in short loops, `e`/`err` in error handling)
- Hungarian notation (`strName`, `lstItems`, `intCount`)
- Redundant type in name (`name_string`, `items_list`, `countInt`)
- Generic names without qualifier (`data`, `info`, `temp`, `result`, `value`)
- Negated booleans (`is_not_valid` → use `is_valid` and negate at call site)
- Abbreviated names that lose meaning (`ve`, `proc`, `mgr` — spell it out)

### Good vs Bad Examples

```
# GOOD — descriptive, intention-revealing names
validated_entity = validate_entity(raw_entity, config)
is_pipeline_active = check_pipeline_status(run_id)
retry_delay_ms = get_retry_delay(attempt_count)

# BAD — abbreviated, ambiguous, or misleading
ve = val(re, cfg)
flag = check(rid)
x = get_rd(ac)
```

---

## 2 — Function Design

### Size & Responsibility

```
# GOOD — single responsibility, clear contract
def compute_content_hash(content: str, algorithm: str = "sha256") -> str:
    """Compute content-addressable hash for idempotency."""
    if not content:
        return ""
    return hashlib.new(algorithm, content.encode()).hexdigest()[:16]

# BAD — does too many things, unclear boundaries
def process(data, config, db):
    # fetches, validates, computes, stores, logs — all in one
    ...
```

### Function Rules

| Rule                      | Threshold                          |
| ------------------------- | ---------------------------------- |
| Max lines per function    | 30 lines (aim for 15-20)           |
| Max parameters            | 5 (use DTO/struct if more)         |
| Max nesting depth         | 3 levels (use early returns)       |
| Max cyclomatic complexity | 10 per function                    |
| Return type               | Always annotated (typed languages) |
| Docstring/comment         | Required for public functions      |

### Early Returns (Guard Clauses)

```
# GOOD — flat structure, easy to follow
func validate(entity EntityDTO, config Config) bool {
    if entity.ID == "" {
        return false
    }
    if entity.Score < config.MinScore {
        return false
    }
    if entity.Status != "active" {
        return false
    }
    return true
}

# BAD — deep nesting, hard to follow
func validate(entity EntityDTO, config Config) bool {
    if entity.ID != "" {
        if entity.Score >= config.MinScore {
            if entity.Status == "active" {
                return true
            }
        }
    }
    return false
}
```

---

## 3 — Module & File Structure

### File Organization (Language-Agnostic Pattern)

```
1. Module/package docstring — one sentence describing purpose
2. Standard library imports
3. Third-party imports (only in adapters/database layers)
4. Project imports (contracts, core, shared utilities)
5. Constants
6. Type aliases (if needed)
7. Public classes/functions (exported API)
8. Private helpers (internal implementation)
```

### Import Rules

1. Standard library first
2. Third-party second (only in `database/`, adapters)
3. Project imports third (`contracts/`, same-module)
4. Blank line between each group
5. **NEVER** wildcard imports — explicit imports only
6. **NEVER** import another module's internals — only `contracts/`

---

## 4 — Error Handling

### Principles

```
# GOOD — specific errors, meaningful context
try:
    result = adapter.execute(command)
except ConnectionError as e:
    logger.error("adapter_connection_failed", extra={
        "adapter": command.adapter_name,
        "error": str(e),
    })
    raise
except ValueError as e:
    logger.warning("invalid_command_rejected", extra={
        "command_id": command.id,
        "reason": str(e),
    })
    return None

# BAD — bare except, swallows errors silently
try:
    result = adapter.execute(command)
except:
    pass
```

### Error Handling Rules

| Rule                                  | Enforcement                                      |
| ------------------------------------- | ------------------------------------------------ |
| Never bare `except:`/`catch`          | Always catch specific exception/error types      |
| Never silently swallow errors         | Log or re-raise — never ignore                   |
| Exceptions for exceptional cases only | Don't use exceptions for control flow            |
| Error context in structured logs      | Include entity_id, stage, module, relevant state |
| Fail fast at system boundaries        | Validate early, reject invalid input at entry    |
| Wrap errors with context              | Add module/stage context when propagating errors |

---

## 5 — Language-Specific Idioms

### Python

```python
# Prefer comprehensions over manual loops (when simple)
valid_items = [item for item in items if item.status == "active"]

# Context managers for resources
with open(path) as f:
    data = f.read()

# Type hints on all function signatures
def compute_hash(content: str, prefix: str = "") -> str:
    return hashlib.sha256(content.encode()).hexdigest()[:16]

# NEVER mutable default arguments
def bad(items: list = []):  ...  # FORBIDDEN
def good(items: list | None = None):  # Correct
    items = items or []
```

### Go

```go
// Prefer early returns over else chains
func process(input InputDTO) (OutputDTO, error) {
    if input.ID == "" {
        return OutputDTO{}, fmt.Errorf("empty ID")
    }
    // happy path continues...
}

// Use structured logging (log/slog)
slog.Info("entity_processed", "entity_id", entity.ID, "stage", "validation")

// Errors are values — always check them
result, err := adapter.Execute(ctx, cmd)
if err != nil {
    return fmt.Errorf("execute failed: %w", err)
}
```

### TypeScript

```typescript
// Use readonly for DTO fields
interface EntityResult {
  readonly entityId: string;
  readonly status: string;
  readonly score: number;
}

// Prefer strict null checks
function process(input: InputDTO): OutputDTO | null {
  if (!input.entityId) {
    return null;
  }
  // ...
}
```

### Rust

```rust
// Use Result for fallible operations
fn process(input: &InputDTO) -> Result<OutputDTO, ProcessError> {
    if input.id.is_empty() {
        return Err(ProcessError::InvalidInput("empty ID".into()));
    }
    // ...
}

// Prefer pattern matching over if-else chains
match entity.status.as_str() {
    "active" => process_active(entity),
    "pending" => process_pending(entity),
    _ => Err(ProcessError::UnknownStatus(entity.status.clone())),
}
```

### Java

```java
// Use records for DTOs (Java 16+)
public record EntityResult(
    String entityId,
    String status,
    double score
) {}

// Use Optional instead of null returns
public Optional<EntityResult> findEntity(String id) {
    if (id == null || id.isBlank()) {
        return Optional.empty();
    }
    // ...
}
```

---

## 6 — Code Smells to Reject

| Smell                     | Detection                                         | Fix                                      |
| ------------------------- | ------------------------------------------------- | ---------------------------------------- |
| God function              | >30 lines, >3 responsibilities                    | Extract into focused functions           |
| Deep nesting              | >3 indentation levels                             | Guard clauses, extract helper            |
| Magic numbers             | Unnamed numeric literals                          | Named constants or config values         |
| Boolean parameters        | `doThing(data, true, false)`                      | Separate functions or Enum               |
| Long parameter list       | >5 parameters                                     | Group into DTO or config struct          |
| Dead code                 | Unreachable branches, commented-out code          | Delete it                                |
| Duplicate logic           | Same pattern in 3+ places                         | Extract shared utility                   |
| Stringly-typed            | Magic strings for state (`"pending"`, `"active"`) | Enum or Literal/const type               |
| Premature abstraction     | Generic framework for 1 use case                  | Inline until 3+ uses justify abstraction |
| Inconsistent return types | Sometimes returns value, sometimes nil/null       | Explicit Optional or raise/error         |

---

## 7 — Logging Standards

### Required

- Use structured logging (language-appropriate library) — **NEVER** `print()`, `console.log()`, `println!()`
- Log levels: `DEBUG` for trace, `INFO` for progress, `WARNING` for recoverable issues, `ERROR` for failures
- Include contextual fields: `entity_id`, `stage`, `module`, relevant state

### Forbidden

- Unstructured console output in modules or database layers
- Logging sensitive data (credentials, tokens, PII)
- Log-and-swallow (logging an error then ignoring it)

---

## Constraints Summary

- ALWAYS follow the language's naming convention consistently
- ALWAYS annotate return types on public functions
- ALWAYS use guard clauses instead of deep nesting
- NEVER use bare exception/error catches
- NEVER use mutable default arguments (Python)
- NEVER leave magic numbers without named constants
- NEVER exceed 30 lines per function without strong justification
- NEVER use `print()`/`console.log()` — use structured logging
- ALWAYS maintain import order: stdlib → third-party → project
- NEVER import another module's internals — only `contracts/` types
