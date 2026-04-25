#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# Hook: activate-env — activate runtime environment before agent execution
# Go projects do not require virtualenv activation; this hook ensures the
# correct Go toolchain is on PATH and the module cache is accessible.
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${HOOK_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

# Ensure go is available
if ! command -v go &>/dev/null; then
    echo "[activate-env] ERROR: 'go' not found on PATH. Install Go and retry."
    exit 1
fi

echo "[activate-env] Go $(go version)"
echo "[activate-env] GOPATH=${GOPATH:-$(go env GOPATH)}"
echo "[activate-env] Module: $(go list -m 2>/dev/null || echo '(unknown)')"
echo "[activate-env] Ready."
