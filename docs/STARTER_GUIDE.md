# Starter Guide

> Step-by-step playbook for using the skeleton-parallel framework to build a new project.

---

## 1. Prerequisites

- **Runtime environment** for the project’s chosen language installed
- **Git** with worktree support (Git 2.5+)
- **VS Code** with GitHub Copilot extension
- **Copilot CLI** for automated agent validation (required by default, see Agent Behavior below)

---

## 2. Quick Start

### Option A: Using the Skeleton CLI (Recommended)

```bash
# Install the framework
git clone <skeleton-parallel-repo-url>
export PATH="$PWD/skeleton-parallel/bin:$PATH"

# Initialize a new Go project
skeleton init go --name=my-service --dir=my-service
cd my-service

# Verify setup
skeleton doctor
```

Supported languages: `go`, `python`, `typescript`, `nodejs`, `rust`, `java`

The CLI creates a complete project with:

- Modular monolith structure with health module reference implementation
- All framework files (.github/, scripts/, config/, docs/)
- 28 skills, 14 agents, 5 prompts pre-installed
- Language-specific build/test tooling
- Git repository initialized

### Option B: Upgrade an Existing Repository

```bash
cd existing-project
skeleton upgrade            # Auto-detects existing mechanisms, prompts Replace/Hybrid/Skip
skeleton upgrade --mode=hybrid  # Force hybrid mode (merge additively)
skeleton doctor
```

The upgrade command now:

1. **Detects** existing skeleton-parallel mechanisms (skills, agents, prompts, run_parallel.sh, etc.)
2. **Prompts** for upgrade mode if mechanisms are found:
   - **Replace** — Remove existing, install fresh from upstream
   - **Hybrid** — Merge additively (add missing, preserve custom files)
   - **Skip** — Install only completely new components
3. **Spawns** a Copilot agent for post-upgrade validation and auto-fix

### Agent Behavior

Starting with v1.1.0, all file-modifying commands automatically spawn a Copilot CLI agent for validation:

| Command      | Agent Mode    | Agent Task                                                  |
| ------------ | ------------- | ----------------------------------------------------------- |
| `init`       | Foreground    | Validates project structure, docs, parallel readiness       |
| `upgrade`    | Foreground    | Validates upgrade completeness, fixes issues                |
| `doctor`     | Foreground/BG | Deep health check with auto-fix (FG if issues, BG if clean) |
| `sync`       | Background    | Validates sync results didn't break anything                |
| `add`        | Background    | Validates newly added skill/agent format                    |
| `autoskills` | Background    | Validates skills match project's technology stack           |

**Disabling agents:**

```bash
skeleton init go --name=my-service --no-agent    # Per-command
SKIP_AGENT=true skeleton upgrade                  # Global via env var
```

**Agent model:** Controlled by `COPILOT_MODEL` env var (default: `claude-sonnet-4.6`).

**Agent logs:** Saved to `.parallel-dev/agent-logs/` with timestamps.

### Option C: Manual Setup

#### Step 1: Clone the Framework

```bash
git clone <skeleton-parallel-repo-url> my-project
cd my-project
git remote remove origin
git init  # Fresh history for your project
```

### Step 2: Generate Architecture

Use the architecture prompt to create your system design:

```
@workspace Use .github/prompts/architecture.prompt.md to generate docs/architecture.md
```

The prompt will guide you through:

- Defining your system goal
- Listing pipeline stages
- Defining data models
- Specifying DTO contracts

### Step 3: Generate Roadmap

Use the roadmap prompt to create your implementation plan:

```
@workspace Use .github/prompts/roadmap.prompt.md to generate docs/implementation_roadmap.md
```

This generates:

- Phase-by-phase tasks with exit criteria
- File ownership matrix for parallel development
- Priority tiers (P0/P1/P1.5/P2)

### Step 4: Generate Supporting Specs

Generate the remaining specification documents:

```
@workspace Use .github/prompts/dto.prompt.md to generate docs/dto_contracts.md
@workspace Use .github/prompts/orchestrator.prompt.md to generate docs/orchestrator_spec.md
@workspace Use .github/prompts/db_adapter.prompt.md to generate docs/db_adapter_spec.md
```

### Step 5: Implement Phase 0 (Infrastructure)

Phase 0 creates the foundation. Always implement this first:

```
@phase-builder implement Phase 0
```

This creates:

- Database schema and migrations
- Database adapter
- Configuration loader
- Entry point
- Orchestrator skeleton

### Step 6: Run Parallel Development

Once Phase 0 is complete, run remaining phases in parallel:

