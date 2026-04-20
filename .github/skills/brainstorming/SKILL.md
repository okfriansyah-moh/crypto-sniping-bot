---
name: brainstorming
type: skill
description: >
  Pre-implementation design skill. You MUST use this before any creative work —
  creating features, building pipeline stages, adding modules, or modifying
  architecture. Explores user intent, requirements, and design before implementation.
---

# Brainstorming: Ideas Into Designs

<HARD-GATE>
Do NOT write any code, scaffold any module, or take any implementation action until
you have presented a design and the user has approved it. This applies to EVERY
request regardless of perceived simplicity.
</HARD-GATE>

---

## When to Use

**Always before:**

- Creating a new pipeline stage or module
- Adding a significant feature or behavior change
- Modifying architecture or DTO contracts
- Designing a new agent or skill

**Anti-pattern:** "This is too simple to need a design."
Even simple modules benefit from a one-paragraph design. Unexamined assumptions in "simple" work cause the most wasted effort.

---

## Process

### Step 1 — Explore project context

Check existing files, `docs/architecture.md`, recent changes, and current module structure before proposing anything.

### Step 2 — Ask clarifying questions

One question at a time. Understand: purpose, constraints, success criteria.

- Prefer multiple-choice questions when possible
- Focus on: what it does, how it fits in the pipeline, what DTOs it consumes/produces

### Step 3 — Propose 2-3 approaches

Present options with trade-offs. Lead with your recommended approach and explain why.
Apply YAGNI ruthlessly — remove unnecessary features from all designs.

### Step 4 — Present design in sections

Scale to complexity — a few sentences for simple modules, up to 200 words for complex stages.
Cover: module responsibility, DTO flow (inputs/outputs), database interactions, error handling, testability.
Ask "looks right so far?" after each section.

### Step 5 — Write design document

Save to `docs/specs/YYYY-MM-DD-<topic>-design.md` and commit.

### Step 6 — Spec self-review

Check the written spec for:

- Placeholders or incomplete sections (TBD, TODO)
- Internal contradictions
- Scope creep (does this need to be split?)
- Ambiguous requirements (pick one interpretation and make it explicit)

### Step 7 — User reviews written spec

Ask the user to review before proceeding:

> "Spec written and committed to `<path>`. Review it and let me know if you want changes before we write the implementation plan."

Wait for approval. Only proceed once the user approves.

### Step 8 — Invoke `writing-plans` skill

Transition to implementation by loading the `writing-plans` skill.

---

## Process Flow

```
Explore context
  → Ask clarifying questions (one at a time)
    → Propose 2-3 approaches with trade-offs
      → Present design sections → user approves each section
        → Write spec doc → self-review → user review gate
          → writing-plans skill (ONLY skill invoked after this)
```

**Terminal state is writing-plans.** Do not invoke any implementation skill directly.

---

## Key Principles

- **One question at a time** — don't overwhelm
- **YAGNI ruthlessly** — remove unnecessary complexity from all designs
- **Explore alternatives** — always propose 2-3 approaches
- **Incremental validation** — present design sections, get approval as you go
- **skeleton-parallel constraints** — every design must respect:
  - Module communication only through DTOs in `contracts/`
  - No direct DB access in modules (orchestrator only)
  - Same input + same config = identical output (determinism)

---

## Design Checklist

- [ ] Explored existing architecture and module structure
- [ ] Asked clarifying questions (one at a time)
- [ ] Proposed 2-3 approaches with trade-offs
- [ ] Design presented in sections, user approved each
- [ ] Design saved to `docs/specs/`
- [ ] Spec self-review passed (no placeholders, no contradictions)
- [ ] User reviewed and approved spec
- [ ] DTO inputs/outputs defined
- [ ] Ready to invoke `writing-plans`
