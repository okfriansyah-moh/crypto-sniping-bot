---
name: running-prompt
description: "This skill defines a structured workflow for executing tasks, including planning, implementation, security review, verification, and issue remediation. It ensures that all tasks are completed securely, verified, and approved before final confirmation. Optimized for Principal Engineer execution standards."
---

## Trigger

Use this skill when:

- You have a clear task that requires implementation, security review, and verification.
- You need to ensure a structured workflow with explicit user approval before completion.
- You want to maintain high standards of security, reliability, and performance in your implementations.
- You want to optimize for deterministic and production-grade outputs while allowing controlled exploration during planning.
- You want to ensure that all critical decisions are clarified with the user before implementation.
- You want to ensure that all issues are remediated before confirming completion.
- running-prompt is designed to guide you through a comprehensive task execution process, ensuring that all aspects of the implementation are thoroughly planned, securely implemented, and rigorously verified before final approval and completion confirmation.

---

# Task Execution Workflow

Follow the steps below to handle the tasks effectively.

---

# 0. Principal Engineer Temperature Configuration

Set temperature dynamically based on task type, optimized for deterministic and production-grade execution.

## Temperature Profile

| Task Category                      | Temperature | Rationale                                       |
| ---------------------------------- | ----------- | ----------------------------------------------- |
| Implementation / Execution         | **0.15**    | Deterministic, precise, production-safe output  |
| Research / Planning / Architecture | **0.45**    | Controlled exploration with trade-off reasoning |
| Security Review / Audit            | **0.2**     | Deterministic threat modeling                   |
| Verification / Analysis            | **0.2**     | Accurate validation without creative deviation  |
| Remediation / Fixing               | **0.15**    | Precise issue resolution                        |

---

## Temperature Rules

- Never exceed **0.5** for production work.
- Use **≤ 0.2** for:
  - Security-sensitive systems
  - Financial systems
  - Authentication / authorization
  - Data pipelines

- Lower temperature = higher determinism and auditability.

---

# 0.5. Execution Safety & Timeout Prevention

**This section governs ALL subsequent steps.** Every step in the workflow MUST follow these rules to prevent timeouts, context exhaustion, and network failure during execution.

## Core Principle

> **Never attempt to do everything in one massive output.** Break every task into small, atomic steps. Complete and confirm each step before starting the next. Prefer many small successful operations over one large operation that risks failure.

## Chunking Rules (MANDATORY)

| Rule                                | Description                                                                                                                  |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| **Max files per step**              | Create or edit at most **3 files** before pausing to confirm success                                                         |
| **Max lines per output**            | Keep each response under **200 lines of generated code** — split larger work into multiple steps                             |
| **Checkpoint after each step**      | After each logical unit of work, update the todo list and briefly confirm what was done before proceeding                    |
| **Save progress incrementally**     | Use `manage_todo_list` to track every sub-task — if a failure occurs, the todo list shows exactly where to resume            |
| **One subagent call at a time**     | Never chain multiple subagent calls without reading results — each subagent result must be received before invoking the next |
| **Prefer sequential over parallel** | For write operations (file creation, edits), execute sequentially — parallel reads are fine                                  |

## Step Decomposition Protocol

Before starting ANY multi-file task:

1. **Enumerate all files** that need to be created or modified
2. **Group into batches** of 1-3 files maximum per step
3. **Create a todo list** with one item per batch
4. **Execute batch by batch**, marking each todo as completed immediately after success
5. **Never skip the todo update** — this is your crash recovery mechanism

```
Example — Creating 10 files:
  Step 1: Create files 1-3     → mark todo completed → confirm
  Step 2: Create files 4-6     → mark todo completed → confirm
  Step 3: Create files 7-9     → mark todo completed → confirm
  Step 4: Create file 10       → mark todo completed → confirm
  Step 5: Update index/config  → mark todo completed → confirm
```

## Session Memory for Crash Recovery

For tasks spanning many operations:

