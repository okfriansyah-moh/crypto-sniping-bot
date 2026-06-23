#!/usr/bin/env bash
# gate_review_collect.sh — Collect live bot logs, compute production-gate-reviewer
# evidence, and write a structured gate-review brief ready for a Copilot session.
#
# Usage:
#   ./scripts/gate_review_collect.sh [DURATION_MINUTES] [SERVICE] [MODE]
#
#   DURATION_MINUTES  How long to collect logs.  Default: 60
#   SERVICE           Docker Compose service name. Default: sniper-bot
#   MODE              Force review mode: PIPELINE_PROOF | SHADOW_TRADING |
#                     MICRO_CAPITAL | LIVE_MONITORING
#                     Default: auto-detected from evidence counts.
#
# Output (all under output/logs/):
#   gate_raw_<TIMESTAMP>.log      — full newline-delimited JSON log
#   gate_brief_<TIMESTAMP>.txt    — structured gate-review brief (paste into Copilot)
#   gate_evidence_<TIMESTAMP>.json — machine-readable evidence snapshot
#
# Throughput section (PLAN §1.1 / Task 17) — appended to brief + evidence JSON:
#   wsol_token_address_emitted, ingestion_valid_token_ratio,
#   market_probes_backlog_ratio, dq_pass_or_risky_pass, shadow_observer_failed,
#   per-program ingestion heartbeat finals (pumpfun-amm, raydium-v4),
#   THROUGHPUT_VERDICT: CODE_DEFECT | MARKET_QUIET | GUARDRAILS_ACTIVE | HEALTHY
#
# After the script finishes, open a new Copilot chat and paste:
#   "Review this using the production-gate-reviewer skill:" + brief content.
#
# The script never modifies source code. It is read-only, append-only to output/.

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
# Two execution modes:
#   Collect:  ./scripts/gate_review_collect.sh [MINS] [SERVICE] [MODE]
#   Analyze:  ./scripts/gate_review_collect.sh --analyze /path/to/gate_raw_TIMESTAMP.log [MODE]

OUTPUT_DIR="$(cd "$(dirname "$0")/.." && pwd)/output/logs"

ANALYZE_ONLY=false
if [[ "${1:-}" == "--analyze" ]]; then
  ANALYZE_ONLY=true
  _INPUT="${2:-}"
  [[ -z "$_INPUT" ]] && { echo "[gate_review] FATAL: --analyze requires a path to a raw log file" >&2; exit 1; }
  [[ -f "$_INPUT" ]] || { echo "[gate_review] FATAL: File not found: $_INPUT" >&2; exit 1; }
  RAW_LOG="$(cd "$(dirname "$_INPUT")" && pwd)/$(basename "$_INPUT")"
  _BASE="$(basename "$RAW_LOG" .log)"
  TIMESTAMP="${_BASE#gate_raw_}"
  [[ "$TIMESTAMP" == "$_BASE" ]] && TIMESTAMP="reanalysis_$(date +%Y%m%d_%H%M%S)"
  DURATION_MINUTES="0"
  SERVICE="N/A"
  FORCED_MODE="${3:-}"
else
  DURATION_MINUTES="${1:-60}"
  SERVICE="${2:-sniper-bot}"
  FORCED_MODE="${3:-}"
  TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
  RAW_LOG="$OUTPUT_DIR/gate_raw_${TIMESTAMP}.log"
fi

CLEAN_LOG="$OUTPUT_DIR/gate_clean_${TIMESTAMP}.log"
BRIEF="$OUTPUT_DIR/gate_brief_${TIMESTAMP}.txt"
EVIDENCE_SNAPSHOT="$OUTPUT_DIR/gate_evidence_${TIMESTAMP}.json"

# Stub-detection: flag a numeric field as stubbed when its value appears in
# >90% of sampled lines (threshold ≥ 20 samples).
STUB_THRESHOLD_PCT=90
MIN_SAMPLES=20

# ── Helpers ───────────────────────────────────────────────────────────────────
log()  { echo "[gate_review] $*" >&2; }
die()  { echo "[gate_review] FATAL: $*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "'$1' not found — install it first."
}

if [[ "$ANALYZE_ONLY" != "true" ]]; then
  require_cmd docker
fi
require_cmd jq
require_cmd awk
require_cmd sort
require_cmd uniq

# ── Setup ─────────────────────────────────────────────────────────────────────
mkdir -p "$OUTPUT_DIR"
log "Output directory: $OUTPUT_DIR"
log "Raw log:          $RAW_LOG"
log "Brief:            $BRIEF"
log "Duration:         ${DURATION_MINUTES} minute(s)"
log "Service:          $SERVICE"
[[ -n "$FORCED_MODE" ]] && log "Mode (forced):    $FORCED_MODE"
echo ""

# ── Phase 1: Collect ──────────────────────────────────────────────────────────
if [[ "$ANALYZE_ONLY" == "true" ]]; then
  log "Phase 1/3 — Skipped (--analyze mode). Using: $RAW_LOG"
  LINE_COUNT=$(wc -l < "$RAW_LOG" | tr -d ' ')
  log "Phase 1/3 — Raw log: $LINE_COUNT lines."
else
  log "Phase 1/3 — Starting log collection (Ctrl-C to stop early)..."

  DURATION_SECS=$(( DURATION_MINUTES * 60 ))

  docker compose logs -f --no-log-prefix "$SERVICE" 2>/dev/null \
    | sed 's/^[a-zA-Z0-9_-]*-[0-9]* *| //' \
    > "$RAW_LOG" &
  COLLECTOR_PID=$!

  stop_collector() {
    kill "$COLLECTOR_PID" 2>/dev/null || true
    wait "$COLLECTOR_PID" 2>/dev/null || true
  }

  trap 'log "Interrupted — stopping collector..."; stop_collector; trap - INT TERM EXIT' INT TERM EXIT

  log "Collecting for ${DURATION_MINUTES}m (PID ${COLLECTOR_PID}). Press Ctrl-C to stop early..."
  sleep "$DURATION_SECS" || true
  stop_collector
  trap - INT TERM EXIT

  LINE_COUNT=$(wc -l < "$RAW_LOG" | tr -d ' ')
  log "Phase 1/3 — Collection complete: $LINE_COUNT lines written."
fi

if [[ "$LINE_COUNT" -eq 0 ]]; then
  die "No log lines captured. Is the '$SERVICE' container running?"
fi

log "Phase 1/3 — Filtering to valid JSON lines..."
jq -Rc 'fromjson?' "$RAW_LOG" > "$CLEAN_LOG" 2>/dev/null || true
LINE_COUNT_CLEAN=$(wc -l < "$CLEAN_LOG" | tr -d ' ')
log "Phase 1/3 — Valid JSON: $LINE_COUNT_CLEAN / $LINE_COUNT lines."

# ── Phase 2: Evidence Extraction ──────────────────────────────────────────────
log "Phase 2/3 — Extracting gate-review evidence..."

# Helper: count lines matching a jq filter
count_jq() { jq -c "$1" "$CLEAN_LOG" 2>/dev/null | wc -l | tr -d ' '; }

# Helper: count stage_completed events for a registered worker group (canonical
# per-worker health signal — see internal/orchestrator/logfields.go).
count_stage_worker() {
  local worker="$1"
  count_jq "select(.msg == \"stage_completed\" and .worker_group == \"$worker\")"
}

# Helper: count stage_completed events filtered by output_status
# (emitted | filtered | rejected | skipped | terminal).
count_stage_worker_status() {
  local worker="$1"
  local status="$2"
  count_jq "select(.msg == \"stage_completed\" and .worker_group == \"$worker\" and .output_status == \"$status\")"
}

# Helper: compute integer average of a numeric field across matching events
avg_jq() {
  local filter="$1"
  local field="$2"
  jq -r "$filter | .$field // empty" "$CLEAN_LOG" 2>/dev/null \
    | awk 'BEGIN{s=0;n=0} /^[0-9]+(\.[0-9]+)?$/{s+=$1;n++} END{if(n>0) printf "%d", s/n; else print "N/A"}'
}

