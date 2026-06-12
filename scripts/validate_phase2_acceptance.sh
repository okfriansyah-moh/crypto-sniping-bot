#!/usr/bin/env bash
# validate_phase2_acceptance.sh — Phase 2 §1.1 full acceptance gate (PLAN Task 19)
#
# Reads gate_evidence_*.json from gate_review_collect.sh and checks all six
# Phase 2 success criteria before declaring PIPELINE_PROOF hardening complete.
#
# Usage:
#   ./scripts/validate_phase2_acceptance.sh [EVIDENCE_JSON]
#
# PASS (exit 0) iff:
#   wsol_token_address_emitted == 0
#   ingestion_valid_token_ratio >= 0.80
#   market_probes_backlog_ratio <= 0.05
#   dq_pass_or_risky_pass >= 1
#   traces_completed >= 1
#   shadow_observer_failed == 0
#
# Also runs validate_pipeline_proof.sh (subset) when PHASE2_ACCEPTANCE_ONLY is unset.
#
# Read-only — never modifies source code or the database.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="$REPO_ROOT/output/logs"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

fail() {
  echo "PHASE2_ACCEPTANCE: FAIL — $*" >&2
  echo "PRODUCTION_DECISION: NOT_READY"
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "'$1' not found — install it first"
}

require_cmd jq

EVIDENCE="${1:-}"
if [[ -z "$EVIDENCE" ]]; then
  EVIDENCE=$(ls -t "$OUTPUT_DIR"/gate_evidence_*.json 2>/dev/null | head -1 || true)
  [[ -n "$EVIDENCE" ]] || fail "no gate_evidence_*.json under $OUTPUT_DIR — run gate_review_collect.sh first"
fi

[[ -f "$EVIDENCE" ]] || fail "evidence file not found: $EVIDENCE"

WSOL=$(jq -r '.throughput_metrics.wsol_token_address_emitted // 0' "$EVIDENCE")
VALID_RATIO=$(jq -r '.throughput_metrics.ingestion_valid_token_ratio // "0"' "$EVIDENCE")
BACKLOG=$(jq -r '.throughput_metrics.market_probes_backlog_ratio // "1"' "$EVIDENCE")
DQ_PASS=$(jq -r '.throughput_metrics.dq_pass_or_risky_pass // 0' "$EVIDENCE")
SHADOW_FAIL=$(jq -r '.throughput_metrics.shadow_observer_failed // 0' "$EVIDENCE")
TRACES=$(jq -r '.operational_evidence.traces_completed // 0' "$EVIDENCE")

for _var in WSOL DQ_PASS SHADOW_FAIL TRACES; do
  _val="${!_var}"
  if ! [[ "$_val" =~ ^[0-9]+$ ]]; then
    fail "invalid integer field $_var=$_val"
  fi
done

if [[ "$WSOL" -gt 0 ]]; then
  fail "wsol_token_address_emitted=$WSOL (want 0)"
fi

if ! awk -v r="$VALID_RATIO" 'BEGIN{exit !(r+0 >= 0.80)}'; then
  fail "ingestion_valid_token_ratio=$VALID_RATIO (want >= 0.80)"
fi

if ! awk -v b="$BACKLOG" 'BEGIN{exit !(b+0 <= 0.05)}'; then
  fail "market_probes_backlog_ratio=$BACKLOG (want <= 0.05)"
fi

if [[ "$DQ_PASS" -lt 1 ]]; then
  fail "dq_pass_or_risky_pass=$DQ_PASS (want >= 1)"
fi

if [[ "$TRACES" -lt 1 ]]; then
  fail "traces_completed=0 (want >= 1)"
fi

if [[ "$SHADOW_FAIL" -gt 0 ]]; then
  fail "shadow_observer_failed=$SHADOW_FAIL (want 0)"
fi

# Delegate minimal pipeline-proof trio (duplicate_execution, etc.)
bash "$SCRIPT_DIR/validate_pipeline_proof.sh" "$EVIDENCE" >/dev/null

echo "PHASE2_ACCEPTANCE: PASS"
echo "PRODUCTION_DECISION: SHADOW_READY"
echo "evidence=$EVIDENCE wsol=$WSOL valid_ratio=$VALID_RATIO backlog=$BACKLOG dq_pass=$DQ_PASS traces=$TRACES shadow_failed=$SHADOW_FAIL"
exit 0