1. **At the start**, create a session memory note (`/memories/session/`) summarizing the full plan
2. **After each batch**, update the session note with progress
3. **If context is getting long**, proactively summarize completed work and focus only on remaining items
4. This ensures that if a timeout/disconnect occurs, the next prompt can read the session memory and resume exactly where work stopped

## Context Window Management

| Signal                             | Action                                                                                         |
| ---------------------------------- | ---------------------------------------------------------------------------------------------- |
| Task requires >15 file operations  | Split into multiple conversation turns — tell user "I'll do X now, then we'll continue with Y" |
| Response is generating >300 lines  | Stop, deliver what's done, ask to continue                                                     |
| Multiple large skills need reading | Read skills one at a time, not all at once                                                     |
| Subagent tasks are complex         | Give subagent a focused scope — never "do everything"                                          |

## Anti-Timeout Patterns

- **NEVER** generate a massive single response with all files at once
- **NEVER** read more than 5 files simultaneously in parallel
- **NEVER** run a subagent with a prompt longer than 2000 words
- **NEVER** attempt to create an entire module (10+ files) in one step
- **ALWAYS** confirm tool call results before proceeding to the next operation
- **ALWAYS** prefer multiple small tool calls over one giant tool call
- **ALWAYS** update todo list between batches so progress is never lost
- **ALWAYS** tell the user the plan and batch count upfront so they know what to expect

## Recovery Protocol

If a previous attempt timed out or was interrupted:

1. Check `/memories/session/` for any saved progress notes
2. Check the todo list for last completed item
3. Read completed files to verify they were saved correctly
4. Resume from the next incomplete todo item — never restart from scratch
5. Tell the user: "Resuming from step N — steps 1 through N-1 are already done"

---

# 0.6. Task Completion Guarantee — NEVER Stop Mid-Task

**ABSOLUTE RULE: Once a task is accepted, it MUST be completed to the end. Stopping, pausing, or abandoning a task midway is FORBIDDEN.**

## The Completion Contract

> **Every user prompt represents a contract. You MUST fulfill the ENTIRE request — not part of it, not most of it — ALL of it. If the user asks for 10 files, deliver 10 files. If the user asks for 5 changes, make all 5 changes. There is no acceptable reason to stop mid-task and say "done" when work remains.**

## Mandatory Completion Rules

| Rule                          | Description                                                                                     |
| ----------------------------- | ----------------------------------------------------------------------------------------------- |
| **Finish what you start**     | If you begin a task, you MUST complete every item in it before responding to the user as "done" |
| **No silent abandonment**     | NEVER quietly drop remaining items because the response is getting long — continue working      |
| **Todo list is the contract** | Every unchecked todo item is unfinished work — you are NOT done until ALL items show completed  |
| **Count your deliverables**   | Before saying "done", count what the user asked for vs what you delivered — they MUST match     |
| **Batching ≠ stopping**       | Breaking work into batches (Section 0.5) means pause-then-continue, NOT pause-then-stop         |

## Self-Monitoring Protocol

At EVERY checkpoint between batches, ask yourself:

1. **"Are there remaining todo items?"** → If YES, keep working. Do NOT respond to user yet.
2. **"Did I deliver everything the user asked for?"** → If NO, continue immediately.
3. **"Is there a next batch to process?"** → If YES, process it now. Do NOT wait for user prompt.
4. **"Am I about to say 'done' or 'complete'?"** → STOP. Verify todo list first. If any item is not-started or in-progress, you are NOT done.

## Continuation Triggers

When working through batches, these conditions mean you MUST continue (not stop):

- Todo list has items with status `not-started` or `in-progress`
- The user's request enumerated N items and you've only completed < N
- You said "I'll do X in batches" and haven't finished all batches
- The plan you created has remaining unexecuted steps
- Files you planned to create/edit have not yet been created/edited

## Anti-Stall Patterns