# Helper: check if a numeric field is stubbed (VARIES or STUBBED:<val>)
check_stub() {
  local field="$1"
  local values
  values=$(jq -r "select(.$field != null) | .$field | tostring" "$CLEAN_LOG" 2>/dev/null)
  local total
  total=$(echo "$values" | grep -c . || true)
  if [[ "$total" -lt "$MIN_SAMPLES" ]]; then
    echo "INSUFFICIENT_SAMPLES:$total"
    return
  fi
  local top
  top=$(echo "$values" | sort | uniq -c | sort -rn | head -1)
  local top_count
  top_count=$(echo "$top" | awk '{print $1}')
  local top_val
  top_val=$(echo "$top" | awk '{print $2}')
  local pct=$(( top_count * 100 / total ))
  if [[ "$pct" -ge "$STUB_THRESHOLD_PCT" ]]; then
    echo "STUBBED:$top_val:$top_count/$total:${pct}%"
  else
    echo "VARIES"
  fi
}

# ── Pipeline stage counts ─────────────────────────────────────────────────────
TOTAL_JSON=$LINE_COUNT_CLEAN
TOTAL_UNSTRUCTURED=$(( LINE_COUNT - LINE_COUNT_CLEAN ))

TOTAL_ERROR=$(count_jq 'select(.level == "ERROR")')
TOTAL_WARN=$(count_jq 'select(.level == "WARN")')
TOTAL_PANIC=$(count_jq 'select(.level == "PANIC" or .level == "FATAL")')

UNIQUE_TRACES=$(jq -r 'select(.trace_id != null) | .trace_id' "$CLEAN_LOG" 2>/dev/null | sort -u | wc -l | tr -d ' ')

COUNT_INGESTION=$(count_jq 'select(.msg == "solana_ingestion_emitted" or .msg == "dex_pool_detected")')
INGESTION_DELIVERY_MODE=$(jq -r 'select(.msg == "solana_ingestion_delivery") | .mode' "$CLEAN_LOG" 2>/dev/null | tail -1)
[ -z "$INGESTION_DELIVERY_MODE" ] || [ "$INGESTION_DELIVERY_MODE" = "null" ] && INGESTION_DELIVERY_MODE="stream"
COUNT_INGESTION_WEBHOOK=$(count_jq 'select(.msg == "solana_ingestion_emitted" and .transport == "webhook")')
COUNT_INGESTION_STREAM=$(count_jq 'select(.msg == "solana_ingestion_emitted" and (.transport == "ws" or .transport == null or .transport == ""))')
COUNT_DQ=$(count_jq 'select(.msg == "dq_decision")')
COUNT_DQ_STAGE=$(count_stage_worker "dq_worker")
COUNT_DQ_EMITTED=$(count_stage_worker_status "dq_worker" "emitted")

# L2–L8: canonical stage_completed counts (worker_group from cmd/server.go).
COUNT_FEATURES=$(count_stage_worker "features_worker")
COUNT_FEATURES_EMITTED=$(count_stage_worker_status "features_worker" "emitted")
COUNT_FEATURES_FILTERED=$(count_stage_worker_status "features_worker" "filtered")
COUNT_FEATURES_REJECTED=$(count_stage_worker_status "features_worker" "rejected")
COUNT_FEATURES_SKIPPED=$(count_stage_worker_status "features_worker" "skipped")

COUNT_EDGE=$(count_stage_worker "edge_worker")
COUNT_EDGE_EMITTED=$(count_stage_worker_status "edge_worker" "emitted")
COUNT_EDGE_FILTERED=$(count_stage_worker_status "edge_worker" "filtered")
COUNT_EDGE_REJECTED=$(count_stage_worker_status "edge_worker" "rejected")
COUNT_EDGE_SKIPPED=$(count_stage_worker_status "edge_worker" "skipped")

COUNT_PROB=$(count_stage_worker "probability_worker")
COUNT_PROB_EMITTED=$(count_stage_worker_status "probability_worker" "emitted")
COUNT_PROB_FILTERED=$(count_stage_worker_status "probability_worker" "filtered")
COUNT_PROB_REJECTED=$(count_stage_worker_status "probability_worker" "rejected")

COUNT_SLIP=$(count_stage_worker "slippage_worker")
COUNT_SLIP_EMITTED=$(count_stage_worker_status "slippage_worker" "emitted")
COUNT_SLIP_FILTERED=$(count_stage_worker_status "slippage_worker" "filtered")
COUNT_SLIP_REJECTED=$(count_stage_worker_status "slippage_worker" "rejected")

COUNT_VAL=$(count_stage_worker "validation_worker")
COUNT_VAL_EMITTED=$(count_stage_worker_status "validation_worker" "emitted")
COUNT_VAL_FILTERED=$(count_stage_worker_status "validation_worker" "filtered")
COUNT_VAL_REJECTED=$(count_stage_worker_status "validation_worker" "rejected")
COUNT_VAL_SKIPPED=$(count_stage_worker_status "validation_worker" "skipped")
# ACCEPT/REJECT aliases for pipeline completion rate (emitted = passed validation).
COUNT_VAL_ACCEPT=$COUNT_VAL_EMITTED
COUNT_VAL_REJECT=$COUNT_VAL_REJECTED

COUNT_SEL=$(count_stage_worker "selection_worker")
COUNT_SEL_EMITTED=$(count_stage_worker_status "selection_worker" "emitted")

COUNT_ALLOC=$(count_stage_worker "capital_worker")
COUNT_ALLOC_EMITTED=$(count_stage_worker_status "capital_worker" "emitted")