```bash
# Mode 3 (Hybrid) — recommended default
./scripts/run_parallel.sh start --mode=3 1 2 3 4

# Mode 1 (Full Parallel) — maximum speed
./scripts/run_parallel.sh start --mode=1 2 3 4

# Mode 2 (Token-Optimized) — minimum cost
./scripts/run_parallel.sh start --mode=2 1 2 3
```

---

## 3. Framework Structure

```
skeleton-parallel/
├── bin/
│   └── skeleton                   # CLI tool (init, upgrade, doctor, add, list, sync, autoskills)
├── templates/                     # Language-specific project templates
│   ├── common/                    # Shared files (README, .gitignore)
│   ├── go/                        # Go vertical slice template
│   ├── python/                    # Python modular monolith template
│   ├── typescript/                # TypeScript template
│   ├── nodejs/                    # Node.js (JavaScript) template
│   ├── rust/                      # Rust template
│   └── java/                      # Java template
├── .github/
│   ├── copilot-instructions.md    # Hard architectural constraints (always loaded)
│   ├── prompts/                   # One-shot generation prompts
│   │   ├── architecture.prompt.md
│   │   ├── roadmap.prompt.md
│   │   ├── orchestrator.prompt.md
│   │   ├── dto.prompt.md
│   │   └── db_adapter.prompt.md
│   ├── agents/                    # Autonomous execution agents (14 total)
│   │   ├── phase-builder.agent.md     # Core pipeline agents
│   │   ├── dto-guardian.agent.md
│   │   ├── integration.agent.md
│   │   ├── refactor.agent.md
│   │   ├── orchestrator.agent.md
│   │   ├── module-builder.agent.md
│   │   ├── conflict-resolver.agent.md
│   │   ├── merge-reviewer.agent.md
│   │   ├── task-sync.agent.md
│   │   ├── scaffold.agent.md          # Framework agents
│   │   ├── security-auditor.agent.md
│   │   ├── test-builder.agent.md
│   │   ├── upgrade-manager.agent.md
│   │   └── doctor.agent.md
│   └── skills/                    # Focused knowledge modules (28 total)
│       ├── dto/SKILL.md               # Core pipeline skills
│       ├── pipeline/SKILL.md
│       ├── modularity/SKILL.md
│       ├── determinism/SKILL.md
│       ├── idempotency/SKILL.md
│       ├── failure/SKILL.md
│       ├── token-optimization/SKILL.md
│       ├── config-validation/SKILL.md
│       ├── code-quality/SKILL.md
│       ├── coding-standards/SKILL.md
│       ├── conflict-resolution/SKILL.md
│       ├── docs-sync/SKILL.md
│       ├── database-portability/SKILL.md
│       ├── running-prompt/SKILL.md
│       ├── security-audit/SKILL.md    # Framework skills
│       ├── test-generation/SKILL.md
│       ├── vertical-slice/SKILL.md
│       ├── api-design/SKILL.md
│       ├── project-scaffold/SKILL.md
│       ├── dependency-analysis/SKILL.md
│       ├── migration-management/SKILL.md
│       ├── performance-optimization/SKILL.md
│       ├── caveman/SKILL.md           # Always-active skills
│       ├── brainstorming/SKILL.md
│       ├── writing-plans/SKILL.md
│       ├── subagent-driven-development/SKILL.md
│       ├── test-driven-development/SKILL.md
│       └── rtk/SKILL.md
├── docs/                          # Architecture + specs (templates)
├── contracts/                     # Immutable DTO definitions
├── database/                      # DB adapter + migrations
├── config/                        # YAML configuration
├── app/                           # Application code
│   ├── main.*                     # Entry point (language-specific)
│   ├── modules/                   # One package per pipeline stage
│   └── orchestrator/              # Pipeline orchestration
├── tests/                         # Unit + integration tests
├── scripts/                       # Automation (run_parallel.sh)
└── output/                        # Generated artifacts (gitignored)
```

---

## 4. Development Workflow

### Single Phase (Manual)

```
@phase-builder implement Phase 3
```

The phase-builder agent:

1. Reads the roadmap for Phase 3 requirements
2. Creates module under `app/modules/`
3. Defines DTOs in `contracts/`
4. Writes unit tests
5. Wires into orchestrator

### Parallel Phases (Automated)

```bash
./scripts/run_parallel.sh start --mode=3 2 3 4
```

The script:

1. Groups phases by dependency
2. Creates Git worktrees per group
3. Runs agent pipeline (build → validate → integrate → fix)
4. Merges results
5. Runs global validation

