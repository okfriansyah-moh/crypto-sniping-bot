---
name: writing-plans
type: skill
description: >
  Implementation planning skill. Converts an approved design spec into a detailed,
  task-by-task implementation plan. Each task is small (2-5 minutes), has exact file
  paths, complete code guidance, and verification steps. Used after brainstorming,
  before subagent-driven-development or executing-plans.
---

# Writing Implementation Plans

Converts an approved design spec into a detailed, step-by-step implementation plan
that an agent (or junior developer) can follow without ambiguity.

---

## When to Use

After `brainstorming` has produced an approved spec. Before:

- `subagent-driven-development` (same-session subagent execution)
- `phase-builder` (parallel phase implementation)
- Any manual implementation of a spec

---

## Plan Format

Save plan to `docs/plans/YYYY-MM-DD-<topic>-plan.md`.

Each plan contains:

### Header

```markdown
# Implementation Plan: <topic>

Date: YYYY-MM-DD
Spec: docs/specs/YYYY-MM-DD-<topic>-design.md
Status: [ ] In Progress / [ ] Complete
```

### Task Block Template

```markdown
## Task N: <short description>

**Files:** `path/to/file.go` (create|modify)

**What:** One sentence describing the behavior to implement.

**How:**

- Exact function signatures to add/modify
- DTO fields to use/produce
- SQL to write (if any)
- Error conditions to handle

**Verification:**

- Unit test that confirms the behavior
- `go test ./...` passes (or language equivalent)
- No new lint errors

**Acceptance:**

- [ ] Behavior matches spec section N
- [ ] All verification steps pass
```

---

## Rules for Good Tasks

### Size

- Each task: **2-5 minutes** to implement
- If longer: split it
- If shorter: consider merging with the next task

### Independence

- Tasks must be sequential (each builds on prior)
- Do NOT design tasks that require parallel execution
- Each task must leave the codebase in a **compilable, testable state**

### Completeness

- Every task includes exact file paths — no ambiguity
- Code snippets for non-trivial logic (function signatures, key algorithms)
- Every task has a verification step — usually a specific test to write and pass

### skeleton-parallel Constraints

Apply to every task:

- Modules accept DTOs, return DTOs — no side effects
- All DB writes go through `database/adapter.*` — never directly in modules
- Content-addressable IDs: `SHA256(content)[:16]`
- Use `ON CONFLICT DO NOTHING` for all inserts
- Config values from `config/*.yaml` — never hardcoded

---

## Plan Self-Review

After writing the plan, check:

1. **Coverage:** Every spec requirement has ≥1 task
2. **Size:** No task is larger than 5 minutes
3. **Compilable states:** Every task leaves code compilable
4. **No skipped tests:** Every task has a test verification step
5. **DTO completeness:** All DTOs are defined before they're consumed
6. **DB access:** Only orchestrator tasks touch `database/adapter.*`

---

## Transition

After plan is complete, present to user:

> "Plan written to `<path>`. Ready to implement via `subagent-driven-development` or manually. Proceed?"

Wait for approval. Then invoke the appropriate execution skill.

---

## Checklist

- [ ] Plan saved to `docs/plans/`
- [ ] All spec requirements covered by tasks
- [ ] Tasks are 2-5 minutes each
- [ ] Each task has exact file paths
- [ ] Each task has a verification step
- [ ] No task violates skeleton-parallel module boundaries
- [ ] Plan self-review passed
- [ ] User approved plan