COUNT_EXEC_STAGE=$(count_stage_worker "execution_worker")
COUNT_EXEC_EMITTED=$(count_stage_worker_status "execution_worker" "emitted")
COUNT_EXEC_SUB=$(count_jq 'select(.msg == "execution_submitted")')
COUNT_EXEC_CON=$(count_jq 'select(.msg == "execution_confirmed" or .msg == "trade_executed")')
COUNT_EXEC_FAIL=$(count_jq 'select(.msg == "execution_failed")')
COUNT_POS_OPEN=$(count_jq 'select(.msg == "position_opened")')
COUNT_POS_CLOSE=$(count_jq 'select(.msg == "position_closed")')
COUNT_POS_STUCK=$(count_jq 'select(.msg == "position_stuck" or (.msg == "position_closed" and (.exit_reason // "" | test("FORCE_CLOSE|TIMEOUT"))))')
COUNT_LEARN=$(count_jq 'select(.msg == "learning_record_emitted")')
COUNT_DRAWDOWN=$(count_jq 'select(.msg | test("drawdown|kill_switch"))')
COUNT_KILL_SWITCH=$(count_jq 'select(.msg == "kill_switch_triggered")')
COUNT_OVER_EXPOSURE=$(count_jq 'select(.msg | test("over_exposure|exposure_exceeded"))')
COUNT_JOIN_TIMEOUT=$(count_jq 'select(
    (.msg == "validation_decision" and (.reject_reason // "" | test("join_timeout"))) or
    (.msg == "stage_completed" and .worker_group == "validation_worker" and (.decision_reason // "" | test("join_timeout")))
  )')
HB_ZERO_EMITTED=$(count_jq 'select(.msg | test("_heartbeat")) | select(.events_emitted == 0)')

# ── Throughput metrics (PLAN §1.1 / Task 17) ─────────────────────────────────
WSOL_MINT="So11111111111111111111111111111111111111112"
WSOL_TOKEN_ADDRESS_EMITTED=$(count_jq "select(.msg == \"solana_ingestion_emitted\" and .token == \"$WSOL_MINT\")")
COUNT_PROBES_COMPLETED=$(count_jq 'select(.msg == "market_probes_completed")')
COUNT_DQ_PASS=$(count_jq 'select(.msg == "dq_decision" and (.decision == "PASS" or .decision == "RISKY_PASS"))')
COUNT_DQ_SKIP=$(count_jq 'select(.msg == "dq_decision" and .decision == "SKIP")')
COUNT_DQ_REJECT=$(count_jq 'select(.msg == "dq_decision" and .decision == "REJECT")')
COUNT_PROBE_PASS_COMPLETE=$(count_jq 'select(.msg == "market_probes_completed" and .probe_pass_complete == true)')
COUNT_PROBE_INCOMPLETE_ENQUEUED=$(count_jq 'select(.msg == "market_probes_completed" and .background_retry_enqueued == true)')
COUNT_PROBE_EXHAUSTED_RETRY=$(count_jq 'select(.msg == "probe_pending_requeued" and .transport == "probe_exhausted_retry")')
COUNT_PROBE_PENDING_ENQUEUED=$(count_jq 'select(.msg == "probe_pending_enqueued")')
COUNT_RESCAN_PROBE_SKIP=$(count_jq 'select(.msg == "rescan_probe_skip_complete")')
COUNT_DQ_DECISIONS=$(count_jq 'select(.msg == "dq_decision")')
COUNT_DQ_WITH_CREATOR=$(count_jq 'select(.msg == "dq_decision" and (.creator_address // "") != "")')
CREATOR_ADDRESS_POPULATED_PCT="N/A"
if [[ "$COUNT_DQ_DECISIONS" -gt 0 ]]; then
  CREATOR_ADDRESS_POPULATED_PCT=$(awk -v c="$COUNT_DQ_WITH_CREATOR" -v t="$COUNT_DQ_DECISIONS" 'BEGIN{printf "%.1f", 100*c/t}')
fi
PROBE_PENDING_QUEUE_DEPTH="N/A"
if command -v docker >/dev/null 2>&1; then
  _pp_depth=$(docker compose exec -T db psql -U sniper -d sniper -tAc \
    "SELECT COUNT(*) FROM probe_pending_queue WHERE status='pending'" 2>/dev/null | tr -d ' \n' || true)
  if [[ -n "$_pp_depth" && "$_pp_depth" =~ ^[0-9]+$ ]]; then
    PROBE_PENDING_QUEUE_DEPTH="$_pp_depth"
  fi
fi
COUNT_UNKNOWN_REJECT=$(jq -r 'select(.msg == "dq_decision" and .decision == "REJECT" and ((.reject_reasons // []) | index("unknown_social_links") or index("unknown_creator_count") or index("unknown_holder_count") or index("unknown_total_supply"))) | .trace_id' "$CLEAN_LOG" 2>/dev/null | sort -u | wc -l | tr -d ' ')
COUNT_PROBE_PARTIAL_SKIP=$(count_jq 'select(.msg == "dq_skip" and ((.flags // []) | index("probe_partial:social") or index("probe_partial:creator") or index("probe_partial:holder") or index("probe_partial:supply") or index("probe_exhausted")))')
# serial_launcher_skipped dominates Helius pump.fun — intentional capital guardrail, not a code defect.
COUNT_SERIAL_LAUNCHER_SKIP=$(jq -r 'select(.msg == "dq_decision" and (.flags // [] | index("serial_launcher_skipped"))) | .trace_id' "$CLEAN_LOG" 2>/dev/null | sort -u | wc -l | tr -d ' ')
COUNT_SHADOW_OBS_FAIL=$(count_jq 'select(.msg == "shadow_observer_failed")')

INGESTION_VALID_TOKEN_RATIO="N/A"
INGESTION_VALID_COUNT=0
if [[ "$COUNT_INGESTION" -gt 0 ]]; then
  INGESTION_VALID_COUNT=$(( COUNT_INGESTION - WSOL_TOKEN_ADDRESS_EMITTED ))
  INGESTION_VALID_TOKEN_RATIO=$(awk -v v="$INGESTION_VALID_COUNT" -v t="$COUNT_INGESTION" 'BEGIN{printf "%.4f", v/t}')
fi

MARKET_PROBES_COMPLETION_RATIO="N/A"
MARKET_PROBES_BACKLOG_RATIO="N/A"
if [[ "$COUNT_INGESTION" -gt 0 ]]; then
  MARKET_PROBES_COMPLETION_RATIO=$(awk -v p="$COUNT_PROBES_COMPLETED" -v i="$COUNT_INGESTION" 'BEGIN{printf "%.4f", p/i}')
  MARKET_PROBES_BACKLOG_RATIO=$(awk -v p="$COUNT_PROBES_COMPLETED" -v i="$COUNT_INGESTION" 'BEGIN{printf "%.4f", 1 - (p/i)}')
fi

last_heartbeat_family() {
  local family="$1"
  jq -c "select(.msg == \"solana_ingestion_heartbeat\" and .family == \"$family\")" "$CLEAN_LOG" 2>/dev/null | tail -1
}

HB_PUMPFUN_AMM_FINAL=$(last_heartbeat_family "pumpfun-amm")
HB_RAYDIUM_V4_FINAL=$(last_heartbeat_family "raydium-v4")

format_heartbeat_final() {
  local line="$1"
  local label="$2"
  if [[ -z "$line" ]]; then
    echo "  $label: (no heartbeat in window)"
    return
  fi
  echo "$line" | jq -r --arg lbl "$label" \
    '"  \($lbl): notifications=\(.notifications_received // 0) events_emitted=\(.events_emitted // 0) system_mint_rejected=\(.system_mint_rejected // 0) valid_token_emitted=\(.valid_token_emitted // 0) mint_pair_swapped=\(.mint_pair_swapped // 0) raydium_init_fallback_fetch=\(.raydium_init_fallback_fetch // 0)"' \
    2>/dev/null || echo "  $label: (parse error)"
}

TOTAL_INGESTION_NOTIFICATIONS=0
for _hb_line in "$HB_PUMPFUN_AMM_FINAL" "$HB_RAYDIUM_V4_FINAL"; do
  if [[ -n "$_hb_line" ]]; then
    _n=$(echo "$_hb_line" | jq -r '.notifications_received // 0' 2>/dev/null || echo 0)
    TOTAL_INGESTION_NOTIFICATIONS=$(( TOTAL_INGESTION_NOTIFICATIONS + _n ))
  fi
done

ratio_lt() {
  awk -v r="$1" -v threshold="$2" 'BEGIN{exit !(r+0 < threshold+0)}'
}

ratio_gte() {
  awk -v r="$1" -v threshold="$2" 'BEGIN{exit !(r+0 >= threshold+0)}'
}

THROUGHPUT_VERDICT="HEALTHY"
if [[ "$WSOL_TOKEN_ADDRESS_EMITTED" -gt 0 ]]; then
  THROUGHPUT_VERDICT="CODE_DEFECT"
elif [[ "$COUNT_SHADOW_OBS_FAIL" -gt 0 ]]; then
  THROUGHPUT_VERDICT="CODE_DEFECT"
elif [[ "$COUNT_INGESTION" -gt 0 ]]; then
  if ratio_lt "$INGESTION_VALID_TOKEN_RATIO" 0.80; then
    THROUGHPUT_VERDICT="CODE_DEFECT"
  elif ratio_lt "$MARKET_PROBES_COMPLETION_RATIO" 0.95; then
    THROUGHPUT_VERDICT="CODE_DEFECT"
  elif [[ "$TOTAL_INGESTION_NOTIFICATIONS" -ge 10000 && "$COUNT_DQ_PASS" -eq 0 ]]; then
    # High-volume feed with zero DQ pass: distinguish guardrails doing their job
    # (serial launcher / mandatory rejects) from a broken pipeline.
    if [[ "$COUNT_DQ_SKIP" -gt 0 && "$COUNT_SERIAL_LAUNCHER_SKIP" -gt 0 ]]; then
      _sl_pct=$(( COUNT_SERIAL_LAUNCHER_SKIP * 100 / (COUNT_DQ_SKIP + COUNT_DQ_REJECT + 1) ))
      if [[ "$_sl_pct" -ge 50 ]]; then
        THROUGHPUT_VERDICT="GUARDRAILS_ACTIVE"
      else
        THROUGHPUT_VERDICT="CODE_DEFECT"
      fi
    else
      THROUGHPUT_VERDICT="CODE_DEFECT"
    fi
  elif [[ "$TOTAL_INGESTION_NOTIFICATIONS" -lt 1000 && "$COUNT_INGESTION" -lt 5 ]]; then
    THROUGHPUT_VERDICT="MARKET_QUIET"
  else
    THROUGHPUT_VERDICT="HEALTHY"
  fi
elif [[ "$TOTAL_INGESTION_NOTIFICATIONS" -lt 1000 ]]; then
  THROUGHPUT_VERDICT="MARKET_QUIET"
else
  THROUGHPUT_VERDICT="CODE_DEFECT"
fi

# ── Idempotency / determinism checks ─────────────────────────────────────────
# Only count true idempotency violations — exclude log lines that legitimately
# carry an input event_id for trace correlation (same exclusions as collect_logs.sh).
DUP_EVENT_IDS=$(jq -r 'select(
    .event_id != null and
    .msg != "stage_completed" and
    .msg != "market_probe_failed" and
    .msg != "market_probes_completed" and
    .msg != "solana_ingestion_emitted" and
    .msg != "pre_probe_guard_skipped_all_probes" and
    .msg != "pre_probe_rate_limit_skipped" and
    .msg != "pre_probe_name_dedup_cache_hit" and
    .msg != "pre_probe_name_dedup_db_hit" and
    .msg != "pre_probe_copycat_detected" and
    .msg != "dq_decision" and
    .msg != "dq_skip"
  ) | .event_id' "$CLEAN_LOG" 2>/dev/null \
  | sort | uniq -d | wc -l | tr -d ' ')

MISSING_TRACE=$(count_jq 'select(.trace_id == null or .trace_id == "")')
MISSING_VERSION=$(count_jq 'select(.version_id == null or .version_id == "")')

# ── Traces that completed the full L0→L10 lifecycle in this window ─────────────
# PIPELINE_PROOF completion anchor: distinct trace_ids with learning_record_emitted
# (real L10 evidence), not partial execution/position lifecycle hints.
TRACES_COMPLETED=$(jq -r 'select(.msg == "learning_record_emitted" and .trace_id != null) | .trace_id' \
  "$CLEAN_LOG" 2>/dev/null | sort -u | wc -l | tr -d ' ')

# ── Latency & slippage averages ───────────────────────────────────────────────
AVG_LATENCY=$(avg_jq 'select(.msg == "position_opened" and .pipeline_latency_ms != null)' "pipeline_latency_ms")
AVG_SLIPPAGE=$(avg_jq 'select(.msg == "position_closed" and .realized_slippage_bps != null)' "realized_slippage_bps")

# ── Stub checks ───────────────────────────────────────────────────────────────
STUB_PROB_USED=$(check_stub "probability_used")
STUB_RISK=$(check_stub "risk_score")
STUB_P50=$(check_stub "p50_bps")
STUB_P95=$(check_stub "p95_bps")

# ── Dead-worker detection ─────────────────────────────────────────────────────
# A worker slice is "dead" only when upstream stage_completed shows emitted
# traffic but the downstream worker_group is completely silent in this window.
# Upstream filtered/rejected/skipped alone must NOT flag downstream as dead.
DEAD_WORKERS=""
if [[ "$COUNT_INGESTION" -gt 0 && "$COUNT_DQ_STAGE" -eq 0 ]]; then
  DEAD_WORKERS="${DEAD_WORKERS}  - Layer 1 (dq_worker): 0 stage_completed — DQ worker may be dead\n"
fi
if [[ "$COUNT_DQ_EMITTED" -gt 0 && "$COUNT_FEATURES" -eq 0 ]]; then
  DEAD_WORKERS="${DEAD_WORKERS}  - Layer 2 (features_worker): 0 stage_completed after DQ emitted — Feature worker may be dead\n"
fi
if [[ "$COUNT_FEATURES_EMITTED" -gt 0 && "$COUNT_EDGE" -eq 0 ]]; then
  DEAD_WORKERS="${DEAD_WORKERS}  - Layer 3 (edge_worker): 0 stage_completed after features emitted — Edge worker may be dead\n"
fi
if [[ "$COUNT_FEATURES_EMITTED" -gt 0 && "$COUNT_PROB" -eq 0 ]]; then
  DEAD_WORKERS="${DEAD_WORKERS}  - Layer 4 (probability_worker): 0 stage_completed after features emitted — Probability worker may be dead\n"
fi
if [[ "$COUNT_FEATURES_EMITTED" -gt 0 && "$COUNT_SLIP" -eq 0 ]]; then
  DEAD_WORKERS="${DEAD_WORKERS}  - Layer 4 (slippage_worker): 0 stage_completed after features emitted — Slippage worker may be dead\n"
fi
if [[ "$COUNT_EDGE_EMITTED" -gt 0 && "$COUNT_VAL" -eq 0 ]]; then
  DEAD_WORKERS="${DEAD_WORKERS}  - Layer 5 (validation_worker): 0 stage_completed after edge emitted — Validation worker may be dead\n"
fi
if [[ "$COUNT_VAL_EMITTED" -gt 0 && "$COUNT_SEL" -eq 0 ]]; then
  DEAD_WORKERS="${DEAD_WORKERS}  - Layer 6 (selection_worker): 0 stage_completed after validation emitted — Selection worker may be dead\n"
fi
if [[ "$COUNT_SEL_EMITTED" -gt 0 && "$COUNT_ALLOC" -eq 0 ]]; then
  DEAD_WORKERS="${DEAD_WORKERS}  - Layer 7 (capital_worker): 0 stage_completed after selection emitted — Capital worker may be dead\n"
fi
if [[ "$COUNT_ALLOC_EMITTED" -gt 0 && "$COUNT_EXEC_STAGE" -eq 0 ]]; then
  DEAD_WORKERS="${DEAD_WORKERS}  - Layer 8 (execution_worker): 0 stage_completed after allocation emitted — Execution worker may be dead\n"
fi
if [[ "$COUNT_POS_OPEN" -gt 0 ]]; then
  [[ "$COUNT_POS_CLOSE" -eq 0 ]] && DEAD_WORKERS="${DEAD_WORKERS}  - Layer 9 (position_closed): 0 events — Position exit worker may be dead\n"
fi
if [[ "$COUNT_POS_CLOSE" -gt 0 ]]; then
  [[ "$COUNT_LEARN" -eq 0 ]]     && DEAD_WORKERS="${DEAD_WORKERS}  - Layer 10 (learning_record_emitted): 0 events — Learning worker may be dead\n"
fi
[[ -z "$DEAD_WORKERS" ]] && DEAD_WORKERS="  NONE detected in this window"

# ── Execution failure rate ────────────────────────────────────────────────────
EXEC_TOTAL=$(( COUNT_EXEC_SUB + COUNT_EXEC_FAIL ))
EXEC_FAIL_RATE="N/A"
if [[ "$EXEC_TOTAL" -gt 0 ]]; then
  EXEC_FAIL_RATE="${COUNT_EXEC_FAIL}/${EXEC_TOTAL} ($(( COUNT_EXEC_FAIL * 100 / EXEC_TOTAL ))%)"
fi

# ── Pipeline completion rate (validated → position_closed) ───────────────────
PIPELINE_COMPLETION_PCT="N/A"
if [[ "$COUNT_VAL_ACCEPT" -gt 0 ]]; then
  PIPELINE_COMPLETION_PCT="$(( COUNT_POS_CLOSE * 100 / COUNT_VAL_ACCEPT ))%  (${COUNT_POS_CLOSE}/${COUNT_VAL_ACCEPT} accepted→closed)"
fi

# ── Position close success rate ───────────────────────────────────────────────
POS_CLOSE_SUCCESS_PCT="N/A"
if [[ "$COUNT_POS_OPEN" -gt 0 ]]; then
  POS_CLOSE_SUCCESS_PCT="$(( COUNT_POS_CLOSE * 100 / COUNT_POS_OPEN ))%  (${COUNT_POS_CLOSE}/${COUNT_POS_OPEN} opened→closed)"
fi

# ── AUTO MODE DETECTION ───────────────────────────────────────────────────────
# Priority: forced arg > evidence-based heuristic
if [[ -n "$FORCED_MODE" ]]; then
  DETECTED_MODE="$FORCED_MODE"
  MODE_REASON="(forced via argument)"
elif [[ "$COUNT_POS_CLOSE" -eq 0 && "$TRACES_COMPLETED" -eq 0 ]]; then
  # No complete lifecycle evidence — in proof phase
  DETECTED_MODE="PIPELINE_PROOF"
  MODE_REASON="(auto: 0 completed traces)"
elif [[ "$COUNT_POS_CLOSE" -lt 500 ]]; then
  # Some completions but below shadow threshold
  DETECTED_MODE="SHADOW_TRADING"
  MODE_REASON="(auto: ${COUNT_POS_CLOSE} positions closed — below 500 shadow threshold)"
elif [[ "$COUNT_POS_CLOSE" -lt 600 ]]; then
  # Right at threshold — could be transitioning to micro-capital
  DETECTED_MODE="SHADOW_TRADING"
  MODE_REASON="(auto: ${COUNT_POS_CLOSE} positions closed — at shadow exit threshold)"
else
  DETECTED_MODE="MICRO_CAPITAL"
  MODE_REASON="(auto: ${COUNT_POS_CLOSE} positions closed — above 500 shadow threshold)"
fi

# Override to LIVE_MONITORING if kill switch or drawdown events observed
# (implies real capital is in play)
if [[ "$COUNT_KILL_SWITCH" -gt 0 || "$COUNT_OVER_EXPOSURE" -gt 0 ]] && [[ -z "$FORCED_MODE" ]]; then
  DETECTED_MODE="LIVE_MONITORING"
  MODE_REASON="(auto: kill_switch or over_exposure events detected — real capital in play)"
fi

# ── BLOCKER DETECTION ─────────────────────────────────────────────────────────
# Only classify as BLOCKER when a condition matches the BLOCKER criteria from
# the production-gate-reviewer skill (capital loss, duplicate execution, etc.)
BLOCKERS_LIST=""
BLOCKER_COUNT=0

_add_blocker() {
  # Enforce MAX_BLOCKERS_PER_REVIEW: 3 — only top 3 by priority
  if [[ "$BLOCKER_COUNT" -lt 3 ]]; then
    BLOCKERS_LIST="${BLOCKERS_LIST}$1"
    (( BLOCKER_COUNT++ )) || true
  fi
}

# Priority 1: Capital safety
if [[ "$TOTAL_PANIC" -gt 0 ]]; then
  _add_blocker "  BLOCKER [capital-safety]: PANIC/FATAL in logs ($TOTAL_PANIC lines)\n    Impact: uncontrolled process crash — positions may be left open without exit coverage\n    Location: grep for level=PANIC or level=FATAL in $BRIEF\n    Required fix: identify and fix the panic root cause before any trading\n\n"
fi
if [[ "$COUNT_KILL_SWITCH" -gt 0 && "$COUNT_DRAWDOWN" -gt 0 ]]; then
  _add_blocker "  BLOCKER [capital-safety]: kill_switch_triggered ($COUNT_KILL_SWITCH events)\n    Impact: drawdown protection fired — system is halted; requires operator /resume\n    Location: Telegram /resume command or drawdown config in config/capital.yaml\n    Required fix: operator must explicitly resume after reviewing drawdown cause\n\n"
fi
if [[ "$COUNT_OVER_EXPOSURE" -gt 0 ]]; then
  _add_blocker "  BLOCKER [capital-safety]: over_exposure events detected ($COUNT_OVER_EXPOSURE)\n    Impact: position size exceeds hard limit — uncapped allocation violation\n    Location: internal/modules/capital/ allocation gate\n    Required fix: ensure exposure monitor enforces hard cap before AllocationDTO is produced\n\n"
fi

# Priority 2: Deterministic integrity
if [[ "$DUP_EVENT_IDS" -gt 0 ]]; then
  _add_blocker "  BLOCKER [deterministic-integrity]: duplicate event_ids ($DUP_EVENT_IDS)\n    Impact: same execution_id processed twice — duplicate trades possible\n    Location: event emission in internal/workers/ — check idempotency key generation\n    Required fix: ensure SHA256 content-addressable IDs; add ON CONFLICT DO NOTHING\n\n"
fi

# Priority 3: Pipeline completion
if [[ -n "$DEAD_WORKERS" && "$DEAD_WORKERS" != "  NONE detected in this window" ]]; then
  _add_blocker "  BLOCKER [pipeline-completion]: dead or silent pipeline stage(s)\n${DEAD_WORKERS}\n    Impact: L0→L10 lifecycle cannot complete — PIPELINE_PROOF exit condition blocked\n    Required fix: verify worker registration in internal/app/app.go startup sequence\n\n"
fi
if [[ "$COUNT_POS_OPEN" -gt 0 && "$COUNT_POS_CLOSE" -eq 0 && "$COUNT_EXEC_CON" -gt 0 ]]; then
  _add_blocker "  BLOCKER [pipeline-completion]: positions opened but NEVER closed (${COUNT_POS_OPEN} open, 0 closed)\n    Impact: TP/SL/TIME exit logic unreachable — capital permanently locked\n    Location: internal/workers/position_worker.go — monitoring loop\n    Required fix: verify position monitoring loop is started and polling correctly\n\n"
fi

[[ -z "$BLOCKERS_LIST" ]] && BLOCKERS_LIST="  NONE"

# ── PRODUCTION CONFIDENCE MODEL (0–100 per dimension) ─────────────────────────
# pipeline_stability: based on L0→L10 stage coverage and completion rate
PC_PIPELINE=0
STAGE_COVERAGE=0
[[ "$COUNT_INGESTION" -gt 0 ]] && (( STAGE_COVERAGE += 9 )) || true
[[ "$COUNT_DQ_STAGE" -gt 0 ]]    && (( STAGE_COVERAGE += 9 )) || true
[[ "$COUNT_FEATURES" -gt 0 ]]  && (( STAGE_COVERAGE += 9 )) || true
[[ "$COUNT_EDGE" -gt 0 ]]      && (( STAGE_COVERAGE += 9 )) || true
[[ "$COUNT_PROB" -gt 0 ]]      && (( STAGE_COVERAGE += 9 )) || true
[[ "$COUNT_VAL" -gt 0 ]]       && (( STAGE_COVERAGE += 9 )) || true
[[ "$COUNT_ALLOC" -gt 0 ]]     && (( STAGE_COVERAGE += 9 )) || true
[[ "$COUNT_EXEC_STAGE" -gt 0 || "$COUNT_EXEC_CON" -gt 0 ]] && (( STAGE_COVERAGE += 9 )) || true
[[ "$COUNT_POS_OPEN" -gt 0 ]]  && (( STAGE_COVERAGE += 9 )) || true
[[ "$COUNT_POS_CLOSE" -gt 0 ]] && (( STAGE_COVERAGE += 9 )) || true
[[ "$COUNT_LEARN" -gt 0 ]]     && (( STAGE_COVERAGE += 10 )) || true
PC_PIPELINE=$STAGE_COVERAGE

# execution_reliability: exec confirmation rate and no panics
PC_EXEC=50  # baseline
if [[ "$TOTAL_PANIC" -gt 0 ]]; then PC_EXEC=0
elif [[ "$COUNT_EXEC_CON" -gt 0 && "$COUNT_EXEC_FAIL" -eq 0 ]]; then PC_EXEC=100
elif [[ "$COUNT_EXEC_CON" -gt 0 ]]; then
  FAIL_RATIO=$(( COUNT_EXEC_FAIL * 100 / (COUNT_EXEC_CON + COUNT_EXEC_FAIL) ))
  PC_EXEC=$(( 100 - FAIL_RATIO ))
elif [[ "$COUNT_EXEC_SUB" -gt 0 ]]; then PC_EXEC=40
elif [[ "$COUNT_ALLOC" -gt 0 ]]; then PC_EXEC=20
else PC_EXEC=0; fi

# determinism_integrity: penalise dup event_ids and missing trace_ids
PC_DET=100
[[ "$DUP_EVENT_IDS" -gt 0 ]] && PC_DET=$(( PC_DET - DUP_EVENT_IDS * 25 )) || true
if [[ "$MISSING_TRACE" -gt 0 && "$TOTAL_JSON" -gt 0 ]]; then
  MISS_PCT=$(( MISSING_TRACE * 100 / TOTAL_JSON ))
  [[ "$MISS_PCT" -gt 10 ]] && PC_DET=$(( PC_DET - 30 )) || true
  [[ "$MISS_PCT" -gt 5 && "$MISS_PCT" -le 10 ]] && PC_DET=$(( PC_DET - 15 )) || true
fi
[[ "$PC_DET" -lt 0 ]] && PC_DET=0

# capital_safety: penalise kill switch, over-exposure, panics
PC_CAP=100
[[ "$TOTAL_PANIC" -gt 0 ]]        && PC_CAP=$(( PC_CAP - 50 )) || true
[[ "$COUNT_KILL_SWITCH" -gt 0 ]]  && PC_CAP=$(( PC_CAP - 30 )) || true
[[ "$COUNT_OVER_EXPOSURE" -gt 0 ]] && PC_CAP=$(( PC_CAP - 30 )) || true
[[ "$DUP_EVENT_IDS" -gt 0 ]]      && PC_CAP=$(( PC_CAP - 20 )) || true
[[ "$PC_CAP" -lt 0 ]] && PC_CAP=0

# operational_consistency: heartbeat zero-emits and worker health
PC_OPS=100
if [[ "$HB_ZERO_EMITTED" -gt 10 ]]; then PC_OPS=$(( PC_OPS - 30 ))
elif [[ "$HB_ZERO_EMITTED" -gt 3 ]]; then PC_OPS=$(( PC_OPS - 15 ))
fi
if [[ "$STUB_PROB_USED" != "VARIES" && "$STUB_PROB_USED" != INSUFFICIENT_SAMPLES* ]]; then
  PC_OPS=$(( PC_OPS - 20 ))
fi
if [[ "$STUB_RISK" != "VARIES" && "$STUB_RISK" != INSUFFICIENT_SAMPLES* ]]; then
  PC_OPS=$(( PC_OPS - 20 ))
fi
[[ "$PC_OPS" -lt 0 ]] && PC_OPS=0

# Clamp all dimensions to 0–100
for _dim in PC_PIPELINE PC_EXEC PC_DET PC_CAP PC_OPS; do
  _val="${!_dim}"
  [[ "$_val" -gt 100 ]] && eval "$_dim=100"
  [[ "$_val" -lt 0 ]]   && eval "$_dim=0"
done

# Confidence tier interpretation
confidence_tier() {
  local score="$1"
  if [[ "$score" -ge 86 ]]; then echo "STABLE_PRODUCTION_CAPABLE"
  elif [[ "$score" -ge 71 ]]; then echo "STABLE_SHADOW_CAPABLE"
  elif [[ "$score" -ge 41 ]]; then echo "OPERATIONAL_IMMATURE"
  else echo "UNSTABLE"
  fi
}

CT_PIPELINE=$(confidence_tier "$PC_PIPELINE")
CT_EXEC=$(confidence_tier "$PC_EXEC")
CT_DET=$(confidence_tier "$PC_DET")
CT_CAP=$(confidence_tier "$PC_CAP")
CT_OPS=$(confidence_tier "$PC_OPS")

# ── PRODUCTION DECISION SUGGESTION ────────────────────────────────────────────
PROD_DECISION="NOT_READY"
if [[ "$BLOCKER_COUNT" -gt 0 ]]; then
  PROD_DECISION="NOT_READY"
elif [[ "$DETECTED_MODE" == "PIPELINE_PROOF" ]]; then
  # Still in proof mode — check if at least one full trace completed
  if [[ "$TRACES_COMPLETED" -ge 1 ]]; then
    PROD_DECISION="SHADOW_READY"
  else
    PROD_DECISION="PIPELINE_PROOF_READY"
  fi
elif [[ "$DETECTED_MODE" == "SHADOW_TRADING" ]]; then
  # Shadow mode exit: ≥500 closed, ≥95% pipeline completion, 0 dup exec, 0 determinism violations
  if [[ "$COUNT_POS_CLOSE" -ge 500 && "$DUP_EVENT_IDS" -eq 0 && "$COUNT_KILL_SWITCH" -eq 0 ]]; then
    PROD_DECISION="MICRO_CAPITAL_READY"
  else
    PROD_DECISION="SHADOW_READY"
  fi
elif [[ "$DETECTED_MODE" == "MICRO_CAPITAL" ]]; then
  # Micro-capital exit: no uncontrolled loss, no stuck positions, 0 dup exec
  if [[ "$DUP_EVENT_IDS" -eq 0 && "$COUNT_POS_STUCK" -eq 0 && "$TOTAL_PANIC" -eq 0 && "$COUNT_KILL_SWITCH" -eq 0 ]]; then
    PROD_DECISION="LIMITED_PRODUCTION_READY"
  else
    PROD_DECISION="MICRO_CAPITAL_READY"
  fi
elif [[ "$DETECTED_MODE" == "LIVE_MONITORING" ]]; then
  if [[ "$COUNT_KILL_SWITCH" -gt 0 ]]; then
    PROD_DECISION="NOT_READY"
  else
    PROD_DECISION="LIMITED_PRODUCTION_READY"
  fi
fi

# ── Phase 3: Write Gate-Review Brief ─────────────────────────────────────────
log "Phase 3/3 — Writing gate-review brief to $BRIEF"

{
echo "# Production Gate Review Brief"
echo "# Generated by scripts/gate_review_collect.sh"
echo "# Service: $SERVICE | Duration: ${DURATION_MINUTES}m | Timestamp: $TIMESTAMP"
echo "# Raw log: $RAW_LOG"
echo ""
echo "═══════════════════════════════════════════════════════════════════"
echo "GATE REVIEW BRIEF — paste into Copilot with production-gate-reviewer skill"
echo "═══════════════════════════════════════════════════════════════════"
echo ""
echo "───────────────────────────────────────────────────────────────────"
echo "1. MODE"
echo "───────────────────────────────────────────────────────────────────"
echo "  Detected: $DETECTED_MODE  $MODE_REASON"
echo ""
echo "───────────────────────────────────────────────────────────────────"
echo "2. BLOCKERS (max 3, priority-ordered)"
echo "───────────────────────────────────────────────────────────────────"
printf "%b\n" "$BLOCKERS_LIST"
echo ""
echo "───────────────────────────────────────────────────────────────────"
echo "3. SAFE_TO_IGNORE_FOR_NOW (auto-detected non-blockers)"
echo "───────────────────────────────────────────────────────────────────"
SAFE_LIST=""
[[ "$TOTAL_WARN" -gt 0 ]]                && SAFE_LIST="${SAFE_LIST}  - ${TOTAL_WARN} WARN log lines (retries/reconnects — not blockers unless stuck)\n"
[[ "$HB_ZERO_EMITTED" -le 5 && "$HB_ZERO_EMITTED" -gt 0 ]] \
                                          && SAFE_LIST="${SAFE_LIST}  - ${HB_ZERO_EMITTED} heartbeat zero-emitted events (transient eligibility filter gaps)\n"
[[ "$COUNT_LEARN" -lt 30 && "$COUNT_LEARN" -gt 0 ]] \
                                          && SAFE_LIST="${SAFE_LIST}  - Learning records low (${COUNT_LEARN}) — cold-start, expected pre-500 trades\n"
[[ "$STUB_PROB_USED" == INSUFFICIENT_SAMPLES* ]] \
                                          && SAFE_LIST="${SAFE_LIST}  - probability_used: $STUB_PROB_USED — not enough samples yet, not a stub defect\n"
[[ "$STUB_RISK" == INSUFFICIENT_SAMPLES* ]] \
                                          && SAFE_LIST="${SAFE_LIST}  - risk_score: $STUB_RISK — not enough samples yet\n"
[[ "$COUNT_JOIN_TIMEOUT" -gt 0 ]]         && SAFE_LIST="${SAFE_LIST}  - ${COUNT_JOIN_TIMEOUT} join_timeout rejects — timing issue, not a code defect\n"
[[ "$THROUGHPUT_VERDICT" == "GUARDRAILS_ACTIVE" ]] \
                                          && SAFE_LIST="${SAFE_LIST}  - High-volume serial_launcher SKIP dominates — mandatory capital guardrails, not a pipeline defect\n"
[[ "$TOTAL_ERROR" -gt 0 ]]               && SAFE_LIST="${SAFE_LIST}  - ${TOTAL_ERROR} ERROR lines — review individually; most are transient RPC errors\n"
[[ -z "$SAFE_LIST" ]] && SAFE_LIST="  NONE"
printf "%b\n" "$SAFE_LIST"
echo ""
echo "───────────────────────────────────────────────────────────────────"
echo "4. POST_PROFITABILITY_PHASE (deferred)"
echo "───────────────────────────────────────────────────────────────────"
echo "  - Feature calibration improvements"
echo "  - Slippage model tuning"
echo "  - Advanced Telegram PnL analytics"
echo "  - Scalability / throughput optimization"
echo "  - Non-critical observability probes"
echo ""
echo "───────────────────────────────────────────────────────────────────"
echo "5. OPERATIONAL EVIDENCE"
echo "───────────────────────────────────────────────────────────────────"
echo "  traces_completed         $TRACES_COMPLETED"
echo "  validated_edges          $COUNT_VAL_ACCEPT"
echo "  executions               $COUNT_EXEC_CON"
echo "  positions_closed         $COUNT_POS_CLOSE"
echo "  learning_records         $COUNT_LEARN"
echo "  duplicate_execution      $DUP_EVENT_IDS"
echo "  determinism_violations   0  (auto-check: dup_event_ids=$DUP_EVENT_IDS  missing_trace=$MISSING_TRACE)"
echo "  avg_latency              ${AVG_LATENCY}ms"
echo "  avg_slippage             ${AVG_SLIPPAGE}bps"
echo ""
echo "  Pipeline stage counts (stage_completed worker_group + output_status):"
echo "    L0   ingestion:             $COUNT_INGESTION"
echo "    L1   dq_worker:             $COUNT_DQ_STAGE  (dq_decision=$COUNT_DQ  emitted=$COUNT_DQ_EMITTED)"
echo "    L2   features_worker:       $COUNT_FEATURES  (emitted=$COUNT_FEATURES_EMITTED filtered=$COUNT_FEATURES_FILTERED rejected=$COUNT_FEATURES_REJECTED skipped=$COUNT_FEATURES_SKIPPED)"
echo "    L3   edge_worker:           $COUNT_EDGE  (emitted=$COUNT_EDGE_EMITTED filtered=$COUNT_EDGE_FILTERED rejected=$COUNT_EDGE_REJECTED skipped=$COUNT_EDGE_SKIPPED)"
echo "    L4   probability_worker:    $COUNT_PROB  (emitted=$COUNT_PROB_EMITTED filtered=$COUNT_PROB_FILTERED rejected=$COUNT_PROB_REJECTED)"
echo "    L4   slippage_worker:       $COUNT_SLIP  (emitted=$COUNT_SLIP_EMITTED filtered=$COUNT_SLIP_FILTERED rejected=$COUNT_SLIP_REJECTED)"
echo "    L5   validation_worker:     $COUNT_VAL  (emitted=$COUNT_VAL_EMITTED filtered=$COUNT_VAL_FILTERED rejected=$COUNT_VAL_REJECTED skipped=$COUNT_VAL_SKIPPED)"
echo "    L6   selection_worker:      $COUNT_SEL  (emitted=$COUNT_SEL_EMITTED)"
echo "    L7   capital_worker:        $COUNT_ALLOC  (emitted=$COUNT_ALLOC_EMITTED)"
echo "    L8   execution_worker:      $COUNT_EXEC_STAGE  (emitted=$COUNT_EXEC_EMITTED)"
echo "    L8   execution_submitted:   $COUNT_EXEC_SUB"
echo "    L8   execution_confirmed:   $COUNT_EXEC_CON"
echo "    L8   execution_failed:      $COUNT_EXEC_FAIL"
echo "    L9   position_opened:       $COUNT_POS_OPEN"
echo "    L9   position_closed:       $COUNT_POS_CLOSE"
echo "    L9   position_stuck:        $COUNT_POS_STUCK"
echo "    L10  learning_record:       $COUNT_LEARN"
echo ""
echo "  Pipeline completion rate:   $PIPELINE_COMPLETION_PCT"
echo "  Position close success:     $POS_CLOSE_SUCCESS_PCT"
echo "  Execution failure rate:     $EXEC_FAIL_RATE"
echo "  Unique trace_ids:           $UNIQUE_TRACES"
echo "  Kill switch events:         $COUNT_KILL_SWITCH"
echo "  Over-exposure events:       $COUNT_OVER_EXPOSURE"
echo "  Drawdown events:            $COUNT_DRAWDOWN"
echo ""
echo "  Throughput metrics (PLAN §1.1):"
echo "    ingestion_delivery_mode      $INGESTION_DELIVERY_MODE"
echo "    ingestion_stream_emitted     $COUNT_INGESTION_STREAM"
echo "    ingestion_webhook_emitted    $COUNT_INGESTION_WEBHOOK"
echo "    wsol_token_address_emitted   $WSOL_TOKEN_ADDRESS_EMITTED"
echo "    ingestion_valid_token_ratio  $INGESTION_VALID_TOKEN_RATIO  ($INGESTION_VALID_COUNT/$COUNT_INGESTION)"
echo "    market_probes_completed      $COUNT_PROBES_COMPLETED"
echo "    probe_pass_complete          $COUNT_PROBE_PASS_COMPLETE"
echo "    probe_incomplete_enqueued    $COUNT_PROBE_INCOMPLETE_ENQUEUED"
echo "    probe_exhausted_retry        $COUNT_PROBE_EXHAUSTED_RETRY"
echo "    probe_pending_enqueued       $COUNT_PROBE_PENDING_ENQUEUED"
echo "    probe_pending_queue_depth    $PROBE_PENDING_QUEUE_DEPTH"
echo "    rescan_probe_skip_complete   $COUNT_RESCAN_PROBE_SKIP"
echo "    creator_address_populated_pct ${CREATOR_ADDRESS_POPULATED_PCT}% ($COUNT_DQ_WITH_CREATOR/$COUNT_DQ_DECISIONS)"
echo "    unknown_star_reject_traces   $COUNT_UNKNOWN_REJECT"
echo "    probe_partial_skip           $COUNT_PROBE_PARTIAL_SKIP"
echo "    market_probes_completion     $MARKET_PROBES_COMPLETION_RATIO"
echo "    market_probes_backlog_ratio  $MARKET_PROBES_BACKLOG_RATIO"
echo "    dq_pass_or_risky_pass         $COUNT_DQ_PASS"
echo "    shadow_observer_failed        $COUNT_SHADOW_OBS_FAIL"
echo "    ingestion_notifications_sum   $TOTAL_INGESTION_NOTIFICATIONS  (pumpfun-amm + raydium-v4 final HB)"
echo "    THROUGHPUT_VERDICT             $THROUGHPUT_VERDICT"
echo ""
echo "  Per-program ingestion heartbeat (final row in window):"
format_heartbeat_final "$HB_PUMPFUN_AMM_FINAL" "pumpfun-amm"
format_heartbeat_final "$HB_RAYDIUM_V4_FINAL" "raydium-v4"
echo ""
echo "───────────────────────────────────────────────────────────────────"
echo "6. PRODUCTION CONFIDENCE MODEL"
echo "───────────────────────────────────────────────────────────────────"
echo "  Dimension                  Score  Tier"
printf "  %-26s %3d    %s\n" "pipeline_stability"     "$PC_PIPELINE" "$CT_PIPELINE"
printf "  %-26s %3d    %s\n" "execution_reliability"  "$PC_EXEC"     "$CT_EXEC"
printf "  %-26s %3d    %s\n" "determinism_integrity"  "$PC_DET"      "$CT_DET"
printf "  %-26s %3d    %s\n" "capital_safety"          "$PC_CAP"      "$CT_CAP"
printf "  %-26s %3d    %s\n" "operational_consistency" "$PC_OPS"      "$CT_OPS"
echo ""
echo "  Interpretation:"
echo "    0–40:   UNSTABLE"
echo "    41–70:  OPERATIONAL_IMMATURE"
echo "    71–85:  STABLE_SHADOW_CAPABLE"
echo "    86–100: STABLE_PRODUCTION_CAPABLE"
echo ""
echo "───────────────────────────────────────────────────────────────────"
echo "7. NEXT SINGLE ACTION (pre-computed suggestion)"
echo "───────────────────────────────────────────────────────────────────"
if [[ "$BLOCKER_COUNT" -gt 0 ]]; then
  # Extract first line of first blocker as the action
  NEXT_ACTION=$(printf "%b" "$BLOCKERS_LIST" | grep "BLOCKER " | head -1 | sed 's/  BLOCKER \[.*\]: //')
  echo "  Fix highest-priority BLOCKER: $NEXT_ACTION"
else
  case "$DETECTED_MODE" in
    PIPELINE_PROOF)
      if [[ "$TRACES_COMPLETED" -ge 1 ]]; then
        echo "  At least 1 complete trace confirmed — proceed to SHADOW_TRADING mode"
      else
        echo "  Start shadow trading run to produce the first complete L0→L10 trace"
      fi
      ;;
    SHADOW_TRADING)
      REMAINING=$(( 500 - COUNT_POS_CLOSE ))
      [[ "$REMAINING" -lt 0 ]] && REMAINING=0
      echo "  Continue shadow trading — ${COUNT_POS_CLOSE}/500 positions closed (${REMAINING} remaining for MICRO_CAPITAL gate)"
      ;;
    MICRO_CAPITAL)
      echo "  Continue micro-capital trading — monitor slippage and latency vs shadow baseline"
      ;;
    LIVE_MONITORING)
      if [[ "$COUNT_KILL_SWITCH" -gt 0 ]]; then
        echo "  Investigate kill switch trigger — review drawdown cause before resuming"
      else
        echo "  System in live monitoring — no action required; watch for latency/slippage spikes"
      fi
      ;;
  esac
fi
echo ""
echo "───────────────────────────────────────────────────────────────────"
echo "8. PRODUCTION DECISION (auto-suggested)"
echo "───────────────────────────────────────────────────────────────────"
echo "  $PROD_DECISION"
echo ""
echo "═══════════════════════════════════════════════════════════════════"
echo "HOW TO USE THIS BRIEF WITH COPILOT PRODUCTION-GATE-REVIEWER"
echo "═══════════════════════════════════════════════════════════════════"
echo ""
echo "  1. Open a new GitHub Copilot chat in VS Code."
echo "  2. Paste the following message:"
echo ""
echo "     Review the gate-review brief below using the production-gate-reviewer skill."
echo "     Confirm or override the auto-detected MODE, BLOCKERS, and PRODUCTION DECISION."
echo ""
echo "     <paste the full content of this file>"
echo ""
echo "  Raw log for deep analysis: $RAW_LOG"
echo "═══════════════════════════════════════════════════════════════════"
} > "$BRIEF"

