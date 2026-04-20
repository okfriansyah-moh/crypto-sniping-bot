---
name: project-scaffold
type: skill
description: "Project scaffolding patterns. Use when initializing new projects, generating boilerplate, or reviewing project structure for completeness."
---

## Purpose

Ensure new projects are initialized with the correct structure, configuration, and tooling for the skeleton-parallel framework. Validate that all required directories, files, and configurations exist.

---

## Rules

### Required Structure

Every skeleton-parallel project MUST have:

```
<project>/
├── .github/
│   ├── copilot-instructions.md    # Architectural constraints
│   ├── agents/                    # Autonomous execution agents
│   ├── skills/                    # Knowledge modules
│   └── prompts/                   # Generation prompts
├── app/                           # Application code
│   ├── main.* | cmd/             # Entry point (language-specific)
│   ├── modules/                   # Domain modules (vertical slices)
│   └── orchestrator/              # Pipeline orchestration
├── contracts/                     # Immutable DTO definitions
├── database/                      # DB adapter + migrations
├── config/                        # YAML configuration
│   └── phases.yaml               # Phase metadata for parallel dev
├── scripts/
│   └── run_parallel.sh           # Parallel development orchestrator
├── tests/                         # Test directory
├── docs/                          # Architecture + specifications
├── output/                        # Generated artifacts (gitignored)
├── .gitignore
└── README.md
```

### Language-Specific Entry Points

| Language   | Entry Point                   | Module Location              |
| ---------- | ----------------------------- | ---------------------------- |
| Go         | `app/cmd/root.go`             | `app/internal/modules/`      |
| Python     | `app/main.py`                 | `app/modules/`               |
| TypeScript | `src/main.ts`                 | `src/modules/`               |
| Rust       | `src/main.rs`                 | `src/modules/`               |
| Java       | `src/main/java/.../Main.java` | `src/main/java/.../modules/` |

### Initialization Checklist

```
[ ] Project directory created with correct structure
[ ] .github/ contains copilot-instructions, skills, agents, prompts
[ ] scripts/run_parallel.sh installed and executable
[ ] config/phases.yaml present with default phase configuration
[ ] contracts/ directory exists with initial DTO file
[ ] database/ directory exists with adapter stub
[ ] docs/ directory populated with template specifications
[ ] .gitignore includes output/, .parallel-dev/, language-specific patterns
[ ] README.md with quick-start instructions
[ ] Git repository initialized with initial commit
[ ] Language-specific build/test configuration present
[ ] Health check module scaffolded as reference implementation
```

### Post-Init Validation

After initialization, run `skeleton doctor` to verify:

1. All framework files present
2. Skills and agents installed
3. Configuration valid
4. Project structure matches language template
5. Git repository healthy

---

## Anti-Patterns

| Pattern                      | Problem                    | Fix                                 |
| ---------------------------- | -------------------------- | ----------------------------------- |
| Manual copy-paste setup      | Inconsistent structure     | Use `skeleton init <lang>`          |
| Missing copilot-instructions | Agents can't enforce rules | Always include .github/             |
| Hardcoded config values      | Not portable               | Use config.yaml                     |
| No initial module            | No reference pattern       | Include health module               |
| Missing .gitignore           | Generated files in git     | Include language-specific gitignore |