```
FORBIDDEN behavior:
  ❌ "I've completed items 1-5. Let me know if you'd like me to continue with 6-10."
  ❌ "Here's what I've done so far. Shall I proceed with the rest?"
  ❌ "I'll stop here and we can continue in the next message."
  ❌ Generating a summary of completed work while items remain undone.
  ❌ Asking the user for permission to continue when the original request is clear.

REQUIRED behavior:
  ✅ Complete items 1-5, then IMMEDIATELY continue with 6-10 in the same response.
  ✅ If response is getting long, finish current batch, update todos, then start next batch.
  ✅ Only present final summary AFTER every single todo item is marked completed.
  ✅ If uncertain whether all work is done, check the todo list — trust the list, not your feeling.
```

## Context Length Strategy

If the context window is filling up during a long task:

1. **Do NOT stop working** — this is not an excuse to leave work incomplete
2. **Compress completed work** — refer to it briefly ("Steps 1-5 done") instead of repeating details
3. **Focus output on remaining work** — allocate context to what's left, not what's done
4. **If truly at context limit**, save progress to session memory, tell user exactly what remains, and provide the EXACT command to resume: "Please say: Continue from step N"
5. Even at context limit, you must have attempted and completed as many items as physically possible — stopping early "to be safe" is FORBIDDEN

## Pre-Response Completion Checklist

Before EVERY response that could be the final one, verify:

- [ ] All todo items are marked `completed`
- [ ] Count of delivered items matches count of requested items
- [ ] No planned files remain uncreated
- [ ] No planned edits remain unapplied
- [ ] The user's original request is fully satisfied — re-read it if unsure

**If ANY checkbox fails, DO NOT send the response. Continue working.**

---

# 0.7. Agent & Skill Utilization — ALWAYS Leverage Available Resources

**ABSOLUTE RULE: Every step of the workflow MUST consider whether a specialized agent or skill can improve the outcome. Do not perform work manually when a purpose-built agent or skill exists.**

## Core Principle

> **This system has specialized subagents and skills. They encode domain expertise, enforce invariants, and produce higher-quality results than ad-hoc manual work. You MUST use them whenever they are relevant to the current task.**

## When to Spawn a Subagent

Spawn a subagent when the current step involves:

1. **Domain expertise** — Use the domain-matched agent (e.g., `security-auditor` for security review, `dto-guardian` for DTO validation)
2. **Cross-cutting validation** — Use `integration` or `merge-reviewer` after writing code that touches multiple modules
3. **Phase implementation** — Use `phase-builder` when building a new phase
4. **Quality assurance** — Use `doctor` when quality gates need checking
5. **Module-specific work** — Use `module-builder` for building individual modules
6. **Post-implementation review** — Steps 3a/3b MUST use `security-auditor` and relevant verification agents

## Mandatory Agent Usage Points in Workflow

| Workflow Step             | Required Agent(s)                                                        |
| ------------------------- | ------------------------------------------------------------------------ |
| Step 1 (Planning)         | `phase-builder` (if building a phase), `Explore` (for codebase research) |
| Step 2 (Implementation)   | Relevant domain agent for the module being built                         |
| Step 3a (Security Review) | `security-auditor` — MANDATORY for all security reviews                  |
| Step 3b (Verification)    | `doctor` or `integration` — MANDATORY for verification                   |
| Step 4 (Remediation)      | Same agents that found the issues                                        |

## Skill Loading Protocol

Before ANY implementation step, check if a matching skill exists:

1. **Match the task domain** against available skills
2. **Read the SKILL.md** file BEFORE writing any code — skills contain production rules, invariants, and constraints
3. **Follow the skill's procedure** — skills encode tested workflows, not suggestions

## Agent + Skill Composition Pattern

For complex tasks, combine agents with skills:

```
Example — Implementing a new module:

1. Read skills: dto, modularity, code-quality
2. Spawn subagent: module-builder with task "build module_x"
3. After implementation, spawn: integration to validate boundaries
4. Spawn: security-auditor for security review
5. Read skill: test-generation then write tests
6. Spawn: doctor to verify overall health
```