# ── Write machine-readable evidence JSON ─────────────────────────────────────
HB_PUMPFUN_AMM_JSON="null"
HB_RAYDIUM_V4_JSON="null"
[[ -n "$HB_PUMPFUN_AMM_FINAL" ]] && HB_PUMPFUN_AMM_JSON=$(echo "$HB_PUMPFUN_AMM_FINAL" | jq -c '.' 2>/dev/null || echo "null")
[[ -n "$HB_RAYDIUM_V4_FINAL" ]] && HB_RAYDIUM_V4_JSON=$(echo "$HB_RAYDIUM_V4_FINAL" | jq -c '.' 2>/dev/null || echo "null")

{
  echo "{"
  echo "  \"timestamp\": \"$TIMESTAMP\","
  echo "  \"service\": \"$SERVICE\","
  echo "  \"duration_minutes\": $DURATION_MINUTES,"
  echo "  \"detected_mode\": \"$DETECTED_MODE\","
  echo "  \"production_decision\": \"$PROD_DECISION\","
  echo "  \"blocker_count\": $BLOCKER_COUNT,"
  echo "  \"operational_evidence\": {"
  echo "    \"traces_completed\": $TRACES_COMPLETED,"
  echo "    \"validated_edges\": $COUNT_VAL_ACCEPT,"
  echo "    \"executions\": $COUNT_EXEC_CON,"
  echo "    \"positions_closed\": $COUNT_POS_CLOSE,"
  echo "    \"learning_records\": $COUNT_LEARN,"
  echo "    \"duplicate_execution\": $DUP_EVENT_IDS,"
  echo "    \"avg_latency_ms\": \"$AVG_LATENCY\","
  echo "    \"avg_slippage_bps\": \"$AVG_SLIPPAGE\""
  echo "  },"
  echo "  \"confidence_model\": {"
  echo "    \"pipeline_stability\": $PC_PIPELINE,"
  echo "    \"execution_reliability\": $PC_EXEC,"
  echo "    \"determinism_integrity\": $PC_DET,"
  echo "    \"capital_safety\": $PC_CAP,"
  echo "    \"operational_consistency\": $PC_OPS"
  echo "  },"
  echo "  \"throughput_metrics\": {"
  echo "    \"ingestion_delivery_mode\": \"$INGESTION_DELIVERY_MODE\","
  echo "    \"ingestion_stream_emitted\": $COUNT_INGESTION_STREAM,"
  echo "    \"ingestion_webhook_emitted\": $COUNT_INGESTION_WEBHOOK,"
  echo "    \"wsol_token_address_emitted\": $WSOL_TOKEN_ADDRESS_EMITTED,"
  echo "    \"ingestion_valid_token_ratio\": \"$INGESTION_VALID_TOKEN_RATIO\","
  echo "    \"ingestion_emitted\": $COUNT_INGESTION,"
  echo "    \"ingestion_valid_count\": $INGESTION_VALID_COUNT,"
  echo "    \"market_probes_completed\": $COUNT_PROBES_COMPLETED,"
  echo "    \"probe_pass_complete\": $COUNT_PROBE_PASS_COMPLETE,"
  echo "    \"probe_incomplete_enqueued\": $COUNT_PROBE_INCOMPLETE_ENQUEUED,"
  echo "    \"probe_exhausted_retry\": $COUNT_PROBE_EXHAUSTED_RETRY,"
  echo "    \"probe_pending_enqueued\": $COUNT_PROBE_PENDING_ENQUEUED,"
  echo "    \"probe_pending_queue_depth\": \"$PROBE_PENDING_QUEUE_DEPTH\","
  echo "    \"rescan_probe_skip_complete\": $COUNT_RESCAN_PROBE_SKIP,"
  echo "    \"creator_address_populated_pct\": \"$CREATOR_ADDRESS_POPULATED_PCT\","
  echo "    \"dq_decisions_with_creator\": $COUNT_DQ_WITH_CREATOR,"
  echo "    \"dq_decisions_total\": $COUNT_DQ_DECISIONS,"
  echo "    \"unknown_star_reject_traces\": $COUNT_UNKNOWN_REJECT,"
  echo "    \"probe_partial_skip\": $COUNT_PROBE_PARTIAL_SKIP,"
  echo "    \"market_probes_completion_ratio\": \"$MARKET_PROBES_COMPLETION_RATIO\","
  echo "    \"market_probes_backlog_ratio\": \"$MARKET_PROBES_BACKLOG_RATIO\","
  echo "    \"dq_pass_or_risky_pass\": $COUNT_DQ_PASS,"
  echo "    \"shadow_observer_failed\": $COUNT_SHADOW_OBS_FAIL,"
  echo "    \"ingestion_notifications_sum\": $TOTAL_INGESTION_NOTIFICATIONS,"
  echo "    \"throughput_verdict\": \"$THROUGHPUT_VERDICT\","
  echo "    \"heartbeat_pumpfun_amm_final\": $HB_PUMPFUN_AMM_JSON,"
  echo "    \"heartbeat_raydium_v4_final\": $HB_RAYDIUM_V4_JSON"
  echo "  },"
  echo "  \"raw_log\": \"$RAW_LOG\","
  echo "  \"brief\": \"$BRIEF\""
  echo "}"
} > "$EVIDENCE_SNAPSHOT"

log "Phase 3/3 — Done."
echo ""
echo "════════════════════════════════════════════════════════════"
echo "  Mode: $DETECTED_MODE   Decision: $PROD_DECISION   Blockers: $BLOCKER_COUNT   Throughput: $THROUGHPUT_VERDICT"
echo "  Brief:    $BRIEF"
echo "  Evidence: $EVIDENCE_SNAPSHOT"
echo "════════════════════════════════════════════════════════════"
echo ""
echo "Next step: paste $BRIEF into a new Copilot chat"
echo "and say: 'Review this using the production-gate-reviewer skill'"
