#!/usr/bin/env bash
# validate_pipeline_scenarios.sh — Run all battle-test scenario fixtures offline.
#
# For each entry in tests/fixtures/scenarios/manifest.json:
#   1. gate_review_collect.sh --analyze <fixture.log>
#   2. Assert expect_evidence / expect_log / pipeline_proof_pass from manifest
#
# Usage:
#   ./scripts/validate_pipeline_scenarios.sh
#
# Exit 0 when all scenarios pass. Read-only — no DB, no Docker.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST="$REPO_ROOT/tests/fixtures/scenarios/manifest.json"
SCENARIO_DIR="$REPO_ROOT/tests/fixtures/scenarios"
GATE_SCRIPT="$REPO_ROOT/scripts/gate_review_collect.sh"
PROOF_SCRIPT="$REPO_ROOT/scripts/validate_pipeline_proof.sh"
OUTPUT_DIR="$REPO_ROOT/output/logs"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[battle-test] FATAL: '$1' not found" >&2
    exit 1
  }
}

require_cmd jq
require_cmd bash

[[ -f "$MANIFEST" ]] || { echo "[battle-test] FATAL: missing $MANIFEST" >&2; exit 1; }

log_count() {
  local log="$1" filter="$2"
  jq -r "$filter" "$log" 2>/dev/null | grep -c . || true
}

newest_evidence() {
  local newest="" mtime=0
  local e
  for e in "$OUTPUT_DIR"/gate_evidence_*.json; do
    [[ -f "$e" ]] || continue
    local mt
    mt=$(stat -f '%m' "$e" 2>/dev/null || stat -c '%Y' "$e" 2>/dev/null || echo 0)
    if [[ "$mt" -ge "$mtime" ]]; then
      mtime=$mt
      newest=$e
    fi
  done
  echo "$newest"
}

check_evidence_field() {
  local evidence="$1" jq_path="$2" op="$3" expected="$4"
  local actual
  actual=$(jq -r "$jq_path // 0" "$evidence")
  case "$op" in
    eq)   [[ "$actual" == "$expected" ]] || return 1 ;;
    min)  [[ "${actual:-0}" -ge "$expected" ]] || return 1 ;;
    max)  [[ "${actual:-0}" -le "$expected" ]] || return 1 ;;
    *) return 1 ;;
  esac
}

FAILED=0
PASSED=0
TOTAL=0

