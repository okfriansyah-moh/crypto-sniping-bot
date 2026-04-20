---
name: rtk
type: skill
description: >
  Token-efficient CLI proxy. Use `rtk <cmd>` instead of running commands
  directly to get compressed, AI-optimized output with 60-90% fewer tokens.
  Supports 100+ commands including git, go, npm, docker, curl, and more.
---

# RTK — AI-Optimized CLI Proxy

`rtk` is a Rust CLI tool that runs commands and compresses their output for LLM consumption — same semantic content, 60-90% fewer tokens.

GitHub: https://github.com/rtk-ai/rtk

---

## Installation

```bash
# macOS / Linux
curl -fsSL https://rtk.ai/install.sh | sh

# Or via cargo
cargo install rtk
```

Verify: `rtk --version`

---

## Basic Usage

```bash
rtk <command> [args]
```

Same interface as the original command. `rtk` runs it, captures output, and returns a compressed summary.

```bash
# Instead of:
git log --oneline -20

# Use:
rtk git log --oneline -20
```

---

## Supported Commands (Key Categories)

| Category   | Commands                                                   |
| ---------- | ---------------------------------------------------------- |
| Git        | `git status`, `git log`, `git diff`, `git blame`           |
| Go         | `go test ./...`, `go build`, `go vet`, `golangci-lint run` |
| JavaScript | `npm test`, `npm install`, `yarn`, `eslint`                |
| Python     | `pytest`, `pip install`, `mypy`, `ruff check`              |
| Docker     | `docker ps`, `docker logs`, `docker build`                 |
| Files      | `ls`, `find`, `cat`, `head`, `tail`, `grep`, `wc`          |
| System     | `ps`, `top`, `df`, `du`, `env`                             |
| Network    | `curl`, `wget`, `ping`, `netstat`                          |
| Build      | `make`, `cmake`, `cargo build`, `cargo test`               |
| Database   | `psql`, `sqlite3`, `mysql` (query result compression)      |

---

## When to Use rtk

**Use rtk for:**

- Commands with verbose output (test results, build logs, git history)
- Repeated commands during debugging sessions
- Any command where you need to paste output into an AI prompt
- Long-running commands where only the summary matters

**Skip rtk for:**

- Interactive commands (`vim`, `ssh`, `psql` in interactive mode)
- Commands with binary output
- Short commands where output fits in a few lines anyway

---

## Integration with Skeleton Agents

When skeleton agents run terminal commands for verification, use `rtk` to keep output compact:

```bash
# Test verification
rtk go test ./... -v

# Lint check
rtk golangci-lint run ./...

# Build verification
rtk go build ./...

# Git diff review
rtk git diff --stat HEAD~1
```

This reduces context consumption when agents check build/test status.

---

## Hook Mode (Advanced)

`rtk` supports a hook mode that wraps all shell commands automatically:

```bash
# Add to shell profile to wrap all commands:
eval "$(rtk hook init)"
```

With hooks active, every command you run gets automatically proxied through `rtk`. Disable temporarily with `rtk hook off`.

---

## Output Format

`rtk` preserves:

- Error messages (full, uncompressed — errors matter)
- Exit codes (mirrors the underlying command)
- Test failure details (which tests failed and why)

`rtk` compresses:

- Success logs (summarizes verbose pass output)
- Progress bars and spinners (replaced with final status)
- Redundant repeated lines (counts instead of repeating)
- Timestamp/PID noise in log output

---

## Checklist

- [ ] `rtk` installed and `rtk --version` returns successfully
- [ ] Using `rtk` for commands with >10 lines of output
- [ ] Not using `rtk` for interactive commands
- [ ] Exit codes checked (same as original command)
- [ ] Error messages read fully (rtk doesn't compress errors)
