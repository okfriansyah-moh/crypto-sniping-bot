#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# Skeleton Parallel — Parallel Development Orchestrator
# ─────────────────────────────────────────────────────────────────────────────
# 3-mode execution system for running multiple implementation phases
# simultaneously using autonomous AI agents.
#
# Usage:
#   ./scripts/run_parallel.sh start [--mode=1|2|3] <phases...>
#   ./scripts/run_parallel.sh status
#   ./scripts/run_parallel.sh merge
#   ./scripts/run_parallel.sh cleanup
#   ./scripts/run_parallel.sh gates
#
# Modes:
#   Mode 1 — Full Parallel   : One worktree + agent per phase (max speed)
#   Mode 2 — Token-Optimized  : Single session, sequential phases (min cost)
#   Mode 3 — Hybrid (default) : Parallel across groups, sequential within
#
# Configuration:
#   Phase metadata is loaded from config/phases.yaml (or override via env).
#   See docs/PARALLEL_DEV.md for full documentation.
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

# ── Bash version check (associative arrays require bash 4+) ──────────────
# If running under bash < 4, attempt to find or install a modern bash and re-exec.
if (( BASH_VERSINFO[0] < 4 )); then
    # Candidate paths for Homebrew-installed bash (Apple Silicon / Intel)
    _BREW_BASH_PATHS=("/opt/homebrew/bin/bash" "/usr/local/bin/bash")

    _find_modern_bash() {
        for _p in "${_BREW_BASH_PATHS[@]}"; do
            if [[ -x "$_p" ]] && "$_p" -c '(( BASH_VERSINFO[0] >= 4 ))' 2>/dev/null; then
                echo "$_p"
                return 0
            fi
        done
        return 1
    }

    _modern_bash="$(_find_modern_bash || true)"

    # If no modern bash found, try to install via Homebrew
    if [[ -z "${_modern_bash}" ]]; then
        echo "INFO: bash 4+ required (found bash ${BASH_VERSION})."
        if command -v brew &>/dev/null; then
            echo "INFO: Installing modern bash via Homebrew..."
            brew install bash
            _modern_bash="$(_find_modern_bash || true)"
            if [[ -z "${_modern_bash}" ]]; then
                echo "ERROR: brew install bash succeeded but no bash 4+ found at expected paths."
                echo "       Searched: ${_BREW_BASH_PATHS[*]}"
                exit 1
            fi
        else
            echo "ERROR: bash 4+ required and Homebrew is not installed."
            echo "       Install Homebrew (https://brew.sh) then re-run, or install bash manually:"
            echo "         brew install bash"
            exit 1
        fi
    fi

    echo "INFO: Re-executing with ${_modern_bash} (bash $("${_modern_bash}" -c 'echo ${BASH_VERSION}'))..."
    exec "${_modern_bash}" "$0" "$@"
fi

# ─────────────────────────────────────────────────────────────────────────────
# Constants
# ─────────────────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
PROJECT_NAME="$(basename "${PROJECT_ROOT}")"
WORKTREE_BASE="${PROJECT_ROOT}/../${PROJECT_NAME}-worktrees"
LOG_DIR="${PROJECT_ROOT}/.parallel-dev/logs"
STATE_FILE="${PROJECT_ROOT}/.parallel-dev/state.json"
INTEGRATION_BRANCH="integration/parallel-$(date +%Y%m%d-%H%M%S)"
PHASES_CONFIG="${PROJECT_ROOT}/config/phases.yaml"

# Default mode
MODE=3

# MODEL ROUTING STRATEGY:
#   Mode 1 (Full Parallel) : claude-opus-4.6   — heaviest phase gets opus
#   Mode 2 (Token-Optimized): claude-sonnet-4.6 — single session, sonnet only
#   Mode 3 (Hybrid)         : claude-sonnet-4.6 — heaviest group gets sonnet
#   Rotate models: sonnet-4.6 → sonnet-4.5 → gpt-5.3-codex → gpt-5.4 (round-robin)
#                  Used for: all other phases, conflict-resolver, post-merge review,
#                            docs sync, quality gate remediation, integration remediation
#
MODEL_HEAVY="${MODEL_HEAVY:-claude-opus-4.6}"           # Mode 1 only
MODEL_HEAVY_LITE="${MODEL_HEAVY_LITE:-claude-sonnet-4.6}" # Modes 2 & 3
MODEL_ROTATE_POOL=("claude-sonnet-4.6" "claude-sonnet-4.5" "gpt-5.3-codex" "gpt-5.4")
ROTATION_INDEX=0

# ── Per-stage retry limits (bounded — no infinite loops) ──────────────────
MAX_RETRIES_PHASE_BUILDER="${MAX_RETRIES_PHASE_BUILDER:-5}"
MAX_RETRIES_DTO="${MAX_RETRIES_DTO:-5}"
MAX_RETRIES_INTEGRATION="${MAX_RETRIES_INTEGRATION:-5}"
MAX_RETRIES_MERGE="${MAX_RETRIES_MERGE:-5}"
MAX_RETRIES_GLOBAL_VALIDATION="${MAX_RETRIES_GLOBAL_VALIDATION:-5}"
MAX_REMEDIATION_RETRIES="${MAX_REMEDIATION_RETRIES:-3}"

# ── Resource control ─────────────────────────────────────────────────────
MAX_PARALLEL_AGENTS="${MAX_PARALLEL_AGENTS:-3}"

# Agent pipeline — mandatory execution order per phase/group
# All five agents run for every phase: implement → validate DTOs → validate wiring → security → tests
AGENT_PIPELINE=("phase-builder" "dto-guardian" "integration" "security-auditor" "test-builder")
REMEDIATION_AGENT="refactor"

# Per-agent retry limits
MAX_RETRIES_SECURITY="${MAX_RETRIES_SECURITY:-3}"
MAX_RETRIES_TESTS="${MAX_RETRIES_TESTS:-3}"

# All 28 skills — injected into every Copilot call so agents always have full knowledge access
CORE_SKILLS="dto, pipeline, modularity, determinism, idempotency, failure, config-validation, code-quality, coding-standards, database-portability, docs-sync, conflict-resolution, token-optimization, running-prompt, security-audit, test-generation, vertical-slice, api-design, project-scaffold, dependency-analysis, migration-management, performance-optimization, caveman, brainstorming, writing-plans, subagent-driven-development, test-driven-development, rtk"

# Workspace confinement rule — injected into every agent prompt
# Prevents agents from writing to /tmp or paths outside the project (Permission denied errors)
_WORKSPACE_CONSTRAINT="WORKSPACE CONSTRAINT: NEVER write any files, scripts, summaries, or reports to /tmp, /var, /private, or any path outside this project directory. Write ALL output files inside the project — use .parallel-dev/ for temporary artifacts and output/ for generated files."

# Protected paths — agents MUST NOT modify unless explicitly instructed
PROTECTED_PATHS=("contracts/" "database/" "docs/")

# ─────────────────────────────────────────────────────────────────────────────
# Phase metadata — loaded from config/phases.yaml or defined here as defaults
# ─────────────────────────────────────────────────────────────────────────────
# Override these associative arrays after sourcing if using YAML config.
# The load_phase_config() function populates them from config/phases.yaml.

declare -A PHASE_NAMES=()
declare -A PHASE_COMPLEXITY=()
declare -A PHASE_TO_GROUP=()
declare -A PHASE_SKILLS=()

load_phase_config() {
    # Load phase metadata from config/phases.yaml using Python
    # Expected YAML format:
    #   phases:
    #     0:
    #       name: "core-infrastructure"
    #       complexity: 8
    #       group: "A"
    #       skills: "idempotency, failure"
    #     1:
    #       name: "stage-name"
    #       complexity: 5
    #       group: "B"
    #       skills: "dto, modularity, determinism"

    if [[ ! -f "${PHASES_CONFIG}" ]]; then
        log_warn "No phases config at ${PHASES_CONFIG}. Using inline defaults."
        # Provide Phase 0 as a minimal default
        PHASE_NAMES[0]="core-infrastructure"
        PHASE_COMPLEXITY[0]=8
        PHASE_TO_GROUP[0]="A"
        PHASE_SKILLS[0]="idempotency, failure"
        return
    fi

    # Parse YAML with Python (stdlib only — no PyYAML required)
    # Uses a quoted heredoc (<<'PYEOF') to avoid bash/python quoting conflicts
    eval "$(python3 - "${PHASES_CONFIG}" <<'PYEOF'
import re, sys

config_path = sys.argv[1]
try:
    with open(config_path) as f:
        content = f.read()
except FileNotFoundError:
    sys.exit(0)

# Simple YAML parser for flat phase config
phase_block = False
current_phase = None
phases = {}

for line in content.split('\n'):
    stripped = line.strip()
    if stripped == 'phases:':
        phase_block = True
        continue
    if not phase_block:
        continue
    if not stripped or stripped.startswith('#'):
        continue
    # Detect phase number (top-level under phases:)
    indent = len(line) - len(line.lstrip())
    m = re.match(r'^(\d+):\s*$', stripped)
    if m and indent == 2:
        current_phase = m.group(1)
        phases[current_phase] = {}
        continue
    if current_phase and indent >= 4:
        kv = re.match(r"(\w+):\s*[\"']?([^\"']*)[\"'\s]*$", stripped)
        if kv:
            phases[current_phase][kv.group(1)] = kv.group(2).strip()

for num, data in sorted(phases.items(), key=lambda x: int(x[0])):
    name = data.get('name', f'phase-{num}')
    complexity = data.get('complexity', '5')
    group = data.get('group', 'A')
    skills = data.get('skills', 'dto, modularity')
    print(f'PHASE_NAMES[{num}]="{name}"')
    print(f'PHASE_COMPLEXITY[{num}]={complexity}')
    print(f'PHASE_TO_GROUP[{num}]="{group}"')
    print(f'PHASE_SKILLS[{num}]="{skills}"')
PYEOF
)"
}

