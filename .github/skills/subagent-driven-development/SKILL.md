---
name: subagent-driven-development
type: skill
description: >
  Execute implementation plans by dispatching a fresh subagent per task with
  two-stage review (spec compliance, then code quality) after each. Faster
  iteration than manual execution, with isolated context per task.
---

# Subagent-Driven Development

Execute a plan by dispatching fresh subagents per task, with two-stage review after each.

**Core principle:** Fresh subagent per task + spec-compliance review first + code-quality review second = high quality, fast iteration with isolated context.

---

## When to Use

- You have an implementation plan (from `writing-plans` skill)
- Tasks are mostly independent (sequential, not tightly coupled)
- You want quality gates without manual review loops
- Use `phase-builder` instead when tasks should run in parallel sessions

---

## The Process

### 1. Setup

- Load the plan from `docs/plans/`
- Extract ALL tasks upfront (read plan once, don't re-read per task)
- Create a todo list with all tasks

### 2. Per-Task Loop

```
For each task:
  1. Dispatch implementer subagent with:
     - Full task text from plan
     - Relevant context (DTO types, module structure, adjacent files)
     - skeleton-parallel constraints reminder

  2. If subagent asks questions → answer fully before it proceeds

  3. Subagent implements, writes tests, verifies they pass, self-reviews

  4. Dispatch spec-compliance reviewer subagent:
     - Did implementer cover all requirements?
     - Did implementer add anything not in the spec? (remove it)
     - Review again until ✅

  5. Dispatch code-quality reviewer subagent:
     - Module boundaries respected? (no cross-module imports)
     - DTOs used correctly? (immutable, from contracts/)
     - No hardcoded values? (all from config/)
     - Tests present and meaningful?
     - Review again until ✅

  6. Mark task complete in todo list
```

### 3. Final Review

After all tasks complete, dispatch a final reviewer for the full implementation.

---

## Subagent Context to Provide

**Always include:**

- Full task text (copy from plan, don't summarize)
- Relevant DTO types from `contracts/`
- The module's public interface
- skeleton-parallel constraints (see below)

**skeleton-parallel constraints reminder for subagents:**

```
- Modules are pure functions: accept DTOs, return DTOs, no side effects
- No DB access in modules — orchestrator only calls database/adapter.*
- Import only from contracts/ — no cross-module imports
- Content-addressable IDs: SHA256(content)[:16]
- ON CONFLICT DO NOTHING for all inserts
- Config from config/*.yaml — never hardcode values
```

---

## Model Selection

| Task Type                                                   | Model              |
| ----------------------------------------------------------- | ------------------ |
| Mechanical implementation (isolated, 1-2 files, clear spec) | Fast/cheap model   |
| Multi-file coordination, pattern matching                   | Standard model     |
| Architecture, design decisions, final review                | Most capable model |

---

## Implementer Status Handling

| Status               | Action                                                                                         |
| -------------------- | ---------------------------------------------------------------------------------------------- |
| `DONE`               | Proceed to spec-compliance review                                                              |
| `DONE_WITH_CONCERNS` | Read concerns, assess severity, then review                                                    |
| `NEEDS_CONTEXT`      | Provide missing context, re-dispatch same task                                                 |
| `BLOCKED`            | Diagnose: context gap → provide context; too large → split task; wrong plan → escalate to user |

**Never:** ignore escalations or force retry without changes.

---

## Review Loop Rules

- Spec compliance BEFORE code quality (wrong order = wasted review)
- Reviewer finds issues → implementer fixes → reviewer reviews again (loop until ✅)
- Never skip a review stage
- Never accept "close enough" on spec compliance

---

## Advantages vs. Manual Execution

| Manual                           | Subagent-Driven             |
| -------------------------------- | --------------------------- |
| Context accumulates across tasks | Fresh context per task      |
| Human reviews each piece         | Two automated review stages |
| Easy to drift from spec          | Spec compliance enforced    |
| Sequential attention             | Focused subagent per task   |

---

## Red Flags — Stop and Reassess

- Starting implementation without an approved plan
- Dispatching multiple implementation subagents in parallel (causes conflicts)
- Skipping either review stage
- Proceeding while a reviewer has open issues
- Making subagents read the full plan file (provide full task text instead)
- Letting "self-review" replace the actual code-quality review

---

## Checklist

- [ ] Plan loaded and all tasks extracted upfront
- [ ] Todo list created with all tasks
- [ ] Each task dispatched with full task text + context
- [ ] Spec-compliance review completed per task
- [ ] Code-quality review completed per task
- [ ] All review issues resolved before moving to next task
- [ ] Final review completed for full implementation
- [ ] All tasks marked complete