### Merge & Validate

```bash
./scripts/run_parallel.sh merge
```

### Check Status

```bash
./scripts/run_parallel.sh status
```

### Cleanup

```bash
./scripts/run_parallel.sh cleanup
```

---

## 5. Key Concepts

### Modular Monolith

- Single process, single database
- Modules communicate only through DTOs
- No cross-module imports

### Deterministic Execution

- Same input + same config = identical output
- Content-addressable IDs (SHA-256)
- No randomness in processing logic

### Self-Healing Retries

- Every stage: execute → validate → fix → retry → success OR rollback
- Bounded retries (no infinite loops)
- Checkpoint before each phase (Git tags)

### Documentation-First

- Architecture document drives all implementation
- DTO contracts define module interfaces
- Roadmap defines phase boundaries and ownership

---

## 6. Customization

### Adding Pipeline Stages

1. Define the stage in `docs/architecture.md`
2. Add the DTO contract in `docs/dto_contracts.md`
3. Create the module under `app/modules/<stage_name>/` (or `src/modules/` for TypeScript/Node.js)
4. Define DTOs in `contracts/<stage_name>.*` (language-specific extension)
5. Wire into `app/orchestrator/pipeline.*`
6. Add to `docs/implementation_roadmap.md`

### Adding Skills

Create a new folder at `.github/skills/<kebab-case-name>/SKILL.md` with:

- YAML frontmatter (`name`, `type: skill`, `description`)
- Purpose, Rules, Inputs, Outputs, Examples, Checklist sections
- Reference the skill in agent files that need it (under `## Skills Used`)

### Adding Agents

Create `.github/agents/new_agent.agent.md` with:

- YAML frontmatter (name, description, argument-hint, tools, model)
- Skills Used section referencing `.github/skills/<name>/SKILL.md`
- Role, Responsibilities, Constraints, Source of Truth, Output sections

---

## 7. Skeleton CLI Reference

### Commands

| Command                                     | Description                                                             |
| ------------------------------------------- | ----------------------------------------------------------------------- |
| `skeleton init <lang>`                      | Create a new project (go, python, typescript, nodejs, rust, java)       |
| `skeleton upgrade`                          | Upgrade existing repo — detects mechanisms, prompts Replace/Hybrid/Skip |
| `skeleton doctor`                           | Validate project health; auto-fix via Copilot agent if issues found     |
| `skeleton autoskills`                       | Detect tech stack and install matching skills automatically             |
| `skeleton add skill <name>`                 | Install a specific skill                                                |
| `skeleton add agent <name>`                 | Install a specific agent                                                |
| `skeleton list [skills\|agents\|templates]` | Show available resources                                                |
| `skeleton sync`                             | Force-update all skills, agents, prompts, and run_parallel.sh           |
| `skeleton version`                          | Show CLI version                                                        |

### Options

| Option        | Applies to         | Description                                                   |
| ------------- | ------------------ | ------------------------------------------------------------- |
| `--name=NAME` | `init`             | Project name (default: directory name)                        |
| `--dir=DIR`   | most commands      | Target directory (default: current directory or project name) |
| `--mode=MODE` | `upgrade`          | Force upgrade mode: `replace`, `hybrid`, or `skip`            |
| `--no-agent`  | all file-modifying | Skip Copilot CLI agent spawning for this invocation           |
| `--dry-run`   | `autoskills`       | Preview skills to install without installing                  |
| `-y`          | `autoskills`       | Skip confirmation prompt                                      |

### Environment Variables

| Variable        | Default             | Description                                      |
| --------------- | ------------------- | ------------------------------------------------ |
| `COPILOT_MODEL` | `claude-sonnet-4.6` | Model used by all spawned Copilot agents         |
| `SKIP_AGENT`    | _(unset)_           | Set to `true` to globally disable agent spawning |

### Examples

```bash
# Create a Go project named "payment-service"
skeleton init go --name=payment-service

# Create a TypeScript project, skip agent validation
skeleton init typescript --name=my-api --no-agent

# Upgrade with forced hybrid mode (add missing, preserve custom)
skeleton upgrade --mode=hybrid

# Auto-detect and install skills for current project
skeleton autoskills -y

# Preview which skills would be installed
skeleton autoskills --dry-run

# Add a specific skill to current project
skeleton add skill security-audit

# List all available skills
skeleton list skills

# Force-sync all framework files from upstream
skeleton sync

# Use a different model for agent validation
COPILOT_MODEL=claude-opus-4.6 skeleton doctor
```
