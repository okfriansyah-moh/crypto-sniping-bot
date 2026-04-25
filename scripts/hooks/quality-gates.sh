#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# Hook: quality-gates — full quality gate suite for the Go sniper project
# Called after every agent pipeline run. Exits 0 only when ALL gates pass.
# A non-zero exit triggers the remediation loop in run_parallel.sh.
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${HOOK_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

FAILURES=()

# ── Gate 1: Build ────────────────────────────────────────────────────────────
echo "[quality-gates] Gate 1/4: go build ./..."
if go build ./... 2>&1; then
    echo "[quality-gates] Build: PASS"
else
    echo "[quality-gates] Build: FAIL"
    FAILURES+=("build")
fi

# ── Gate 2: Vet ─────────────────────────────────────────────────────────────
echo "[quality-gates] Gate 2/4: go vet ./..."
if go vet ./... 2>&1; then
    echo "[quality-gates] Vet: PASS"
else
    echo "[quality-gates] Vet: FAIL"
    FAILURES+=("vet")
fi

# ── Gate 3: Tests ────────────────────────────────────────────────────────────
echo "[quality-gates] Gate 3/4: go test ./... -count=1 -timeout=120s"
if go test ./... -count=1 -timeout=120s 2>&1; then
    echo "[quality-gates] Tests: PASS"
else
    echo "[quality-gates] Tests: FAIL"
    FAILURES+=("tests")
fi

# ── Gate 4: No direct DB imports in modules ──────────────────────────────────
echo "[quality-gates] Gate 4/4: checking module boundary — no direct DB imports in internal/modules/..."
if grep -r --include="*.go" \
    -e '"database/sql"' \
    -e '"github.com/lib/pq"' \
    -e '"github.com/jackc/pgx' \
    internal/modules/ 2>/dev/null | grep -v "_test.go"; then
    echo "[quality-gates] Module boundary: FAIL — direct DB import found in internal/modules/"
    FAILURES+=("module_boundary")
else
    echo "[quality-gates] Module boundary: PASS"
fi

# ── Summary ──────────────────────────────────────────────────────────────────
if (( ${#FAILURES[@]} > 0 )); then
    echo ""
    echo "[quality-gates] FAILED gates: ${FAILURES[*]}"
    exit 1
fi

echo ""
echo "[quality-gates] All gates PASSED."