## Decision Flowchart

```
For each step in the workflow:
  1. "Does a specialized AGENT exist for this domain?"
     → YES: Spawn it via runSubagent with focused prompt
     → NO: Continue to step 2

  2. "Does a SKILL exist for this domain?"
     → YES: Read the SKILL.md FIRST, then implement following its procedure
     → NO: Implement using general best practices

  3. "Am I doing post-implementation validation?"
     → YES: ALWAYS spawn security-auditor + doctor/integration
     → NO: Continue to next workflow step
```

## Anti-Patterns (FORBIDDEN)

```
❌ Writing module code without reading the matching modularity/dto skills
❌ Implementing a phase without spawning phase-builder
❌ Performing security review manually instead of spawning security-auditor
❌ Writing tests without reading test-generation skill
❌ Skipping skill loading because "I already know how to do this"
❌ Using a generic approach when a specialized agent exists for the domain
```

---

# 1. Planning via Subagent

Use the **subagent** to thoroughly plan the tasks.

Return the implementation plan complete with:

- Technical details
- Architecture decisions
- Confirmed critical approaches
- Identified risks
- Mitigation strategies
- Performance considerations
- Security implications

---

## Mandatory Clarification

The planning subagent **must use the askQuestion tool** to clarify uncertainties and confirm important technical approaches with the user.

This includes anything affecting:

- Functional behavior
- Resiliency
- Security
- Robustness
- Performance
- Reliability
- Cost efficiency
- Scalability

No assumptions are allowed on critical decisions.

---

# 2. Immediate Implementation

Immediately perform the implementation according to the approved plan on the **main agent**, without ending the session.

Follow:

- Copilot instructions and architecture invariants
- Security best practices
- Reliability engineering principles
- Performance optimization guidelines

Implementation must strictly align with approved planning outputs.

---

# 3. Parallel Post-Implementation Review

After implementation, run **two subagents in parallel**:

---

## 3a. Security Review Mode

Perform a comprehensive security assessment.

### Report must include:

- Security issues identified
- Estimated CVSS score
- Risk severity classification
- Exploit scenarios
- Attack vectors
- Compliance gaps
- Recommended remediations

### Coverage Areas

- Input validation
- Injection risks
- AuthN / AuthZ
- Secrets handling
- Data exposure
- Dependency vulnerabilities
- Configuration risks

---

## 3b. Verification Mode

Perform technical verification including:

- Build validation
- Static code analysis
- Automated tests
- Linting
- Type checking
- Coverage validation

### Report must include:

- Build failures
- Test failures
- Code quality issues
- Type violations
- Coverage gaps

---

# 4. Issue Remediation Loop

If any issues are found:

1. Fix all issues immediately
2. Re-implement corrections
3. Re-run Step 3 (parallel reviews)

Repeat until:

- No security findings remain
- No verification issues remain

Zero-issue state is mandatory.

---

# 5. Pre-Completion Approval Gate

Before generating the final summary or completion confirmation:

You **must use the askQuestion tool** to obtain explicit user approval.

---

## Approval Request Must Include

- Implementation summary
- Key technical decisions
- Security review status
- Verification status
- Risks (if any)
- Trade-offs made

⚠️ Final summary is **forbidden** before approval is granted.

---

# 6. Completion Confirmation

Once approval is received and no issues remain, confirm that the implementation is:

- Complete
- Secure
- Verified
- To-do list checked

And has passed:

- Security review
- Verification checks
- Quality gates
- Make sure todo list is fully checked off
- Make sure all issues are remediated
- Make sure codebase error-free and production-ready
- Do not make duplicate files with suffix 2, example : readme 2.md, implementation_roadmap 2.md, etc. If you need to update the content, update the original file instead of creating a new one.
- Do not make duplicate folders with suffix 2, example : docs 2/, etc. If you need to add new content, add it to the original folder instead of creating a new one.

Then, provide the final confirmation of task completion.