# ─────────────────────────────────────────────────────────────────────────────
# Color output
# ─────────────────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log_info()    { echo -e "${BLUE}[INFO]${NC}  $*"; }
log_success() { echo -e "${GREEN}[OK]${NC}    $*"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $*"; }
log_header()  { echo -e "\n${BOLD}${CYAN}═══ $* ═══${NC}\n"; }

# ─────────────────────────────────────────────────────────────────────────────
# Utility functions
# ─────────────────────────────────────────────────────────────────────────────

ensure_dirs() {
    mkdir -p "${LOG_DIR}"
    mkdir -p "$(dirname "${STATE_FILE}")"
}

next_model() {
    local model="${MODEL_ROTATE_POOL[${ROTATION_INDEX}]}"
    ROTATION_INDEX=$(( (ROTATION_INDEX + 1) % ${#MODEL_ROTATE_POOL[@]} ))
    echo "${model}"
}

heaviest_phase() {
    local max_phase=""
    local max_score=0
    for phase in "$@"; do
        local score="${PHASE_COMPLEXITY[$phase]:-0}"
        if (( score > max_score )); then
            max_score=$score
            max_phase=$phase
        fi
    done
    echo "${max_phase}"
}

validate_phases() {
    for phase in "$@"; do
        if [[ -z "${PHASE_NAMES[$phase]+x}" ]]; then
            log_error "Invalid phase number: ${phase}. Valid phases: ${!PHASE_NAMES[*]}"
            exit 1
        fi
    done
}

check_clean_worktree() {
    cd "${PROJECT_ROOT}"
    if ! git diff --quiet || ! git diff --cached --quiet; then
        log_error "Working directory has uncommitted changes. Commit or stash first."
        exit 1
    fi
}

check_copilot_cli() {
    if ! command -v copilot &>/dev/null; then
        log_error "Copilot CLI not found. Install with: npm install -g @githubnext/github-copilot-cli"
        exit 1
    fi
}

check_copilot_auth() {
    # Auth precedence: COPILOT_GITHUB_TOKEN > GH_TOKEN > GITHUB_TOKEN
    if [[ -n "${COPILOT_GITHUB_TOKEN:-}" ]]; then
        log_success "Copilot auth: COPILOT_GITHUB_TOKEN is set"; return 0
    fi
    if [[ -n "${GH_TOKEN:-}" ]]; then
        log_success "Copilot auth: GH_TOKEN is set"; return 0
    fi
    if [[ -n "${GITHUB_TOKEN:-}" ]]; then
        log_success "Copilot auth: GITHUB_TOKEN is set"; return 0
    fi
    log_warn "No Copilot auth token found in environment."
    log_warn "Set COPILOT_GITHUB_TOKEN, GH_TOKEN, or GITHUB_TOKEN — or run 'copilot' interactively."
    log_info "Proceeding anyway — agents may fail if unauthenticated."
}

install_gh_cli() {
    # Platform-aware GitHub CLI installation (best-effort).
    if [[ "$(uname)" == "Darwin" ]]; then
        if command -v brew &>/dev/null; then
            log_info "Installing GitHub CLI via Homebrew..."
            brew install gh; return $?
        fi
    elif command -v apt-get &>/dev/null; then
        log_info "Installing GitHub CLI via apt..."
        curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
            | sudo gpg --dearmor -o /usr/share/keyrings/githubcli-archive-keyring.gpg 2>/dev/null
        echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
            | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
        sudo apt update -qq && sudo apt install gh -y; return $?
    elif command -v dnf &>/dev/null; then
        log_info "Installing GitHub CLI via dnf..."
        sudo dnf install 'dnf-command(config-manager)' -y 2>/dev/null || true
        sudo dnf config-manager --add-repo https://cli.github.com/packages/rpm/gh-cli.repo 2>/dev/null || true
        sudo dnf install gh -y; return $?
    fi
    return 1
}

check_gh_cli() {
    # Returns 0 if gh CLI is available (installs if needed), 1 if unavailable.
    # Non-fatal: callers degrade gracefully if this returns 1.
    if command -v gh &>/dev/null; then
        log_success "GitHub CLI found: $(gh --version 2>/dev/null | head -1)"
        if ! gh auth status &>/dev/null; then
            log_warn "GitHub CLI is not authenticated — run: gh auth login"
            log_info "PR creation may fail without authentication."
        fi
        return 0
    fi
    log_info "GitHub CLI not found — attempting install..."
    install_gh_cli
    if command -v gh &>/dev/null; then
        log_success "GitHub CLI installed: $(gh --version 2>/dev/null | head -1)"
        return 0
    fi
    log_warn "GitHub CLI unavailable — PR creation will be skipped."
    log_info "Install manually: https://cli.github.com"
    return 1
}

run_with_timeout() {
    # run_with_timeout SECONDS COMMAND [ARGS...]
    # Returns command exit code, or 124 on timeout.
    # Tries: native timeout(1) → gtimeout (Homebrew coreutils) → shell watchdog.
    local _timeout_s="$1"; shift
    if command -v timeout >/dev/null 2>&1; then
        timeout "$_timeout_s" "$@"; return $?
    elif command -v gtimeout >/dev/null 2>&1; then
        gtimeout "$_timeout_s" "$@"; return $?
    fi
    # Shell watchdog fallback (macOS bash 3.2 safe)
    "$@" &
    local _cmd_pid=$! _elapsed=0
    while kill -0 "$_cmd_pid" 2>/dev/null; do
        sleep 1; _elapsed=$(( _elapsed + 1 ))
        if [[ "$_elapsed" -ge "$_timeout_s" ]]; then
            kill -TERM "$_cmd_pid" 2>/dev/null || true
            local _kw=0
            while kill -0 "$_cmd_pid" 2>/dev/null && [[ "$_kw" -lt 5 ]]; do
                sleep 1; _kw=$(( _kw + 1 ))
            done
            kill -KILL "$_cmd_pid" 2>/dev/null || true
            wait "$_cmd_pid" 2>/dev/null || true
            return 124
        fi
    done
    wait "$_cmd_pid"; return $?
}

update_phase_status() {
    # update_phase_status PHASE KEY VALUE [KEY VALUE ...]
    # Atomically writes fields to .parallel-dev/phase-status.json
    local _phase="$1"; shift
    local _status_file="${PROJECT_ROOT}/.parallel-dev/phase-status.json"
    python3 - "$_phase" "$_status_file" "$@" <<'PYEOF'
import sys, json, os, time
phase, path = sys.argv[1], sys.argv[2]
kvs = sys.argv[3:]
data = {}
os.makedirs(os.path.dirname(path), exist_ok=True)
if os.path.exists(path):
    try: data = json.load(open(path))
    except: data = {}
if "phases" not in data: data["phases"] = {}
if phase not in data["phases"]: data["phases"][phase] = {"phase": phase}
entry = data["phases"][phase]
it = iter(kvs)
for k in it:
    v = next(it)
    entry[k] = None if v == "null" else int(v) if v.lstrip("-").isdigit() else v
entry["updated_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
tmp = path + ".tmp"
with open(tmp, "w") as f: json.dump(data, f, indent=2)
os.replace(tmp, path)
PYEOF
}

# ─────────────────────────────────────────────────────────────────────────────
# Hook runners — language-agnostic delegation
# The orchestrator calls these; each project provides the hook scripts.
# Missing hook → warning + skip (non-blocking). Failing hook → returns 1.
# ─────────────────────────────────────────────────────────────────────────────

run_hook() {
    # run_hook HOOK_NAME WORK_DIR [extra args]
    # Looks for scripts/hooks/<hook-name>.sh relative to work_dir.
    # Returns hook exit code, or 0 if hook is absent.
    local hook_name="$1" work_dir="$2"
    shift 2
    local hook_path="${work_dir}/scripts/hooks/${hook_name}.sh"
    if [[ ! -f "${hook_path}" ]]; then
        log_warn "Hook not found: scripts/hooks/${hook_name}.sh — skipping"
        return 0
    fi
    log_info "Running hook: scripts/hooks/${hook_name}.sh"
    ( cd "${work_dir}" && bash "${hook_path}" "$@" )
    return $?
}

run_env_hook() {
    # Set up project environment (language-specific).
    # Python: creates .venv + pip install. Node: npm ci. Go: go mod download.
    local work_dir="${1:-${PROJECT_ROOT}}"
    run_hook "setup-env" "${work_dir}" || true   # non-fatal
}

run_validation_hook() {
    # Validate syntax / compile / imports (language-specific).
    # Must exit 0 on success, non-zero on failure.
    local work_dir="${1:-${PROJECT_ROOT}}"
    run_hook "validate" "${work_dir}"
}

run_quality_gates_hook() {
    # Run full quality gates: lint, tests, style, arch (language-specific).
    # Must exit 0 when all gates pass, non-zero on any failure.
    local work_dir="${1:-${PROJECT_ROOT}}"
    run_hook "quality-gates" "${work_dir}"
}

# ─────────────────────────────────────────────────────────────────────────────
# Agent execution logging (with retry awareness)
# ─────────────────────────────────────────────────────────────────────────────

log_agent_start() {
    local agent="$1" phase="$2"
    local attempt="${3:-1}" max="${4:-1}"
    local ts
    ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    log_info "[${agent}] attempt ${attempt}/${max} started — phase=${phase} at ${ts}"
    echo "[${ts}] [${agent}] attempt ${attempt}/${max} started — phase=${phase}" >> "${LOG_DIR}/agent-chain.log"
}

log_agent_end() {
    local agent="$1" phase="$2" result="$3"
    local attempt="${4:-1}" max="${5:-1}"
    local ts
    ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    if [[ "${result}" == "0" ]]; then
        log_success "[${agent}] attempt ${attempt}/${max} passed — phase=${phase}"
        echo "[${ts}] [${agent}] attempt ${attempt}/${max} passed — phase=${phase}" >> "${LOG_DIR}/agent-chain.log"
    else
        log_error "[${agent}] attempt ${attempt}/${max} failed — phase=${phase}"
        echo "[${ts}] [${agent}] attempt ${attempt}/${max} FAILED — phase=${phase}" >> "${LOG_DIR}/agent-chain.log"
    fi
}

log_rollback() {
    local phase="$1" reason="$2"
    local ts
    ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    log_error "[rollback] phase=${phase} — ${reason}"
    echo "[${ts}] [rollback] phase=${phase} — ${reason}" >> "${LOG_DIR}/agent-chain.log"
}

# ─────────────────────────────────────────────────────────────────────────────
# Checkpoint & rollback
# ─────────────────────────────────────────────────────────────────────────────

create_checkpoint() {
    local phase_label="$1"
    local work_dir="${2:-$(pwd)}"
    local tag="checkpoint-${phase_label}-pre"
    cd "${work_dir}"
    git tag -f "${tag}" HEAD 2>/dev/null
    log_info "Checkpoint created: ${tag}"
}

rollback_to_checkpoint() {
    local phase_label="$1" reason="$2"
    local work_dir="${3:-$(pwd)}"
    local tag="checkpoint-${phase_label}-pre"
    cd "${work_dir}"
    if git rev-parse --verify "${tag}" &>/dev/null; then
        git reset --hard "${tag}" 2>/dev/null
        log_rollback "${phase_label}" "reset to ${tag} — ${reason}"
    else
        log_error "Checkpoint ${tag} not found — cannot rollback"
    fi
}

cleanup_checkpoint() {
    local phase_label="$1"
    local work_dir="${2:-$(pwd)}"
    local tag="checkpoint-${phase_label}-pre"
    cd "${work_dir}"
    git tag -d "${tag}" 2>/dev/null || true
}

# ─────────────────────────────────────────────────────────────────────────────
# Universal retry framework
# ─────────────────────────────────────────────────────────────────────────────
# execute → validate → fix → re-validate → bounded retry → success OR rollback
#
# Usage:
#   retry_stage <stage_name> <max_retries> <phase_label> <model> <work_dir> \
#               <execute_fn> <validate_fn> <fix_fn>
# ─────────────────────────────────────────────────────────────────────────────

retry_stage() {
    local stage_name="$1" max_retries="$2" phase_label="$3" model="$4" work_dir="$5"
    local execute_fn="$6" validate_fn="$7" fix_fn="$8"
    local attempt=0

    while (( attempt < max_retries )); do
        ((attempt++))
        log_agent_start "${stage_name}" "${phase_label}" "${attempt}" "${max_retries}"

        local exec_rc=0
        ${execute_fn} "${work_dir}" "${model}" "${phase_label}" "${attempt}" || exec_rc=$?

        local valid_rc=0
        ${validate_fn} "${work_dir}" "${model}" "${phase_label}" "${attempt}" || valid_rc=$?

        if (( valid_rc == 0 )); then
            log_agent_end "${stage_name}" "${phase_label}" "0" "${attempt}" "${max_retries}"
            return 0
        fi

        log_agent_end "${stage_name}" "${phase_label}" "1" "${attempt}" "${max_retries}"

        if (( attempt < max_retries )); then
            log_info "[${stage_name}] attempt ${attempt} failed → retrying after fix"
            ${fix_fn} "${work_dir}" "${model}" "${phase_label}" "${attempt}" || true
        fi
    done

    log_error "[${stage_name}] failed after ${max_retries} retries → rollback triggered"
    return 1
}

# ─────────────────────────────────────────────────────────────────────────────
# Stage-specific execute / validate / fix functions
# ─────────────────────────────────────────────────────────────────────────────

# ── Phase Builder ─────────────────────────────────────────────────────────

phase_builder_execute() {
    local work_dir="$1" model="$2" phase_label="$3" attempt="$4"
    local skill_prompt="${_CURRENT_SKILL_PROMPT}"
    cd "${work_dir}"
    local pb_log="${LOG_DIR}/${phase_label}-phase-builder-${attempt}.log"
    copilot \
        -p "Read PHASE_TASK.md and implement all listed phases sequentially. ${skill_prompt}. MANDATORY: Use ONLY skills as primary knowledge source (${CORE_SKILLS}). DO NOT read full documentation unless skills are insufficient — if reading docs, explain why skills are insufficient. For each phase: implement, test, then commit with message 'feat(phase-N): implement <name>'. Follow all constraints in .github/copilot-instructions.md. ${_WORKSPACE_CONSTRAINT}" \
        --agent=phase-builder \
        --model="${model}" \
        --no-ask-user \
        --allow-all-tools \
        --autopilot \
        2>&1 | tee "${pb_log}"
    return ${PIPESTATUS[0]}
}

phase_builder_validate() {
    local work_dir="$1"
    cd "${work_dir}"
    run_validation_hook "${work_dir}"
}

phase_builder_fix() {
    local work_dir="$1" model="$2" phase_label="$3" attempt="$4"
    local skill_prompt="${_CURRENT_SKILL_PROMPT}"
    cd "${work_dir}"
    local fix_log="${LOG_DIR}/${phase_label}-phase-builder-fix-${attempt}.log"
    copilot \
        -p "Phase builder validation failed. Fix compilation and syntax issues ONLY. Do not change architecture. ${skill_prompt}. Commit fixes. ${_WORKSPACE_CONSTRAINT}" \
        --agent=refactor \
        --model="${model}" \
        --no-ask-user \
        --allow-all-tools \
        --autopilot \
        2>&1 | tee "${fix_log}"
    return ${PIPESTATUS[0]}
}

# ── DTO Guardian ──────────────────────────────────────────────────────────

dto_guardian_execute() {
    local work_dir="$1" model="$2" phase_label="$3" attempt="$4"
    cd "${work_dir}"
    local dg_log="${LOG_DIR}/${phase_label}-dto-guardian-${attempt}.log"
    copilot \
        -p "Validate all DTOs in contracts/ against docs/dto_contracts.md. STRICT checks: no missing fields, no extra fields, no type mismatches, all DTOs are immutable. Use skills: dto. MANDATORY: Use ONLY skills as primary knowledge source. DO NOT read full documentation unless skills are insufficient. Report violations and fix them. Commit fixes if any. ${_WORKSPACE_CONSTRAINT}" \
        --agent=dto-guardian \
        --model="${model}" \
        --no-ask-user \
        --allow-all-tools \
        --autopilot \
        2>&1 | tee "${dg_log}"
    return ${PIPESTATUS[0]}
}

dto_guardian_validate() {
    local work_dir="$1"
    cd "${work_dir}"
    local failures=0
    if [[ -d "contracts" ]]; then
        if grep -rn "@dataclass$" contracts/ 2>/dev/null | grep -v "frozen=True" | head -5 | grep -q .; then
            log_error "[dto-validate] Non-frozen dataclass in contracts/"
            ((failures++))
        fi
        if [[ -d "app/modules" ]] && grep -rn "-> dict" app/modules/ 2>/dev/null | head -5 | grep -q .; then
            log_error "[dto-validate] Module returning raw dict instead of frozen DTO"
            ((failures++))
        fi
        if grep -rn "field(default_factory=list\|field(default_factory=dict" contracts/ 2>/dev/null | head -5 | grep -q .; then
            log_error "[dto-validate] Mutable default in frozen DTO"
            ((failures++))
        fi
    fi
    return $(( failures > 0 ? 1 : 0 ))
}

dto_guardian_fix() {
    local work_dir="$1" model="$2" phase_label="$3" attempt="$4"
    cd "${work_dir}"
    local fix_log="${LOG_DIR}/${phase_label}-dto-fix-${attempt}.log"
    copilot \
        -p "DTO validation failed. Fix DTO-specific issues ONLY: ensure all DTOs are immutable, no missing/extra fields, no type mismatches, no mutable defaults. Use skills: dto. Commit fixes. ${_WORKSPACE_CONSTRAINT}" \
        --agent=dto-guardian \
        --model="${model}" \
        --no-ask-user \
        --allow-all-tools \
        --autopilot \
        2>&1 | tee "${fix_log}"
    return ${PIPESTATUS[0]}
}

# ── Integration Agent ─────────────────────────────────────────────────────

integration_execute() {
    local work_dir="$1" model="$2" phase_label="$3" attempt="$4"
    local skill_prompt="${_CURRENT_SKILL_PROMPT}"
    cd "${work_dir}"
    local int_log="${LOG_DIR}/${phase_label}-integration-${attempt}.log"
    copilot \
        -p "Validate module integration for the phases just implemented. STRICT checklist: (1) DTO compatibility across producer/consumer stages, (2) no cross-module imports, (3) no raw SQL in modules — no database driver imports in app/modules, (4) no module calling another module, (5) database access only through orchestrator, (6) deterministic ordering preserved — all collections explicitly sorted, (7) idempotency preserved — content-addressable IDs, (8) no hidden side effects. Use skills: ${CORE_SKILLS}. MANDATORY: Use ONLY skills as primary knowledge source. Report and fix violations. Commit fixes if any. ${_WORKSPACE_CONSTRAINT}" \
        --agent=integration \
        --model="${model}" \
        --no-ask-user \
        --allow-all-tools \
        --autopilot \
        2>&1 | tee "${int_log}"
    return ${PIPESTATUS[0]}
}

integration_validate() {
    local work_dir="$1"
    cd "${work_dir}"
    local failures=0

    if [[ -d "app/modules" ]]; then
        # No cross-module imports
        local cross_imports
        cross_imports=$(find app/modules/ -name '*.py' -exec grep -ln "from app\.modules\." {} \; 2>/dev/null | head -20)
        if [[ -n "${cross_imports}" ]]; then
            while IFS= read -r file; do
                local file_module
                file_module=$(echo "${file}" | sed 's|app/modules/||' | cut -d'/' -f1)
                local imported_modules
                imported_modules=$(grep "from app\.modules\." "${file}" | sed 's/.*from app\.modules\.\([a-z_]*\).*/\1/' | sort -u)
                for imp in ${imported_modules}; do
                    if [[ "${imp}" != "${file_module}" ]]; then
                        log_error "[integration-validate] Cross-module: ${file} → app.modules.${imp}"
                        ((failures++))
                    fi
                done
            done <<< "${cross_imports}"
        fi

        # No DB usage in modules
        if grep -rn "import sqlite3\|import psycopg2\|import asyncpg\|from database" app/modules/ 2>/dev/null | head -5 | grep -q .; then
            log_error "[integration-validate] DB access in app/modules/"
            ((failures++))
        fi

        # No adapter import in modules
        if grep -rn "import adapter" app/modules/ 2>/dev/null | head -5 | grep -q .; then
            log_error "[integration-validate] Adapter import in app/modules/"
            ((failures++))
        fi

        # No print statements
        if grep -rn "^\s*print(" app/modules/ 2>/dev/null | grep -v "# noqa" | head -5 | grep -q .; then
            log_error "[integration-validate] print() in app/modules/"
            ((failures++))
        fi

        # Deterministic ordering warning
        if grep -rn "for .* in .*\.keys()\|for .* in .*\.values()\|for .* in .*\.items()" app/modules/ 2>/dev/null | grep -v "sorted(" | grep -v "# noqa" | head -5 | grep -q .; then
            log_warn "[integration-validate] Possible non-deterministic dict iteration in app/modules/ (verify sorted)"
        fi
    fi

    return $(( failures > 0 ? 1 : 0 ))
}

integration_fix() {
    local work_dir="$1" model="$2" phase_label="$3" attempt="$4"
    local skill_prompt="${_CURRENT_SKILL_PROMPT}"
    cd "${work_dir}"
    local fix_log="${LOG_DIR}/${phase_label}-integration-fix-${attempt}.log"
    copilot \
        -p "Integration validation failed. Fix integration-level issues: remove cross-module imports, remove DB usage from modules, remove print statements, ensure deterministic ordering (sorted collections). Use skills: ${CORE_SKILLS}. Do not change architecture. Commit fixes. ${_WORKSPACE_CONSTRAINT}" \
        --agent=refactor \
        --model="${model}" \
        --no-ask-user \
        --allow-all-tools \
        --autopilot \
        2>&1 | tee "${fix_log}"
    return ${PIPESTATUS[0]}
}

# ── Security Auditor ──────────────────────────────────────────────────────

security_auditor_execute() {
    local work_dir="$1" model="$2" phase_label="$3" attempt="$4"
    cd "${work_dir}"
    local sa_log="${LOG_DIR}/${phase_label}-security-auditor-${attempt}.log"
    copilot \
        -p "Perform a comprehensive OWASP-aware security audit of all code just implemented. Check: (1) injection vulnerabilities (SQL, command, path traversal), (2) insecure deserialization, (3) hardcoded secrets or credentials, (4) missing input validation at system boundaries, (5) broken access control patterns, (6) insecure dependencies or imports, (7) logging of sensitive data. Use skills: security-audit, ${CORE_SKILLS}. MANDATORY: Use ONLY skills as primary knowledge source. Report ALL findings with severity (critical/high/medium/low). Fix critical and high severity issues immediately. Commit fixes if any. ${_WORKSPACE_CONSTRAINT}" \
        --agent=security-auditor \
        --model="${model}" \
        --no-ask-user \
        --allow-all-tools \
        --autopilot \
        2>&1 | tee "${sa_log}"
    return ${PIPESTATUS[0]}
}

security_auditor_validate() {
    local work_dir="$1"
    cd "${work_dir}"
    local failures=0

    # Check for common hardcoded secret patterns
    local secret_patterns=("password\s*=\s*['\"][^'\"]{4,}" "api_key\s*=\s*['\"][^'\"]{4,}"
                           "secret\s*=\s*['\"][^'\"]{4,}" "token\s*=\s*['\"][^'\"]{4,}")
    for pat in "${secret_patterns[@]}"; do
        if grep -rniE "${pat}" app/ src/ 2>/dev/null \
                | grep -v '_test\.' | grep -v 'test/' | grep -v '.example' \
                | grep -v '# noqa\|//\s*nosec\|//\s*nosemgrep' \
                | head -3 | grep -q .; then
            log_warn "[security-validate] Possible hardcoded secret (pattern: ${pat}) — review required"
        fi
    done

    # Check for SQL string interpolation (parameterized queries required)
    if grep -rn 'f".*SELECT\|f".*INSERT\|f".*UPDATE\|f".*DELETE\|%s.*SELECT\|%s.*INSERT' \
            app/ src/ 2>/dev/null | grep -v '.example' | head -3 | grep -q .; then
        log_error "[security-validate] SQL string interpolation detected — use parameterized queries"
        ((failures++))
    fi

    # Check for shell injection via subprocess with string concat
    if grep -rn 'subprocess.*shell=True\|os\.system(' app/ src/ 2>/dev/null | head -3 | grep -q .; then
        log_warn "[security-validate] shell=True or os.system() detected — verify input is sanitized"
    fi

    return $(( failures > 0 ? 1 : 0 ))
}

security_auditor_fix() {
    local work_dir="$1" model="$2" phase_label="$3" attempt="$4"
    cd "${work_dir}"
    local fix_log="${LOG_DIR}/${phase_label}-security-fix-${attempt}.log"
    copilot \
        -p "Security validation found critical issues. Fix ONLY the security violations: replace string-interpolated SQL with parameterized queries, remove hardcoded secrets (use config/env), review shell injection risks. Use skills: security-audit, ${CORE_SKILLS}. Do not change architecture. Commit fixes. ${_WORKSPACE_CONSTRAINT}" \
        --agent=refactor \
        --model="${model}" \
        --no-ask-user \
        --allow-all-tools \
        --autopilot \
        2>&1 | tee "${fix_log}"
    return ${PIPESTATUS[0]}
}

# ── Test Builder ──────────────────────────────────────────────────────────

test_builder_execute() {
    local work_dir="$1" model="$2" phase_label="$3" attempt="$4"
    local skill_prompt="${_CURRENT_SKILL_PROMPT}"
    cd "${work_dir}"
    local tb_log="${LOG_DIR}/${phase_label}-test-builder-${attempt}.log"
    copilot \
        -p "Generate or validate tests for all modules just implemented. Requirements: (1) all public module functions must have unit tests, (2) tests must be runnable without GPU, network, or real data files, (3) use AAA pattern (Arrange/Act/Assert), (4) mock all external dependencies, (5) test happy path and at least one error path per function, (6) deterministic — no random data, no time-dependent assertions. Use skills: test-generation, test-driven-development, ${CORE_SKILLS}. MANDATORY: Use ONLY skills as primary knowledge source. Run tests after generation to verify they pass. Fix any failing tests before committing. Commit all test files and fixes. ${_WORKSPACE_CONSTRAINT}" \
        --agent=test-builder \
        --model="${model}" \
        --no-ask-user \
        --allow-all-tools \
        --autopilot \
        2>&1 | tee "${tb_log}"
    return ${PIPESTATUS[0]}
}

test_builder_validate() {
    local work_dir="$1"
    cd "${work_dir}"
    run_validation_hook "${work_dir}"
}

test_builder_fix() {
    local work_dir="$1" model="$2" phase_label="$3" attempt="$4"
    cd "${work_dir}"
    local fix_log="${LOG_DIR}/${phase_label}-test-fix-${attempt}.log"
    copilot \
        -p "Test validation failed. Fix ONLY the failing tests: correct assertions, fix mock setups, ensure tests are deterministic and network-free. Do not change production code architecture. Use skills: test-generation, test-driven-development, ${CORE_SKILLS}. Commit fixes. ${_WORKSPACE_CONSTRAINT}" \
        --agent=refactor \
        --model="${model}" \
        --no-ask-user \
        --allow-all-tools \
        --autopilot \
        2>&1 | tee "${fix_log}"
    return ${PIPESTATUS[0]}
}

# ── Protected File Enforcement ────────────────────────────────────────────

validate_protected_files() {
    local work_dir="$1" phase_label="$2"
    cd "${work_dir}"
    local violations=0

    if ! git rev-parse --is-inside-work-tree &>/dev/null; then return 0; fi

    local base_branch="main"
    if ! git rev-parse --verify "${base_branch}" &>/dev/null; then return 0; fi

    # contracts/ — additive only
    local modified_contracts
    modified_contracts=$(git diff --name-only --diff-filter=M "${base_branch}" -- contracts/ 2>/dev/null || true)
    if [[ -n "${modified_contracts}" ]]; then
        log_error "[protected-files] Existing contracts modified (additive-only policy):"
        echo "${modified_contracts}" | while read -r f; do log_error "  ${f}"; done
        ((violations++))
    fi

    # database/ — only Phase 0 allowed
    local db_changes
    db_changes=$(git diff --name-only "${base_branch}" -- database/ 2>/dev/null || true)
    if [[ -n "${db_changes}" ]]; then
        if [[ "${phase_label}" != *"phase-0"* ]] && [[ "${phase_label}" != *"group-0"* ]]; then
            log_error "[protected-files] database/ modified outside Phase 0:"
            echo "${db_changes}" | while read -r f; do log_error "  ${f}"; done
            ((violations++))
        fi
    fi

    # docs/ — read-only
    local doc_changes
    doc_changes=$(git diff --name-only "${base_branch}" -- docs/ 2>/dev/null || true)
    if [[ -n "${doc_changes}" ]]; then
        log_error "[protected-files] docs/ modified (read-only policy):"
        echo "${doc_changes}" | while read -r f; do log_error "  ${f}"; done
        ((violations++))
    fi

    return $(( violations > 0 ? 1 : 0 ))
}

# ─────────────────────────────────────────────────────────────────────────────
# Agent chaining pipeline (with bounded retries + checkpoint/rollback)
# ─────────────────────────────────────────────────────────────────────────────

run_agent_pipeline() {
    local work_dir="$1" model="$2" phase_label="$3"
    shift 3
    local phase_nums=("$@")
    local phase_skills_csv="${PHASE_SKILLS[${phase_nums[0]}]:-${CORE_SKILLS}}"
    export _CURRENT_SKILL_PROMPT="Use skills: ${CORE_SKILLS}, ${phase_skills_csv}"

    cd "${work_dir}"

    # Checkpoint before pipeline
    create_checkpoint "${phase_label}" "${work_dir}"

    # Step 1: phase-builder
    if ! retry_stage "phase-builder" "${MAX_RETRIES_PHASE_BUILDER}" \
            "${phase_label}" "${model}" "${work_dir}" \
            phase_builder_execute phase_builder_validate phase_builder_fix; then
        rollback_to_checkpoint "${phase_label}" "phase-builder exceeded ${MAX_RETRIES_PHASE_BUILDER} retries" "${work_dir}"
        return 1
    fi

    # Step 2: dto-guardian
    if ! retry_stage "dto-guardian" "${MAX_RETRIES_DTO}" \
            "${phase_label}" "${model}" "${work_dir}" \
            dto_guardian_execute dto_guardian_validate dto_guardian_fix; then
        rollback_to_checkpoint "${phase_label}" "dto-guardian exceeded ${MAX_RETRIES_DTO} retries" "${work_dir}"
        return 1
    fi

    # Step 3: integration
    if ! retry_stage "integration" "${MAX_RETRIES_INTEGRATION}" \
            "${phase_label}" "${model}" "${work_dir}" \
            integration_execute integration_validate integration_fix; then
        rollback_to_checkpoint "${phase_label}" "integration exceeded ${MAX_RETRIES_INTEGRATION} retries" "${work_dir}"
        return 1
    fi

    # Step 4: security-auditor
    if ! retry_stage "security-auditor" "${MAX_RETRIES_SECURITY}" \
            "${phase_label}" "${model}" "${work_dir}" \
            security_auditor_execute security_auditor_validate security_auditor_fix; then
        rollback_to_checkpoint "${phase_label}" "security-auditor exceeded ${MAX_RETRIES_SECURITY} retries" "${work_dir}"
        return 1
    fi

    # Step 5: test-builder
    if ! retry_stage "test-builder" "${MAX_RETRIES_TESTS}" \
            "${phase_label}" "${model}" "${work_dir}" \
            test_builder_execute test_builder_validate test_builder_fix; then
        rollback_to_checkpoint "${phase_label}" "test-builder exceeded ${MAX_RETRIES_TESTS} retries" "${work_dir}"
        return 1
    fi

    # Step 6: protected file enforcement
    if ! validate_protected_files "${work_dir}" "${phase_label}"; then
        log_error "Protected file policy violated — rollback"
        rollback_to_checkpoint "${phase_label}" "protected file policy violation" "${work_dir}"
        return 1
    fi

    # Step 7: quality gates → refactor if needed
    if ! run_quality_gates "${work_dir}"; then
        local qg_attempt=0
        while (( qg_attempt < MAX_REMEDIATION_RETRIES )); do
            ((qg_attempt++))
            log_info "[quality-gates] remediation attempt ${qg_attempt}/${MAX_REMEDIATION_RETRIES}"
            log_agent_start "refactor" "${phase_label}" "${qg_attempt}" "${MAX_REMEDIATION_RETRIES}"

            local ref_log="${LOG_DIR}/${phase_label}-refactor-${qg_attempt}.log"
            copilot \
                -p "Quality gates failed. Fix all violations: lint errors, test failures, cross-module imports, raw SQL in modules, print statements. Do not change architecture. Use skills: ${CORE_SKILLS}. MANDATORY: Use ONLY skills as primary knowledge source. Commit fixes. ${_WORKSPACE_CONSTRAINT}" \
                --agent=refactor \
                --model="${model}" \
                --no-ask-user \
                --allow-all-tools \
                --autopilot \
                2>&1 | tee "${ref_log}"
            local ref_rc=${PIPESTATUS[0]}
            log_agent_end "refactor" "${phase_label}" "${ref_rc}" "${qg_attempt}" "${MAX_REMEDIATION_RETRIES}"

            if run_quality_gates "${work_dir}"; then
                break
            fi

            if (( qg_attempt >= MAX_REMEDIATION_RETRIES )); then
                log_error "[quality-gates] failed after ${MAX_REMEDIATION_RETRIES} remediation attempts → rollback"
                rollback_to_checkpoint "${phase_label}" "quality gates exceeded ${MAX_REMEDIATION_RETRIES} remediations" "${work_dir}"
                return 1
            fi
        done
    fi

    cleanup_checkpoint "${phase_label}" "${work_dir}"
    log_success "Agent pipeline completed for ${phase_label}"
    unset _CURRENT_SKILL_PROMPT
    return 0
}

# ─────────────────────────────────────────────────────────────────────────────
# State management
# ─────────────────────────────────────────────────────────────────────────────

save_state() {
    local mode="$1"
    local phases="$2"
    local integration_branch="$3"
    shift 3

    local branches_json="["
    local first=true
    for b in "$@"; do
        if $first; then first=false; else branches_json+=","; fi
        branches_json+="\"${b}\""
    done
    branches_json+="]"

    # Determine model routing for this mode
    local heavy_model rotation_pool_str
    if (( mode == 1 )); then
        heavy_model="${MODEL_HEAVY}"
    else
        heavy_model="${MODEL_HEAVY_LITE}"
    fi
    rotation_pool_str=$(printf ',"%s"' "${MODEL_ROTATE_POOL[@]}")
    rotation_pool_str="[${rotation_pool_str:1}]"

    cat > "${STATE_FILE}" <<EOF
{
    "mode": ${mode},
    "phases": "${phases}",
    "integration_branch": "${integration_branch}",
    "branches": ${branches_json},
    "started_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "status": "running",
    "model_heavy": "${heavy_model}",
    "model_rotation_pool": ${rotation_pool_str}
}
EOF
}

update_state_status() {
    local new_status="$1"
    if [[ -f "${STATE_FILE}" ]]; then
        local tmp
        tmp=$(mktemp)
        sed "s/\"status\": \"[^\"]*\"/\"status\": \"${new_status}\"/" "${STATE_FILE}" > "${tmp}"
        mv "${tmp}" "${STATE_FILE}"
    fi
}

# ─────────────────────────────────────────────────────────────────────────────
# PHASE_TASK.md generation
# ─────────────────────────────────────────────────────────────────────────────

generate_phase_task() {
    local output_path="$1"
    shift
    local phases=("$@")

    cat > "${output_path}" <<'HEADER'
# Phase Implementation Task

> This file was auto-generated by `run_parallel.sh`.
> Read this file completely before starting implementation.

## Mandatory Skill-Based Execution

**CRITICAL:** You MUST use skills as your PRIMARY knowledge source.

1. Use these skills from `.github/skills/<name>/SKILL.md` (all are auto-loaded by Copilot):
   - `dto` — DTO registry, validation, anti-patterns
   - `pipeline` — Stage sequence, DTO flow map
   - `modularity` — Module boundaries, import rules
   - `determinism` — No-randomness enforcement
   - `idempotency` — Content-addressable IDs, ON CONFLICT DO NOTHING
   - `failure` — Retry logic, abort thresholds, graceful degradation
   - `config-validation` — Config-driven parameters, no hardcoded values
   - `code-quality` — Type annotations, logging, standards
   - `coding-standards` — Naming conventions, function design, language idioms
   - `database-portability` — Engine-agnostic SQL patterns
   - `docs-sync` — Detect drift between code and documentation
   - `conflict-resolution` — Git merge conflict resolution
   - `token-optimization` — Context loading optimization
   - `running-prompt` — Structured task execution workflow
   - `security-audit` — OWASP security auditing, injection prevention
   - `test-generation` — Test patterns, coverage requirements
   - `vertical-slice` — Feature-per-folder architecture
   - `api-design` — REST/gRPC API patterns, versioning
   - `project-scaffold` — Project initialization validation
   - `dependency-analysis` — Import graph, coupling analysis
   - `migration-management` — Database migration best practices
   - `performance-optimization` — Profiling, query optimization
   - `caveman` — Ultra-compressed output (~75% fewer tokens)
   - `brainstorming` — Design-first gate before implementation
   - `writing-plans` — Break work into bite-sized tasks
   - `subagent-driven-development` — Fresh subagent per task + 2-stage review
   - `test-driven-development` — RED-GREEN-REFACTOR cycle enforcement
   - `rtk` — Token-efficient CLI proxy (60-90% savings)
2. Read `.github/copilot-instructions.md` for hard architectural constraints.
3. **DO NOT read full documentation** unless skills are insufficient. If you do read docs, explain WHY skills were not enough.
4. Only consult `docs/implementation_roadmap.md` for specific phase details NOT covered by skills.
5. Implement each phase below sequentially, committing after each one.
6. Run tests after each phase.

## Protected File Policy (STRICT)

- `contracts/*` — **additive only**. You may ADD new DTOs. You MUST NOT modify existing DTO fields. Violation = pipeline rollback.
- `database/*` — **Phase 0 only**. No other phase may modify database files. Violation = pipeline rollback.
- `docs/*` — **read-only**. No modifications allowed. Violation = pipeline rollback.
- Do NOT modify files outside your owned directories (see ownership below).

## Deterministic Ordering

- All collections (lists, dicts, sets) MUST be explicitly sorted before iteration or output.
- No implicit ordering. No `random`. No non-deterministic patterns.

## Agent Pipeline

After you finish implementing, the following agents run automatically with **bounded retries**:
1. **dto-guardian** — validates all DTOs (frozen, correct fields, no drift) — up to 5 retries
2. **integration** — validates module wiring, no cross-module imports, no raw SQL — up to 5 retries
3. **security-auditor** — OWASP security audit, fixes critical/high issues — up to 3 retries
4. **test-builder** — generates/validates tests for all public functions — up to 3 retries
5. **refactor** — fixes quality gate failures (if needed) — up to 3 retries
6. If any stage exceeds its retry limit → **rollback to checkpoint**

---

HEADER

    for phase in "${phases[@]}"; do
        local name="${PHASE_NAMES[$phase]:-phase-${phase}}"
        local skills="${PHASE_SKILLS[$phase]:-${CORE_SKILLS}}"

        cat >> "${output_path}" <<EOF
## Phase ${phase} — ${name}

**Required skills:** ${skills}

**Skill-first approach (MANDATORY):**
- Pipeline stage ordering and DTO flow → \`pipeline\` skill
- DTO field definitions and constraints → \`dto\` skill
- Module boundary rules → \`modularity\` skill
- Phase-specific patterns → see required skills above
- DO NOT read full docs unless skills are insufficient — explain why if you do

**Only if skills are insufficient**, consult \`docs/implementation_roadmap.md\` → Phase ${phase} section.

**Constraints:**
- All DTOs must be immutable types in \`contracts/\`
- All database access through \`database/adapter.*\` only — orchestrator calls adapter, modules NEVER touch the database
- No unstructured console output — use structured logging
- No cross-module imports — only \`contracts/\` types
- No module may call another module — only the orchestrator calls modules
- All IDs are content-addressable (SHA256-based)
- All collections must be explicitly sorted (deterministic ordering)
- Tests must work without GPU, network, or real data files
- Protected files: \`contracts/*\` (additive only), \`database/*\` (Phase 0 only), \`docs/*\` (read-only)

**After implementation:**
1. Run tests and fix all failures
2. Verify the project compiles/imports successfully
3. Commit with message: \`feat(phase-${phase}): implement ${name}\`

---

EOF
    done
}

# ─────────────────────────────────────────────────────────────────────────────
# Quality gates — delegated to scripts/hooks/quality-gates.sh
# ─────────────────────────────────────────────────────────────────────────────

run_quality_gates() {
    local work_dir="${1:-${PROJECT_ROOT}}"
    log_header "Quality Gates"
    run_quality_gates_hook "${work_dir}"
    local rc=$?
    if (( rc != 0 )); then
        log_error "Quality gates: FAILED (hook exit code ${rc})"
    else
        log_success "All quality gates passed"
    fi
    return ${rc}
}

# ─────────────────────────────────────────────────────────────────────────────
# Mode 1 — Full Parallel
# ─────────────────────────────────────────────────────────────────────────────

run_mode_1() {
    local phases=("$@")
    local heaviest
    heaviest=$(heaviest_phase "${phases[@]}")
    local branches=()
    local pids=()

    log_header "Mode 1 — Full Parallel"
    log_info "Phases: ${phases[*]}"
    log_info "Heaviest phase: ${heaviest} (gets ${MODEL_HEAVY})"
    log_info "Max parallel agents: ${MAX_PARALLEL_AGENTS}"

    check_clean_worktree
    mkdir -p "${WORKTREE_BASE}"

    cd "${PROJECT_ROOT}"
    local base_commit
    base_commit=$(git rev-parse HEAD)

    for phase in "${phases[@]}"; do
        local branch="track/phase-${phase}"
        local worktree_dir="${WORKTREE_BASE}/phase-${phase}"

        log_info "Creating worktree for Phase ${phase}..."
        git branch -D "${branch}" 2>/dev/null || true
        git branch "${branch}" "${base_commit}"

        if [[ -d "${worktree_dir}" ]]; then
            git worktree remove "${worktree_dir}" --force 2>/dev/null || true
        fi
        git worktree add "${worktree_dir}" "${branch}"
        log_info "  Setting up environment in worktree (Phase ${phase})..."
        run_hook "setup-env" "${worktree_dir}" 2>&1 | tail -5 \
            || log_warn "  setup-env hook had issues — agent will proceed"

        branches+=("${branch}")
    done

    save_state 1 "${phases[*]}" "${INTEGRATION_BRANCH}" "${branches[@]}"

    local active_pids=()
    local phase_for_pid=()
    local failed_phases=()

    for phase in "${phases[@]}"; do
        local worktree_dir="${WORKTREE_BASE}/phase-${phase}"
        local task_file="${worktree_dir}/PHASE_TASK.md"
        local model

        if [[ "${phase}" == "${heaviest}" ]]; then
            model="${MODEL_HEAVY}"
        else
            model=$(next_model)
        fi

        generate_phase_task "${task_file}" "${phase}"

        # Resource control: wait if at MAX_PARALLEL_AGENTS
        while (( ${#active_pids[@]} >= MAX_PARALLEL_AGENTS )); do
            local new_active=()
            local new_phase_map=()
            for i in "${!active_pids[@]}"; do
                if kill -0 "${active_pids[$i]}" 2>/dev/null; then
                    new_active+=("${active_pids[$i]}")
                    new_phase_map+=("${phase_for_pid[$i]}")
                else
                    wait "${active_pids[$i]}" 2>/dev/null && true
                    local rc=$?
                    if (( rc != 0 )); then
                        failed_phases+=("${phase_for_pid[$i]}")
                    fi
                fi
            done
            active_pids=("${new_active[@]}")
            phase_for_pid=("${new_phase_map[@]}")
            if (( ${#active_pids[@]} >= MAX_PARALLEL_AGENTS )); then
                sleep 5
            fi
        done

        log_info "Launching agent pipeline for Phase ${phase} (model: ${model})..."

        (
            update_phase_status "phase-${phase}" state "running" model "${model}" \
                started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
            run_hook "activate-env" "${worktree_dir}" 2>/dev/null || true
            run_agent_pipeline "${worktree_dir}" "${model}" "phase-${phase}" "${phase}"
            local _rc=$?
            if (( _rc == 0 )); then
                update_phase_status "phase-${phase}" state "complete" exit_code "0"
            elif (( _rc == 124 )); then
                update_phase_status "phase-${phase}" state "timed_out" exit_code "124"
            else
                update_phase_status "phase-${phase}" state "failed" exit_code "${_rc}"
            fi
            exit ${_rc}
        ) &
        active_pids+=($!)
        phase_for_pid+=("${phase}")
        pids+=($!)

        log_info "  PID: ${pids[-1]}"
    done

    # Wait for remaining agent pipelines
    log_header "Waiting for remaining agent pipeline(s)..."
    for i in "${!active_pids[@]}"; do
        local pid="${active_pids[$i]}"
        local phase="${phase_for_pid[$i]}"
        if ! wait "${pid}" 2>/dev/null; then
            failed_phases+=("${phase}")
        fi
    done

    if (( ${#failed_phases[@]} > 0 )); then
        log_error "${#failed_phases[@]} phase(s) failed after all retries: ${failed_phases[*]}"
        log_error "Failed phases were rolled back to their checkpoints."
        update_state_status "partial_failure"
    else
        update_state_status "agents_complete"
        log_success "All agents finished. Run './scripts/run_parallel.sh merge' to integrate."
    fi
}

# ─────────────────────────────────────────────────────────────────────────────
# Mode 2 — Token-Optimized (Sequential)
# ─────────────────────────────────────────────────────────────────────────────

run_mode_2() {
    local phases=("$@")
    local branch="track/group-$(IFS=-; echo "${phases[*]}")"

    log_header "Mode 2 — Token-Optimized (Sequential)"
    log_info "Phases: ${phases[*]}"
    log_info "Single session, sequential execution"

    check_clean_worktree
    cd "${PROJECT_ROOT}"

    local base_commit
    base_commit=$(git rev-parse HEAD)

    git branch -D "${branch}" 2>/dev/null || true
    git checkout -b "${branch}" "${base_commit}"

    local task_file="${PROJECT_ROOT}/PHASE_TASK.md"
    generate_phase_task "${task_file}" "${phases[@]}"

    save_state 2 "${phases[*]}" "${INTEGRATION_BRANCH}" "${branch}"

    local model="${MODEL_HEAVY_LITE}"

    local log_file="${LOG_DIR}/group-$(IFS=-; echo "${phases[*]}").log"

    log_info "Launching agent pipeline (model: ${model})..."
    log_info "  Log: ${log_file}"

    local phase_label="group-$(IFS=-; echo "${phases[*]}")"
    update_phase_status "${phase_label}" state "running" model "${model}" \
        started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    run_hook "activate-env" "${PROJECT_ROOT}" 2>/dev/null || true
    run_agent_pipeline "${PROJECT_ROOT}" "${model}" "${phase_label}" "${phases[@]}"
    local _rc=$?
    if (( _rc == 0 )); then
        update_phase_status "${phase_label}" state "complete" exit_code "0"
    elif (( _rc == 124 )); then
        update_phase_status "${phase_label}" state "timed_out" exit_code "124"
    else
        update_phase_status "${phase_label}" state "failed" exit_code "${_rc}"
    fi

    rm -f "${task_file}"

    update_state_status "agents_complete"
    log_success "Sequential session complete. Run './scripts/run_parallel.sh merge' to finalize."
}

# ─────────────────────────────────────────────────────────────────────────────
# Mode 3 — Hybrid
# ─────────────────────────────────────────────────────────────────────────────

run_mode_3() {
    local phases=("$@")

    log_header "Mode 3 — Hybrid (Parallel Groups, Sequential Within)"
    log_info "Phases: ${phases[*]}"

    check_clean_worktree
    cd "${PROJECT_ROOT}"

    local base_commit
    base_commit=$(git rev-parse HEAD)

    # Group phases by PHASE_TO_GROUP mapping
    declare -A groups_map
    for phase in "${phases[@]}"; do
        local group="${PHASE_TO_GROUP[$phase]:-A}"
        if [[ -z "${groups_map[$group]+x}" ]]; then
            groups_map[$group]="${phase}"
        else
            groups_map[$group]="${groups_map[$group]} ${phase}"
        fi
    done

    local sorted_groups
    sorted_groups=$(echo "${!groups_map[@]}" | tr ' ' '\n' | sort)

    local branches=()
    local pids=()

    mkdir -p "${WORKTREE_BASE}"

    local heaviest
    heaviest=$(heaviest_phase "${phases[@]}")

    for group in ${sorted_groups}; do
        local group_phases=(${groups_map[$group]})
        local phase_list
        phase_list=$(IFS=-; echo "${group_phases[*]}")
        local branch="track/group-${phase_list}"
        local worktree_dir="${WORKTREE_BASE}/group-${phase_list}"

        log_info "Group ${group}: Phases [${group_phases[*]}]"

        git branch -D "${branch}" 2>/dev/null || true
        git branch "${branch}" "${base_commit}"

        if [[ -d "${worktree_dir}" ]]; then
            git worktree remove "${worktree_dir}" --force 2>/dev/null || true
        fi
        git worktree add "${worktree_dir}" "${branch}"
        log_info "  Setting up environment in worktree (Group ${group})..."
        run_hook "setup-env" "${worktree_dir}" 2>&1 | tail -5 \
            || log_warn "  setup-env hook had issues for Group ${group}"

        branches+=("${branch}")
    done

    save_state 3 "${phases[*]}" "${INTEGRATION_BRANCH}" "${branches[@]}"

    local active_pids=()
    local group_for_pid=()
    local failed_groups=()

    for group in ${sorted_groups}; do
        local group_phases=(${groups_map[$group]})
        local phase_list
        phase_list=$(IFS=-; echo "${group_phases[*]}")
        local worktree_dir="${WORKTREE_BASE}/group-${phase_list}"
        local task_file="${worktree_dir}/PHASE_TASK.md"

        local model
        local use_heavy=false
        for p in "${group_phases[@]}"; do
            if [[ "${p}" == "${heaviest}" ]]; then
                use_heavy=true
                break
            fi
        done
        if $use_heavy; then
            model="${MODEL_HEAVY_LITE}"
        else
            model=$(next_model)
        fi

        generate_phase_task "${task_file}" "${group_phases[@]}"

        # Resource control
        while (( ${#active_pids[@]} >= MAX_PARALLEL_AGENTS )); do
            local new_active=()
            local new_group_map=()
            for i in "${!active_pids[@]}"; do
                if kill -0 "${active_pids[$i]}" 2>/dev/null; then
                    new_active+=("${active_pids[$i]}")
                    new_group_map+=("${group_for_pid[$i]}")
                else
                    wait "${active_pids[$i]}" 2>/dev/null && true
                    local rc=$?
                    if (( rc != 0 )); then
                        failed_groups+=("${group_for_pid[$i]}")
                    fi
                fi
            done
            active_pids=("${new_active[@]}")
            group_for_pid=("${new_group_map[@]}")
            if (( ${#active_pids[@]} >= MAX_PARALLEL_AGENTS )); then
                sleep 5
            fi
        done

        log_info "Launching Group ${group} agent pipeline (model: ${model}, phases: ${group_phases[*]})..."

        (
            update_phase_status "group-${phase_list}" state "running" model "${model}" \
                started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
            run_hook "activate-env" "${worktree_dir}" 2>/dev/null || true
            run_agent_pipeline "${worktree_dir}" "${model}" "group-${phase_list}" "${group_phases[@]}"
            local _rc=$?
            if (( _rc == 0 )); then
                update_phase_status "group-${phase_list}" state "complete" exit_code "0"
            elif (( _rc == 124 )); then
                update_phase_status "group-${phase_list}" state "timed_out" exit_code "124"
            else
                update_phase_status "group-${phase_list}" state "failed" exit_code "${_rc}"
            fi
            exit ${_rc}
        ) &
        active_pids+=($!)
        group_for_pid+=("${group}")
        pids+=($!)

        log_info "  PID: ${pids[-1]}"
    done

    # Wait for remaining group agents
    log_header "Waiting for remaining group agent(s)..."
    for i in "${!active_pids[@]}"; do
        local pid="${active_pids[$i]}"
        local group="${group_for_pid[$i]}"
        if ! wait "${pid}" 2>/dev/null; then
            failed_groups+=("${group}")
        fi
    done

    if (( ${#failed_groups[@]} > 0 )); then
        log_error "${#failed_groups[@]} group(s) failed after all retries: ${failed_groups[*]}"
        log_error "Failed groups were rolled back to their checkpoints."
        update_state_status "partial_failure"
    else
        update_state_status "agents_complete"
        log_success "All group agents finished. Run './scripts/run_parallel.sh merge' to integrate."
    fi
}

# ─────────────────────────────────────────────────────────────────────────────
# Merge command (with bounded retries + global validation)
# ─────────────────────────────────────────────────────────────────────────────

cmd_merge() {
    log_header "Merge & Integration"

    if [[ ! -f "${STATE_FILE}" ]]; then
        log_error "No active parallel session found. Run 'start' first."
        exit 1
    fi

    cd "${PROJECT_ROOT}"

    local mode
    mode=$(python3 -c "import json; print(json.load(open('${STATE_FILE}'))['mode'])")
    local integration_branch
    integration_branch=$(python3 -c "import json; print(json.load(open('${STATE_FILE}'))['integration_branch'])")

    if (( mode == 2 )); then
        local mode2_branch
        mode2_branch=$(python3 -c "import json; d=json.load(open('${STATE_FILE}')); print(d['branches'][0])")
        local phases_str
        phases_str=$(python3 -c "import json; print(json.load(open('${STATE_FILE}'))['phases'])")

        log_info "Mode 2 — post-pipeline validation for branch ${mode2_branch}..."
        cd "${PROJECT_ROOT}"
        git checkout "${mode2_branch}" 2>/dev/null || true

        run_post_merge_review "${PROJECT_ROOT}" "${mode2_branch}" || {
            update_state_status "review_failed"
            exit 1
        }
        run_docs_sync "${PROJECT_ROOT}"

        log_header "Global Validation"
        update_phase_status "global-validation" state "running" \
            started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        if run_global_validation "${PROJECT_ROOT}"; then
            update_phase_status "global-validation" state "complete" exit_code "0"
            update_state_status "passed"
        elif ! run_remediation "${PROJECT_ROOT}"; then
            update_phase_status "global-validation" state "failed" exit_code "1"
            exit 1
        fi

        create_pr "${mode2_branch}" "${phases_str}"
        log_success "Autonomous integration complete."
        return
    fi

    # Mode 1 or 3: merge worktree branches
    local branches
    branches=$(python3 -c "import json; print(' '.join(json.load(open('${STATE_FILE}'))['branches']))")

    git checkout main
    git branch -D "${integration_branch}" 2>/dev/null || true
    git checkout -b "${integration_branch}"

    local merge_failures=0
    for branch in ${branches}; do
        log_info "Merging ${branch}..."
        if git merge "${branch}" --no-edit 2>/dev/null; then
            log_success "Merged ${branch}"
            if ! validate_merge "${PROJECT_ROOT}"; then
                log_error "Post-merge validation failed for ${branch}"
                ((merge_failures++))
                continue
            fi
        else
            log_error "Merge conflict in ${branch}"
            local merge_resolved=false
            local merge_attempt=0
            while (( merge_attempt < MAX_RETRIES_MERGE )); do
                ((merge_attempt++))
                log_agent_start "integration" "merge-${branch}" "${merge_attempt}" "${MAX_RETRIES_MERGE}"

                local conflict_log="${LOG_DIR}/merge-conflict-${branch//\//-}-${merge_attempt}.log"
                local conflict_model
                conflict_model=$(next_model)

                copilot \
                    -p "Merge conflict detected when merging branch ${branch} (attempt ${merge_attempt}/${MAX_RETRIES_MERGE}). Resolve ALL conflicts using the union strategy — preserve ALL code from both sides, nothing is discarded. Use skills: conflict-resolution, dto, pipeline, modularity. MANDATORY: Use ONLY skills as primary knowledge source. Resolution rules: (1) contracts/ — combine all DTO definitions, keep all DTOs (additive only); (2) app/modules/ — each module owns its directory, keep both modules' implementations; (3) tests/ — combine all test files from all phases; (4) app/orchestrator/ — later phase's wiring changes win for stage registration. Stage all resolved files (git add -A) and commit. ${_WORKSPACE_CONSTRAINT}" \
                    --agent=conflict-resolver \
                    --model="${conflict_model}" \
                    --no-ask-user \
                    --allow-all-tools \
                    --autopilot \
                    2>&1 | tee "${conflict_log}"
                local conflict_rc=${PIPESTATUS[0]}
                log_agent_end "integration" "merge-${branch}" "${conflict_rc}" "${merge_attempt}" "${MAX_RETRIES_MERGE}"

                if (( conflict_rc == 0 )) && ! git diff --name-only --diff-filter=U 2>/dev/null | head -1 | grep -q .; then
                    git add -A 2>/dev/null || true
                    if ! git diff --cached --quiet 2>/dev/null; then
                        git commit --no-edit -m "merge: resolve conflicts from ${branch} via integration agent (attempt ${merge_attempt})" 2>/dev/null
                    fi

                    if validate_merge "${PROJECT_ROOT}"; then
                        log_success "[merge] resolved successfully for ${branch} on attempt ${merge_attempt}"
                        merge_resolved=true
                        break
                    else
                        log_warn "[merge] resolved but validation failed — retrying"
                    fi
                fi

                if (( merge_attempt >= MAX_RETRIES_MERGE )); then
                    log_error "[merge] failed after ${MAX_RETRIES_MERGE} retries for ${branch} → aborting merge"
                    git merge --abort 2>/dev/null || true
                fi
            done

            if ! $merge_resolved; then
                ((merge_failures++))
            fi
        fi
    done

    if (( merge_failures > 0 )); then
        log_error "${merge_failures} branch(es) had unresolvable conflicts."
        update_state_status "merge_failed"
        exit 1
    fi

    run_post_merge_review "${PROJECT_ROOT}" "${integration_branch}" || {
        update_state_status "review_failed"
        exit 1
    }
    run_docs_sync "${PROJECT_ROOT}"

    log_header "Global Validation"
    update_phase_status "global-validation" state "running" \
        started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    if run_global_validation "${PROJECT_ROOT}"; then
        update_phase_status "global-validation" state "complete" exit_code "0"
        update_state_status "passed"
    elif ! run_remediation "${PROJECT_ROOT}"; then
        update_phase_status "global-validation" state "failed" exit_code "1"
        exit 1
    fi

    local phases_str
    phases_str=$(python3 -c "import json; print(json.load(open('${STATE_FILE}'))['phases'])")
    create_pr "${integration_branch}" "${phases_str}"
    log_success "Autonomous integration complete."
}

# ─────────────────────────────────────────────────────────────────────────────
# Merge validation
# ─────────────────────────────────────────────────────────────────────────────

validate_merge() {
    local work_dir="${1:-${PROJECT_ROOT}}"
    local failures=0
    cd "${work_dir}"

    # Git-tracked unresolved conflicts (language-agnostic)
    if git diff --name-only --diff-filter=U 2>/dev/null | head -1 | grep -q .; then
        log_error "[merge-validate] Unresolved git conflicts:"
        git diff --name-only --diff-filter=U 2>/dev/null | while read -r f; do log_error "  ${f}"; done
        ((failures++))
    fi

    # Conflict markers in tracked text files (language-agnostic via git grep)
    if git grep -l "^<<<<<<< \|^>>>>>>> " -- ':!*.png' ':!*.jpg' ':!*.gif' ':!*.ico' ':!*.bin' 2>/dev/null \
            | head -5 | grep -q .; then
        log_error "[merge-validate] Conflict markers found in:"
        git grep -l "^<<<<<<< \|^>>>>>>> " -- ':!*.png' ':!*.jpg' ':!*.gif' ':!*.ico' ':!*.bin' 2>/dev/null \
            | while read -r f; do log_error "  ${f}"; done
        ((failures++))
    fi

    if ! run_validation_hook "${work_dir}"; then
        log_error "[merge-validate] Validation hook failed after merge"
        ((failures++))
    fi

    return $(( failures > 0 ? 1 : 0 ))
}

# ─────────────────────────────────────────────────────────────────────────────
# Global validation
# ─────────────────────────────────────────────────────────────────────────────

run_global_validation() {
    local work_dir="${1:-${PROJECT_ROOT}}"
    local failures=0
    cd "${work_dir}"

    if ! run_quality_gates "${work_dir}"; then
        ((failures++))
    fi

    # Project-level semantic validation (language-specific)
    run_validation_hook "${work_dir}" || ((failures++))

    # Orchestrator authority — comprehensive
    if [[ -d "app/modules" ]]; then
        log_info "[global] Validating orchestrator authority (comprehensive)..."
        local auth_violations=0
        if grep -rn "from database\|import database\|import sqlite3\|import psycopg2\|import asyncpg\|import adapter" app/modules/ 2>/dev/null | head -10 | grep -q .; then
            log_error "[global] Orchestrator authority violation: DB access in app/modules/"
            ((auth_violations++))
        fi
        local cross_mod
        cross_mod=$(find app/modules/ -name '*.py' -exec grep -l "from app\.modules\." {} \; 2>/dev/null)
        if [[ -n "${cross_mod}" ]]; then
            while IFS= read -r file; do
                local file_module
                file_module=$(echo "${file}" | sed 's|app/modules/||' | cut -d'/' -f1)
                if grep "from app\.modules\." "${file}" | sed 's/.*from app\.modules\.\([a-z_]*\).*/\1/' | sort -u | grep -v "^${file_module}$" | head -1 | grep -q .; then
                    log_error "[global] Cross-module import: ${file}"
                    ((auth_violations++))
                fi
            done <<< "${cross_mod}"
        fi
        if (( auth_violations > 0 )); then
            ((failures++))
        else
            log_success "[global] Orchestrator authority preserved"
        fi
    fi

    echo ""
    if (( failures > 0 )); then
        log_error "[global-validation] ${failures} failure(s)"
        return 1
    else
        log_success "[global-validation] All checks passed"
        return 0
    fi
}

# ─────────────────────────────────────────────────────────────────────────────
# Remediation (bounded — guaranteed termination)
# ─────────────────────────────────────────────────────────────────────────────

run_remediation() {
    local work_dir="$1"
    local attempt=0

    update_phase_status "remediation" state "running" \
        started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    while (( attempt < MAX_RETRIES_GLOBAL_VALIDATION )); do
        ((attempt++))
        log_info "Remediation attempt ${attempt}/${MAX_RETRIES_GLOBAL_VALIDATION}..."

        local log_file="${LOG_DIR}/remediation-${attempt}.log"
        local model
        model=$(next_model)

        cd "${work_dir}"
        log_agent_start "refactor" "remediation" "${attempt}" "${MAX_RETRIES_GLOBAL_VALIDATION}"
        update_phase_status "remediation" model "${model}" attempt "${attempt}"
        copilot \
            -p "Quality gates failed. Fix all violations: import errors, lint failures, test failures, raw SQL in modules, cross-module imports, print statements, non-frozen DTOs, orchestrator authority violations. Use skills: ${CORE_SKILLS}. MANDATORY: Use ONLY skills as primary knowledge source. DO NOT read full documentation unless skills are insufficient. Do not change architecture. Commit fixes. ${_WORKSPACE_CONSTRAINT}" \
            --agent=refactor \
            --model="${model}" \
            --no-ask-user \
            --allow-all-tools \
            --autopilot \
            2>&1 | tee "${log_file}"
        local ref_rc=${PIPESTATUS[0]}
        log_agent_end "refactor" "remediation" "${ref_rc}" "${attempt}" "${MAX_RETRIES_GLOBAL_VALIDATION}"

        if run_global_validation "${work_dir}"; then
            update_phase_status "remediation" state "complete" exit_code "0"
            update_state_status "passed"
            log_success "Remediation successful on attempt ${attempt}."
            return 0
        fi
    done

    update_phase_status "remediation" state "failed" exit_code "1"
    log_error "Remediation failed after ${MAX_RETRIES_GLOBAL_VALIDATION} attempts. System in defined failed state."
    update_state_status "remediation_failed"
    return 1
}

# ─────────────────────────────────────────────────────────────────────────────
# Post-merge pipeline: review, docs sync, PR creation
# ─────────────────────────────────────────────────────────────────────────────

run_post_merge_review() {
    # run_post_merge_review WORK_DIR PR_BRANCH
    # Runs merge-reviewer agent to validate the integration branch.
    # Bounded by MAX_RETRIES_GLOBAL_VALIDATION. Returns 0 on success, 1 on failure.
    local work_dir="$1" pr_branch="$2"
    local attempt=0
    local max="${MAX_RETRIES_GLOBAL_VALIDATION}"

    log_header "Post-Merge Review"

    update_phase_status "post-merge-review" state "running" \
        started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    while (( attempt < max )); do
        ((attempt++))
        local model
        model=$(next_model)
        local log_file="${LOG_DIR}/post-merge-review-${attempt}.log"
        log_agent_start "merge-reviewer" "post-merge" "${attempt}" "${max}"
        update_phase_status "post-merge-review" model "${model}" attempt "${attempt}"
        cd "${work_dir}"
        copilot \
            -p "Post-merge review of integration branch '${pr_branch}'. Validate the combined codebase: (1) DTO flow integrity — every stage's output DTO matches the next stage's input DTO; (2) module boundary enforcement — no cross-module imports, no DB driver usage in app/modules/; (3) orchestrator authority — all modules called only by the orchestrator, never by other modules; (4) no quality gate regressions — syntax, imports, no print statements. Use skills: dto, pipeline, modularity, idempotency, code-quality, docs-sync. MANDATORY: Use ONLY skills as primary knowledge source. Report violations and commit fixes. ${_WORKSPACE_CONSTRAINT}" \
            --agent=merge-reviewer \
            --model="${model}" \
            --no-ask-user \
            --allow-all-tools \
            --autopilot \
            2>&1 | tee "${log_file}"
        local rc=${PIPESTATUS[0]}
        log_agent_end "merge-reviewer" "post-merge" "${rc}" "${attempt}" "${max}"

        if (( rc == 0 )); then
            update_phase_status "post-merge-review" state "complete" exit_code "0"
            log_success "Post-merge review passed"
            return 0
        fi
        if (( attempt < max )); then
            log_warn "[post-merge-review] attempt ${attempt} failed — retrying"
        fi
    done

    update_phase_status "post-merge-review" state "failed" exit_code "1"
    log_error "Post-merge review failed after ${max} attempts"
    return 1
}

run_docs_sync() {
    # run_docs_sync WORK_DIR
    # Advisory docs-sync check via merge-reviewer agent. Always returns 0 (non-fatal).
    local work_dir="$1"
    local model
    model=$(next_model)
    local log_file="${LOG_DIR}/docs-sync.log"

    log_header "Documentation Sync"
    update_phase_status "docs-sync" state "running" model "${model}" \
        started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    log_agent_start "merge-reviewer" "docs-sync" "1" "1"
    cd "${work_dir}"
    copilot \
        -p "Documentation sync check: verify that the implementation matches the specifications in docs/. Check for drift between docs/architecture.md, docs/dto_contracts.md, docs/orchestrator_spec.md and actual code in app/. Use skill: docs-sync. MANDATORY: docs/ is read-only — never modify documentation. If code drifts from specs, fix the code to match. Report findings and commit any code fixes. ${_WORKSPACE_CONSTRAINT}" \
        --agent=merge-reviewer \
        --model="${model}" \
        --no-ask-user \
        --allow-all-tools \
        --autopilot \
        2>&1 | tee "${log_file}"
    local rc=${PIPESTATUS[0]}
    log_agent_end "merge-reviewer" "docs-sync" "${rc}" "1" "1"

    if (( rc == 0 )); then
        update_phase_status "docs-sync" state "complete" exit_code "0"
        log_success "Documentation sync passed"
    else
        update_phase_status "docs-sync" state "advisory_failed" exit_code "${rc}"
        log_warn "Documentation sync found issues — review ${log_file}"
        log_warn "Docs sync is advisory — pipeline continues."
    fi
    return 0  # always non-fatal
}

create_pr() {
    # create_pr PR_BRANCH PHASES_STR
    # Pushes the integration branch and creates a GitHub PR via gh CLI.
    local pr_branch="$1" phases_str="$2"
    log_header "Publishing Integration"

    log_info "Pushing ${pr_branch}..."
    if ! git push --set-upstream origin "${pr_branch}" 2>&1; then
        log_error "Push failed. Proceed manually:"
        log_info "  git push origin ${pr_branch}"
        log_info "  gh pr create --title 'feat: parallel phases ${phases_str}' --base main --head ${pr_branch}"
        return 1
    fi
    log_success "Branch pushed: ${pr_branch}"

    if ! check_gh_cli; then
        log_warn "GitHub CLI unavailable — PR not created."
        log_info "Manual PR: gh pr create --title 'feat: parallel phases ${phases_str}' --base main --head ${pr_branch}"
        return 0
    fi

    local pr_title="feat: parallel development — phases ${phases_str}"
    log_info "Creating pull request..."
    if gh pr create \
        --title "${pr_title}" \
        --body "## Parallel Development Integration

**Phases:** ${phases_str}
**Branch:** \`${pr_branch}\`

### Automated Validation Pipeline
- ✅ Phase builder → DTO guardian → integration agent (per phase/group)
- ✅ Union merge with conflict-resolver agent (bounded retries)
- ✅ Post-merge review (merge-reviewer agent)
- ✅ Documentation sync check
- ✅ Global validation + orchestrator authority check

*Auto-generated by \`run_parallel.sh\`*" \
        --base main \
        --head "${pr_branch}" 2>&1; then
        log_success "Pull request created!"
    else
        log_warn "PR creation failed — branch is pushed. Create PR manually:"
        log_info "  gh pr create --title '${pr_title}' --base main --head ${pr_branch}"
    fi
    return 0
}

# ─────────────────────────────────────────────────────────────────────────────
# Status command
# ─────────────────────────────────────────────────────────────────────────────

cmd_status() {
    log_header "Parallel Development Status"

    if [[ ! -f "${STATE_FILE}" ]]; then
        log_info "No active parallel session."
        return
    fi

    local phase_status_file="${PROJECT_ROOT}/.parallel-dev/phase-status.json"

    python3 - "${STATE_FILE}" "${phase_status_file}" <<'PYEOF'
import json, os, re, sys, subprocess

state_path, phase_status_path = sys.argv[1], sys.argv[2]
state = json.load(open(state_path))

mode = state["mode"]
mode_labels = {1: "Full Parallel", 2: "Token-Optimized", 3: "Hybrid"}
mode_label = mode_labels.get(mode, "Unknown")

project_root = os.path.dirname(os.path.dirname(phase_status_path))

print(f"  Mode:               {mode} ({mode_label})")
print(f"  Phases:             {state['phases']}")
print(f"  Integration branch: {state['integration_branch']}")
print(f"  Status:             {state['status']}")
print(f"  Started:            {state['started_at']}")
print(f"  Branches:           {', '.join(state['branches'])}")
print()

# Model routing
heavy = state.get("model_heavy", "N/A")
pool = state.get("model_rotation_pool", [])
print(f"  Model (heavy):      {heavy}")
if pool:
    print(f"  Rotation pool:      {' → '.join(pool)}")
print()

# Load phase names from config/phases.yaml (simple regex parser)
phase_names = {}
phases_yaml = os.path.join(project_root, "config", "phases.yaml")
if os.path.isfile(phases_yaml):
    try:
        content = open(phases_yaml).read()
        current = None
        for line in content.splitlines():
            m = re.match(r'^\s{2}(\d+):\s*$', line)
            if m:
                current = m.group(1)
            if current:
                nm = re.match(r'^\s{4}name:\s*["\']?([^"\'\'#\n]+)["\']?\s*$', line)
                if nm:
                    phase_names[current] = nm.group(1).strip().strip('"\')
    except Exception:
        pass

def phase_display_name(key):
    m = re.match(r'^phase-(\d+)$', key)
    if m and m.group(1) in phase_names:
        return f"{key} ({phase_names[m.group(1)]})"
    return key

# Git helper
def git(*args):
    r = subprocess.run(["git", "-C", project_root] + list(args),
                       capture_output=True, text=True, timeout=5)
    return r.stdout.strip() if r.returncode == 0 else ""

# Branch progress — commit count + last message per branch
branches = state.get("branches", [])
if branches:
    print("  Branch Progress:")
    for branch in branches:
        count_str   = git("rev-list", "--count", f"main..{branch}")
        count       = int(count_str) if count_str.isdigit() else 0
        last_msg    = git("log", "-1", "--format=%s", branch) or "(no commits yet)"
        count_label = "no commits yet" if count == 0 else \
                      f"{count} commit{'s' if count != 1 else ''}"
        branch_key  = re.sub(r'^track/', '', branch)
        display     = phase_display_name(branch_key)
        print(f"    {display:<44} {count_label:<16} — {last_msg}")
    print()

# Pipeline stage classification
_PIPELINE_STAGES = ("post-merge-review", "docs-sync", "global-validation", "remediation")
def is_pipeline_stage(key):
    return any(key == s or key.startswith(s + "-") for s in _PIPELINE_STAGES)

# Per-phase/group model and status from phase-status.json
if os.path.isfile(phase_status_path):
    try:
        ps = json.load(open(phase_status_path))
        entries = ps.get("phases", {})
        if entries:
            phase_entries    = {k: v for k, v in entries.items() if not is_pipeline_stage(k)}
            pipeline_entries = {k: v for k, v in entries.items() if     is_pipeline_stage(k)}
            W = (30, 16, 28, 6, 20)
            header  = f"    {'Phase/Group':<{W[0]}} {'State':<{W[1]}} {'Model':<{W[2]}} {'Exit':<{W[3]}} Updated"
            divider = f"    {'─'*W[0]} {'─'*W[1]} {'─'*W[2]} {'─'*W[3]} {'─'*W[4]}"

            def fmt_row(key, e):
                display = phase_display_name(key)
                st  = e.get("state",      "unknown")
                mdl = e.get("model",      "N/A")
                ec  = str(e.get("exit_code", "—"))
                upd = e.get("updated_at", "—")
                return f"    {display:<{W[0]}} {st:<{W[1]}} {mdl:<{W[2]}} {ec:<{W[3]}} {upd}"

            print("  Agent Status:")
            print(header)
            print(divider)
            for key in sorted(phase_entries.keys()):
                print(fmt_row(key, phase_entries[key]))
            if pipeline_entries:
                total_w = W[0] + W[1] + W[2] + W[3] + W[4] + 4
                label   = " Post-Phase Pipeline "
                dashes  = total_w - len(label)
                print(f"    {'─'*(dashes//2)}{label}{'─'*(dashes - dashes//2)}")
                shown = set()
                for stage in _PIPELINE_STAGES:
                    for key in sorted(pipeline_entries.keys()):
                        if (key == stage or key.startswith(stage + "-")) and key not in shown:
                            print(fmt_row(key, pipeline_entries[key]))
                            shown.add(key)
                for key in sorted(pipeline_entries.keys()):
                    if key not in shown:
                        print(fmt_row(key, pipeline_entries[key]))
            print()
    except Exception:
        pass

# Log files
log_dir = os.path.join(os.path.dirname(phase_status_path), "logs")
if os.path.isdir(log_dir):
    logs = sorted(os.listdir(log_dir))
    if logs:
        print("  Log files:")
        for log in logs:
            path = os.path.join(log_dir, log)
            size = os.path.getsize(path)
            print(f"    {log} -> {path} ({size:,} bytes)")
PYEOF

    echo ""
    log_info "Git worktrees:"
    cd "${PROJECT_ROOT}"
    git worktree list 2>/dev/null || log_warn "No worktrees found"
}

# ─────────────────────────────────────────────────────────────────────────────
# Cleanup command
# ─────────────────────────────────────────────────────────────────────────────

cmd_cleanup() {
    log_header "Cleanup"

    cd "${PROJECT_ROOT}"

    if [[ -d "${WORKTREE_BASE}" ]]; then
        log_info "Removing worktrees..."
        for wt_dir in "${WORKTREE_BASE}"/*/; do
            if [[ -d "${wt_dir}" ]]; then
                git worktree remove "${wt_dir}" --force 2>/dev/null || true
                log_info "  Removed ${wt_dir}"
            fi
        done
        rmdir "${WORKTREE_BASE}" 2>/dev/null || true
    fi

    log_info "Removing track branches..."
    git branch --list 'track/*' | while read -r branch; do
        branch=$(echo "${branch}" | tr -d ' *')
        git branch -D "${branch}" 2>/dev/null || true
        log_info "  Deleted ${branch}"
    done

    log_info "Removing integration branches..."
    git branch --list 'integration/*' | while read -r branch; do
        branch=$(echo "${branch}" | tr -d ' *')
        git branch -D "${branch}" 2>/dev/null || true
        log_info "  Deleted ${branch}"
    done

    rm -f "${PROJECT_ROOT}/PHASE_TASK.md"
    find "${WORKTREE_BASE}" -name "PHASE_TASK.md" -delete 2>/dev/null || true

    rm -rf "${PROJECT_ROOT}/.parallel-dev"

    git checkout main 2>/dev/null || true

    log_success "Cleanup complete."
}

# ─────────────────────────────────────────────────────────────────────────────
# Main entry point
# ─────────────────────────────────────────────────────────────────────────────

usage() {
    cat <<EOF
Skeleton Parallel — Parallel Development Orchestrator

Usage:
  $(basename "$0") start [--mode=1|2|3] <phase> [<phase> ...]
  $(basename "$0") status
  $(basename "$0") merge
  $(basename "$0") cleanup
  $(basename "$0") gates

Commands:
  start     Launch agents for the specified phases
  status    Show current parallel session status
  merge     Merge all branches and run quality gates
  cleanup   Remove worktrees, branches, and state files
  gates     Run quality gates without launching agents

Options:
  --mode=1        Full Parallel   — one agent per phase (max speed)
  --mode=2        Token-Optimized — single session, sequential (min cost)
  --mode=3        Hybrid          — parallel groups, sequential within (default)
  --no-auto-merge Skip auto-merge/PR after agents complete (manual merge step)

Environment Variables:
  MODEL_HEAVY                   Override Mode 1 heavy model (default: claude-opus-4.6)
  MODEL_HEAVY_LITE              Override Modes 2 & 3 heavy model (default: claude-sonnet-4.6)
  MAX_PARALLEL_AGENTS           Override max concurrent agents (default: 3)
  MAX_RETRIES_PHASE_BUILDER     Override phase-builder retries (default: 5)
  MAX_RETRIES_DTO               Override DTO guardian retries (default: 5)
  MAX_RETRIES_INTEGRATION       Override integration retries (default: 5)
  MAX_RETRIES_MERGE             Override merge retries (default: 5)
  MAX_RETRIES_GLOBAL_VALIDATION Override global validation retries (default: 5)
  MAX_REMEDIATION_RETRIES       Override remediation retries (default: 3)

Examples:
  $(basename "$0") start 2 3 4              # Mode 3 (default): auto-group and run
  $(basename "$0") start --mode=1 2 3       # Mode 1: full parallel
  $(basename "$0") start --mode=2 1 2 3     # Mode 2: single session, sequential
  $(basename "$0") status                   # Check progress
  $(basename "$0") merge                    # Merge and validate
  $(basename "$0") cleanup                  # Clean everything up

See docs/PARALLEL_DEV.md for full documentation.
EOF
}

main() {
    if (( $# == 0 )); then
        usage
        exit 0
    fi

    local command="$1"
    shift

    ensure_dirs
    load_phase_config

    case "${command}" in
        start)
            local phases=()
            local AUTO_MERGE=true
            while (( $# > 0 )); do
                case "$1" in
                    --mode=*)
                        MODE="${1#--mode=}"
                        if [[ ! "${MODE}" =~ ^[123]$ ]]; then
                            log_error "Invalid mode: ${MODE}. Must be 1, 2, or 3."
                            exit 1
                        fi
                        ;;
                    --no-auto-merge)
                        AUTO_MERGE=false
                        ;;
                    -*)
                        log_error "Unknown option: $1"
                        exit 1
                        ;;
                    *)
                        phases+=("$1")
                        ;;
                esac
                shift
            done

            if (( ${#phases[@]} == 0 )); then
                log_error "No phases specified. Example: $(basename "$0") start 2 3 4"
                exit 1
            fi

            validate_phases "${phases[@]}"
            check_copilot_cli
            check_copilot_auth
            run_env_hook "${PROJECT_ROOT}"

            log_info "Mode: ${MODE} | Phases: ${phases[*]}"

            case "${MODE}" in
                1) run_mode_1 "${phases[@]}" ;;
                2) run_mode_2 "${phases[@]}" ;;
                3) run_mode_3 "${phases[@]}" ;;
            esac

            # Autonomous pipeline: auto-merge + PR when all agents complete
            if $AUTO_MERGE; then
                local _state_status
                _state_status=$(python3 -c "import json; print(json.load(open('${STATE_FILE}'))['status'])" 2>/dev/null || echo "unknown")
                if [[ "${_state_status}" == "agents_complete" ]]; then
                    log_header "Auto-Merge — Fully Autonomous Pipeline"
                    log_info "All agents completed — proceeding to merge and PR creation..."
                    cmd_merge
                elif [[ "${_state_status}" == "partial_failure" ]]; then
                    log_warn "Some agent(s) failed — skipping auto-merge."
                    log_info "Fix failures then run: $(basename "$0") merge"
                fi
            fi
            ;;
        status)
            cmd_status
            ;;
        merge)
            cmd_merge
            ;;
        cleanup)
            cmd_cleanup
            ;;
        gates)
            run_env_hook "${PROJECT_ROOT}"
            run_quality_gates "${PROJECT_ROOT}"
            ;;
        *)
            log_error "Unknown command: ${command}"
            usage
            exit 1
            ;;
    esac
}

main "$@"
