#!/usr/bin/env bash
# validate_pipeline_proof.sh — PIPELINE_PROOF acceptance harness (PLAN Task 18)
#
# Reads gate-review evidence produced by scripts/gate_review_collect.sh and
# answers whether the session met the minimum bar to advance toward shadow trading.
#
# Usage:
#   ./scripts/validate_pipeline_proof.sh [EVIDENCE_JSON]
#
#   EVIDENCE_JSON  Path to gate_evidence_<TIMESTAMP>.json.
#                  Default: newest output/logs/gate_evidence_*.json by mtime.
#
# PASS (exit 0) iff all of:
#   traces_completed >= 1        (distinct L10 learning_record_emitted trace_ids)
#   duplicate_execution == 0     (operational_evidence.duplicate_execution)
#   wsol_token_address_emitted == 0
#
# Output:
#   PRODUCTION_DECISION: SHADOW_READY | NOT_READY
#   On FAIL: single-line reason on stderr, exit 1
#
# Read-only — never modifies source code or the database.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="$REPO_ROOT/output/logs"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "NOT_READY: '$1' not found — install it first" >&2
    echo "PRODUCTION_DECISION: NOT_READY"
    exit 1
  }
}

fail() {
  echo "NOT_READY: $*" >&2
  echo "PRODUCTION_DECISION: NOT_READY"
  exit 1
}

require_cmd jq

EVIDENCE="${1:-}"
if [[ -z "$EVIDENCE" ]]; then
  EVIDENCE=$(ls -t "$OUTPUT_DIR"/gate_evidence_*.json 2>/dev/null | head -1 || true)
  [[ -n "$EVIDENCE" ]] || fail "no gate_evidence_*.json under $OUTPUT_DIR — run scripts/gate_review_collect.sh first"
fi

[[ -f "$EVIDENCE" ]] || fail "evidence file not found: $EVIDENCE"

TRACES=$(jq -r '.operational_evidence.traces_completed // 0' "$EVIDENCE")
DUP=$(jq -r '.operational_evidence.duplicate_execution // 0' "$EVIDENCE")
WSOL=$(jq -r '.throughput_metrics.wsol_token_address_emitted // 0' "$EVIDENCE")

for _var in TRACES DUP WSOL; do
  _val="${!_var}"
  if ! [[ "$_val" =~ ^[0-9]+$ ]]; then
    fail "invalid numeric field in evidence ($_var=$_val)"
  fi
done

if [[ "$TRACES" -lt 1 ]]; then
  fail "traces_completed=0"
fi
if [[ "$DUP" -gt 0 ]]; then
  fail "duplicate_execution=$DUP"
fi
if [[ "$WSOL" -gt 0 ]]; then
  fail "wsol_token_address_emitted=$WSOL"
fi

echo "PRODUCTION_DECISION: SHADOW_READY"
echo "PASS evidence=$EVIDENCE traces_completed=$TRACES duplicate_execution=$DUP wsol_token_address_emitted=$WSOL"
exit 0
