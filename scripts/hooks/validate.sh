#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# Hook: validate — syntax/compile check after agent implementation
# Runs `go build ./...` to confirm all packages compile cleanly.
# Exits 0 on success, non-zero on failure (triggers retry in run_parallel.sh).
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${HOOK_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

echo "[validate] Running go build ./..."
go build ./...
echo "[validate] Build: OK"

echo "[validate] Running go vet ./..."
go vet ./...
echo "[validate] Vet: OK"
