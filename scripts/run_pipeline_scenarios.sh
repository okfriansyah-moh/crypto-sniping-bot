#!/usr/bin/env bash
# run_pipeline_scenarios.sh — Battle-test entrypoint (offline, all scenarios).
#
# Usage:
#   ./scripts/run_pipeline_scenarios.sh
#
# Delegates to validate_pipeline_scenarios.sh. No Docker or database required.

set -euo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
exec bash "$REPO_ROOT/scripts/validate_pipeline_scenarios.sh" "$@"
