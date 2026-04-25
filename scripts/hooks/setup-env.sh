#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# Hook: setup-env — prepare Go project environment
# Called once per worktree/branch before the agent pipeline starts.
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${HOOK_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

echo "[setup-env] Downloading Go module dependencies..."
go mod download

echo "[setup-env] Verifying module graph..."
go mod verify

echo "[setup-env] Done."
