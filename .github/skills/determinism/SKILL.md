---
name: determinism
description: "Determinism enforcement. Use when implementing or reviewing code to ensure same input + same config = identical output. Detects randomness, non-deterministic patterns, and sorting violations."
---

# Determinism Enforcement Skill

## Purpose

Guarantee that the pipeline produces identical output for identical input on every machine, every run. Detects and prevents randomness, non-deterministic patterns, and sorting violations.

## Rules

### Core Invariant

> Same input + same config = identical output. Always. On every machine, on every run.

### Forbidden Patterns

1. **Random number generation** — No `import random`, `random.choice()`, `uuid.uuid4()` for IDs
2. **Time-dependent logic** — No `datetime.now()` as decision input (logging timestamps OK)
3. **Non-deterministic iteration** — No iterating over `set()` without sorting
4. **Network-dependent decisions** — No behavior changes based on network state
5. **Float comparison without tolerance** — Use `abs(a - b) < 1e-9`

### ID Generation

All IDs must be **content-addressable** — derived from content, not from timestamps or random values:

| ID          | Formula                                  | Deterministic?                    |
| ----------- | ---------------------------------------- | --------------------------------- |
| `entity_id` | `SHA256(content_signature)[:16]`         | ✅ Same content = same ID         |
| `item_id`   | `SHA256(entity_id + unique_fields)[:16]` | ✅ Same entity + fields = same ID |
| `run_id`    | UUID (logging only)                      | ⚠️ Allowed — not a decision input |

### Sorting Rules

All sorted collections must have **deterministic tiebreakers**:

```python
# ❌ INSUFFICIENT — ties are arbitrary
items.sort(key=lambda x: x.score, reverse=True)

# ✅ CORRECT — tiebreaker ensures deterministic order
items.sort(key=lambda x: (-x.score, x.created_at))
```

## Inputs

- Module source code under `app/modules/`
- Any code that produces output or makes decisions

## Outputs

- Validated deterministic code with no randomness violations

## Examples

```python
# ❌ FORBIDDEN
import random
random.choice(items)
uuid.uuid4()  # Non-deterministic UUID

# ✅ CORRECT
items[index % len(items)]  # Deterministic rotation
sorted(data, key=lambda x: x.sort_key)  # Deterministic order
```

### Template/Selection Determinism

```python
# ❌ FORBIDDEN
template = random.choice(TEMPLATES)

# ✅ CORRECT — index-based rotation
template = TEMPLATES[item_index % len(TEMPLATES)]
```

## Checklist

- [ ] No `import random` or `random.` calls
- [ ] No `uuid.uuid4()` for IDs (only for `run_id` logging)
- [ ] No `datetime.now()` as logic input (only logging timestamps)
- [ ] No `set()` iteration without sorting
- [ ] All sorts have deterministic tiebreakers
- [ ] All IDs are SHA-256 content-addressable
- [ ] Selection is index-based, not random
