---
name: token-optimization
description: "Token optimization for development agents. Use when designing agent prompts, planning context loading, or reducing redundant document reads. Provides strategies for progressive loading, skill-first approach, and context compression."
---

# Token Optimization Skill

## Purpose

Minimize token consumption in agent interactions by using skills as pre-digested knowledge, progressive disclosure for document reads, and context compression techniques.

## Rules

1. **Never read full docs as first action** — Load relevant skills first
2. **Never read docs you don't need** — Orchestrator agent doesn't need full DTO definitions
3. **Cache within session** — Don't re-read a skill you already loaded
4. **Use grep for targeted reads** — Search for the section header, read only that section
5. **Delegate exploration** — Use subagents for multi-doc research
6. **Reference, don't repeat** — Say "per dto skill" instead of re-stating the rules

## Inputs

- Agent task context and required knowledge
- Available skills in `.github/skills/`
- Documentation in `docs/`

## Outputs

- Optimized context loading strategy
- Reduced token consumption per agent invocation

## Examples

### Strategy 1: Skill-First Loading

```
BEFORE (wasteful):
  Agent reads architecture.md (8K) → extracts 200 tokens of relevant rules
  Total: 13,000 tokens consumed, 500 tokens useful

AFTER (efficient):
  Agent loads dto skill (500 tokens) → gets pre-digested DTO rules
  Agent loads determinism skill (400 tokens) → gets enforcement rules
  Total: 900 tokens consumed, 900 tokens useful
```

### Strategy 2: Progressive Disclosure

```
Level 1 — Skill Discovery (~100 tokens)
  Read skill name + description → Decide relevance

Level 2 — Skill Body (~300–500 tokens)
  Read SKILL.md → Get focused rules and patterns

Level 3 — Doc Section (~500–2000 tokens)
  Read specific doc section via reference

Level 4 — Full Doc (~5000–10000 tokens)
  ONLY when implementing a full phase from scratch
```

### Strategy 3: Targeted Doc Reads

```python
# ❌ WASTEFUL
read_file("docs/implementation_roadmap.md", 1, 2000)

# ✅ EFFICIENT
grep_search("## Phase 3", includePattern="docs/implementation_roadmap.md")
read_file("docs/implementation_roadmap.md", start_line, end_line)
```

### Strategy 4: Subagent Isolation

Use subagents for research that doesn't need to stay in main context. Delegate multi-doc reads to subagents, get compressed summaries back.

### Strategy 5: Context Compression

```
# ❌ VERBOSE (500 tokens)
"The EntityDTO has the following fields: entity_id (string, 16 hex chars)..."

# ✅ COMPRESSED (50 tokens)
"EntityDTO: all scores [0.0-1.0], sorted by -score then +created_at.
See .github/skills/dto/SKILL.md for full registry."
```

## Anti-Patterns

| Anti-Pattern                                | Token Cost          | Fix                                        |
| ------------------------------------------- | ------------------- | ------------------------------------------ |
| Read all docs before every task             | +25K per invocation | Load 2–3 skills instead                    |
| Re-read same doc in same session            | +8K wasted          | Cache the first read                       |
| Copy DTO definitions into response          | +500 per DTO        | Reference the skill                        |
| Explain architecture before implementing    | +2K per explanation | Skip — skills encode it                    |
| Read implementation_roadmap for refactoring | +10K wasted         | Refactor agent only needs modularity skill |

## Checklist

- [ ] Skills loaded before raw documentation
- [ ] No unnecessary full-doc reads
- [ ] Subagents used for multi-doc research
- [ ] References used instead of repeating rules
- [ ] Progressive disclosure followed (skill → section → full doc)