while IFS= read -r scenario; do
  TOTAL=$(( TOTAL + 1 ))
  id=$(echo "$scenario" | jq -r '.id')
  file=$(echo "$scenario" | jq -r '.file')
  log_path="$SCENARIO_DIR/$file"
  echo "[battle-test] scenario $id"

  [[ -f "$log_path" ]] || {
    echo "  FAIL: missing fixture $file" >&2
    FAILED=$(( FAILED + 1 ))
    continue
  }

  if ! bash "$GATE_SCRIPT" --analyze "$log_path" PIPELINE_PROOF >/dev/null 2>&1; then
    echo "  FAIL: gate_review_collect --analyze failed" >&2
    FAILED=$(( FAILED + 1 ))
    continue
  fi

  evidence=$(newest_evidence)
  [[ -n "$evidence" && -f "$evidence" ]] || {
    echo "  FAIL: no gate evidence written" >&2
    FAILED=$(( FAILED + 1 ))
    continue
  }

  ok=true

  # expect_evidence block (manifest uses *_min / *_max suffix keys)
  ev=$(echo "$scenario" | jq -r '.expect_evidence // {}')
  if [[ "$ev" != "{}" && "$ev" != "null" ]]; then
  for field in traces_completed learning_records dq_pass_or_risky_pass wsol_token_address_emitted; do
    min_key="${field/_or_risky_pass/}_min"
    [[ "$field" == "dq_pass_or_risky_pass" ]] && min_key="dq_pass_min"
    max_key="${field}_max"
    [[ "$field" == "dq_pass_or_risky_pass" ]] && max_key="dq_pass_max"
    [[ "$field" == "wsol_token_address_emitted" ]] && max_key="wsol_max"

    vmin=$(echo "$scenario" | jq -r ".expect_evidence.${min_key} // empty")
    vmax=$(echo "$scenario" | jq -r ".expect_evidence.${max_key} // empty")
    jq_path=".operational_evidence.${field}"
    [[ "$field" == "dq_pass_or_risky_pass" ]] && jq_path='.throughput_metrics.dq_pass_or_risky_pass'
    [[ "$field" == "wsol_token_address_emitted" ]] && jq_path='.throughput_metrics.wsol_token_address_emitted'

    actual=$(jq -r "$jq_path // 0" "$evidence")
    [[ -z "$vmin" || "$vmin" == "null" ]] || { [[ "$actual" -ge "$vmin" ]] || ok=false; }
    [[ -z "$vmax" || "$vmax" == "null" ]] || { [[ "$actual" -le "$vmax" ]] || ok=false; }
  done
  tv=$(echo "$scenario" | jq -r '.expect_evidence.throughput_verdict // empty')
  if [[ -n "$tv" && "$tv" != "null" ]]; then
    actual=$(jq -r '.throughput_metrics.throughput_verdict' "$evidence")
    [[ "$actual" == "$tv" ]] || ok=false
  fi
  fi

  # expect_log block — jq filters on raw NDJSON log
  el=$(echo "$scenario" | jq -r '.expect_log // {}')
  if [[ "$el" != "{}" && "$el" != "null" ]]; then
    dq_dec=$(echo "$scenario" | jq -r '.expect_log.dq_decision // empty')
    if [[ -n "$dq_dec" && "$dq_dec" != "null" ]]; then
      c=$(jq -r --arg d "$dq_dec" 'select(.msg=="dq_decision" and .decision==$d) | .trace_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge 1 ]] || ok=false
    fi
    rr=$(echo "$scenario" | jq -r '.expect_log.reject_reason_contains // empty')
    if [[ -n "$rr" && "$rr" != "null" ]]; then
      c=$(jq -r --arg r "$rr" 'select(.msg=="dq_decision" and ((.reject_reasons // []) | index($r))) | .trace_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge 1 ]] || ok=false
    fi
    sl_min=$(echo "$scenario" | jq -r '.expect_log.serial_launcher_skip_min // empty')
    if [[ -n "$sl_min" && "$sl_min" != "null" ]]; then
      c=$(jq -r 'select(.msg=="dq_decision" and ((.flags // []) | index("serial_launcher_skipped"))) | .trace_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge "$sl_min" ]] || ok=false
    fi
    ef_min=$(echo "$scenario" | jq -r '.expect_log.edge_filtered_min // empty')
    if [[ -n "$ef_min" && "$ef_min" != "null" ]]; then
      c=$(jq -r 'select(.msg=="stage_completed" and .worker_group=="edge_worker" and .output_status=="filtered") | .trace_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge "$ef_min" ]] || ok=false
    fi
    vr_min=$(echo "$scenario" | jq -r '.expect_log.validation_rejected_min // empty')
    if [[ -n "$vr_min" && "$vr_min" != "null" ]]; then
      c=$(jq -r 'select(.msg=="stage_completed" and .worker_group=="validation_worker" and .output_status=="rejected") | .trace_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge "$vr_min" ]] || ok=false
    fi
    se_min=$(echo "$scenario" | jq -r '.expect_log.selection_emitted_min // empty')
    if [[ -n "$se_min" && "$se_min" != "null" ]]; then
      c=$(jq -r 'select(.msg=="stage_completed" and .worker_group=="selection_worker" and .output_status=="emitted") | .trace_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge "$se_min" ]] || ok=false
    fi
    sf_min=$(echo "$scenario" | jq -r '.expect_log.selection_filtered_min // empty')
    if [[ -n "$sf_min" && "$sf_min" != "null" ]]; then
      c=$(jq -r 'select(.msg=="stage_completed" and .worker_group=="selection_worker" and .output_status=="filtered") | .trace_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge "$sf_min" ]] || ok=false
    fi
    sh_min=$(echo "$scenario" | jq -r '.expect_log.shadow_execution_min // empty')
    if [[ -n "$sh_min" && "$sh_min" != "null" ]]; then
      c=$(jq -r 'select(.msg=="execution_submitted" and .shadow==true) | .trace_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge "$sh_min" ]] || ok=false
    fi
    po_min=$(echo "$scenario" | jq -r '.expect_log.position_opened_shadow_min // empty')
    if [[ -n "$po_min" && "$po_min" != "null" ]]; then
      c=$(jq -r 'select(.msg=="position_opened" and .shadow==true) | .trace_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge "$po_min" ]] || ok=false
    fi
    lr_min=$(echo "$scenario" | jq -r '.expect_log.learning_record_min // empty')
    if [[ -n "$lr_min" && "$lr_min" != "null" ]]; then
      c=$(jq -r 'select(.msg=="learning_record_emitted") | .trace_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge "$lr_min" ]] || ok=false
    fi
    er=$(echo "$scenario" | jq -r '.expect_log.exit_reason // empty')
    if [[ -n "$er" && "$er" != "null" ]]; then
      c=$(jq -r --arg r "$er" 'select(.msg=="position_closed" and .exit_reason==$r) | .trace_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge 1 ]] || ok=false
    fi
    sr_min=$(echo "$scenario" | jq -r '.expect_log.shadow_record_emitted_min // empty')
    if [[ -n "$sr_min" && "$sr_min" != "null" ]]; then
      c=$(jq -r 'select(.msg=="shadow_record_emitted") | .shadow_id' "$log_path" | wc -l | tr -d ' ')
      [[ "$c" -ge "$sr_min" ]] || ok=false
    fi
  fi

  pp=$(echo "$scenario" | jq -r '.pipeline_proof_pass // false')
  if [[ "$pp" == "true" ]]; then
    if ! bash "$PROOF_SCRIPT" "$evidence" >/dev/null 2>&1; then
      ok=false
    fi
  fi

  if [[ "$ok" == true ]]; then
    echo "  PASS"
    PASSED=$(( PASSED + 1 ))
  else
    echo "  FAIL (evidence: $evidence)" >&2
    FAILED=$(( FAILED + 1 ))
  fi
done < <(jq -c '.scenarios[]' "$MANIFEST")

echo ""
echo "BATTLE_TEST: $PASSED/$TOTAL scenarios passed"
if [[ "$FAILED" -gt 0 ]]; then
  echo "BATTLE_TEST_CERTIFICATION: NOT_READY"
  exit 1
fi
echo "BATTLE_TEST_CERTIFICATION: READY"
exit 0
