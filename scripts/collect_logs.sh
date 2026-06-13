#!/usr/bin/env bash
# collect_logs.sh — Collect live bot logs, pre-analyse them, write a
# structured findings file ready for the log-reviewer Copilot session.
#
# Usage:
#   ./scripts/collect_logs.sh [DURATION_MINUTES] [SERVICE]
#
#   DURATION_MINUTES  How long to collect logs. Default: 60
#   SERVICE           Docker Compose service name. Default: bot
#
# Output (all under output/logs/):
#   raw_<TIMESTAMP>.log       — full newline-delimited JSON log
#   summary_<TIMESTAMP>.txt   — pre-analysed findings ready for log-reviewer
#
# After the script finishes, open a new Copilot chat and paste:
#   "Review this log summary using the log-reviewer skill: output/logs/summary_<TIMESTAMP>.txt"
#
# The script never modifies source code. It is read-only, append-only to output/.

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
# Two modes:
#   Collect:  ./scripts/collect_logs.sh [MINS] [SERVICE]
#   Analyze:  ./scripts/collect_logs.sh --analyze /path/to/raw_TIMESTAMP.log

OUTPUT_DIR="$(cd "$(dirname "$0")/.." && pwd)/output/logs"

ANALYZE_ONLY=false
if [[ "${1:-}" == "--analyze" ]]; then
  ANALYZE_ONLY=true
  _INPUT="${2:-}"
  [[ -z "$_INPUT" ]] && { echo "[collect_logs] FATAL: --analyze requires a path to a raw log file" >&2; exit 1; }
  [[ -f "$_INPUT" ]] || { echo "[collect_logs] FATAL: File not found: $_INPUT" >&2; exit 1; }
  RAW_LOG="$(cd "$(dirname "$_INPUT")" && pwd)/$(basename "$_INPUT")"
  _BASE="$(basename "$RAW_LOG" .log)"
  TIMESTAMP="${_BASE#raw_}"
  [[ "$TIMESTAMP" == "$_BASE" ]] && TIMESTAMP="reanalysis_$(date +%Y%m%d_%H%M%S)"
  DURATION_MINUTES="0"
  SERVICE="N/A"
else
  DURATION_MINUTES="${1:-60}"
  SERVICE="${2:-bot}"
  TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
  RAW_LOG="$OUTPUT_DIR/raw_${TIMESTAMP}.log"
fi

CLEAN_LOG="$OUTPUT_DIR/clean_${TIMESTAMP}.log"
SUMMARY="$OUTPUT_DIR/summary_${TIMESTAMP}.txt"
PRS_SNAPSHOT="$OUTPUT_DIR/prs_${TIMESTAMP}.json"

# Stub-detection: flag a numeric field as stubbed when its value appears in
# >90% of sampled lines (threshold ≥ 20 samples).
STUB_THRESHOLD_PCT=90
MIN_SAMPLES=20

# ── Helpers ───────────────────────────────────────────────────────────────────
log()  { echo "[collect_logs] $*" >&2; }
die()  { echo "[collect_logs] FATAL: $*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "'$1' not found — install it first."
}

require_cmd docker
require_cmd jq
require_cmd awk
require_cmd sort
require_cmd uniq

# ── Setup ─────────────────────────────────────────────────────────────────────
mkdir -p "$OUTPUT_DIR"
log "Output directory: $OUTPUT_DIR"
log "Raw log:          $RAW_LOG"
log "Summary:          $SUMMARY"
log "Duration:         ${DURATION_MINUTES} minute(s)"
log "Service:          $SERVICE"
echo ""

# ── Phase 1: Collect ──────────────────────────────────────────────────────────
if [[ "$ANALYZE_ONLY" == "true" ]]; then
  log "Phase 1/3 — Skipped (--analyze mode). Using: $RAW_LOG"
  LINE_COUNT=$(wc -l < "$RAW_LOG" | tr -d ' ')
  log "Phase 1/3 — Raw log: $LINE_COUNT lines."
else
  log "Phase 1/3 — Starting log collection (Ctrl-C to stop early)..."

  DURATION_SECS=$(( DURATION_MINUTES * 60 ))

  # macOS does not ship timeout(1) (it lives in GNU coreutils).
  # Use a background job + sleep instead — portable across macOS and Linux.
  docker compose logs -f --no-log-prefix "$SERVICE" 2>/dev/null \
    | sed 's/^[a-zA-Z0-9_-]*-[0-9]* *| //' \
    > "$RAW_LOG" &
  COLLECTOR_PID=$!

  stop_collector() {
    kill "$COLLECTOR_PID" 2>/dev/null || true
    wait "$COLLECTOR_PID" 2>/dev/null || true
  }

  # On Ctrl-C or SIGTERM, stop the collector then fall through to analysis.
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

# jq exits with code 5 on invalid/truncated JSON (common in live-collected logs).
# Filter to valid JSON only before any analysis to prevent pipeline crashes.
log "Phase 1/3 — Filtering to valid JSON lines..."
jq -Rc 'fromjson?' "$RAW_LOG" > "$CLEAN_LOG" 2>/dev/null || true
LINE_COUNT_CLEAN=$(wc -l < "$CLEAN_LOG" | tr -d ' ')
log "Phase 1/3 — Valid JSON: $LINE_COUNT_CLEAN / $LINE_COUNT lines."

# ── Phase 2: Pre-analysis ─────────────────────────────────────────────────────
log "Phase 2/3 — Analysing logs..."

# Helper: count lines matching a jq filter (uses CLEAN_LOG — valid JSON only)
count_jq() { jq -c "$1" "$CLEAN_LOG" 2>/dev/null | wc -l | tr -d ' '; }

# Helper: check if a numeric field is stubbed
# Prints "STUBBED:<value>:<count>:<pct>" or "VARIES"
check_stub() {
  local field="$1"
  # Extract all non-null numeric values for the field
  local values
  values=$(jq -r "select(.$field != null) | .$field | tostring" "$CLEAN_LOG" 2>/dev/null)
  local total
  total=$(echo "$values" | grep -c . || true)
  if [[ "$total" -lt "$MIN_SAMPLES" ]]; then
    echo "INSUFFICIENT_SAMPLES:$total"
    return
  fi
  # Most frequent value and its count
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

# ── Counts ───────────────────────────────────────────────────────────────────
TOTAL_JSON=$LINE_COUNT_CLEAN
TOTAL_UNSTRUCTURED=$(( LINE_COUNT - LINE_COUNT_CLEAN ))

TOTAL_ERROR=$(count_jq 'select(.level == "ERROR")')
TOTAL_WARN=$(count_jq 'select(.level == "WARN")')
TOTAL_PANIC=$(count_jq 'select(.level == "PANIC" or .level == "FATAL")')

UNIQUE_TRACES=$(jq -r 'select(.trace_id != null) | .trace_id' "$CLEAN_LOG" 2>/dev/null | sort -u | wc -l | tr -d ' ')

# Stage counts (R3 pipeline completeness)
COUNT_INGESTION=$(count_jq 'select(.msg == "solana_ingestion_emitted" or .msg == "dex_pool_detected")')
COUNT_DQ=$(count_jq 'select(.msg == "dq_decision")')
COUNT_FEATURES=$(count_jq 'select(.msg == "features_extracted")')
COUNT_EDGE=$(count_jq 'select(.msg == "edge_decision")')
COUNT_PROB=$(count_jq 'select(.msg == "probability_scored")')
COUNT_SLIP=$(count_jq 'select(.msg == "slippage_estimated")')
COUNT_VAL=$(count_jq 'select(.msg == "validation_decision")')
COUNT_SEL=$(count_jq 'select(.msg == "selection_decision")')
COUNT_ALLOC=$(count_jq 'select(.msg == "allocation_decision")')
COUNT_EXEC_SUB=$(count_jq 'select(.msg == "execution_submitted")')
COUNT_EXEC_CON=$(count_jq 'select(.msg == "execution_confirmed")')
COUNT_POS_OPEN=$(count_jq 'select(.msg == "position_opened")')
COUNT_POS_CLOSE=$(count_jq 'select(.msg == "position_closed")')
COUNT_LEARN=$(count_jq 'select(.msg == "learning_record_emitted")')

COUNT_VAL_ACCEPT=$(count_jq 'select(.msg == "validation_decision" and .decision == "ACCEPT")')
COUNT_VAL_REJECT=$(count_jq 'select(.msg == "validation_decision" and .decision == "REJECT")')
COUNT_JOIN_TIMEOUT=$(count_jq 'select(.msg == "validation_decision" and (.reject_reason // "" | test("join_timeout")))')

# Stub checks (R5)
STUB_PROB=$(check_stub "probability")
STUB_PROB_USED=$(check_stub "probability_used")
STUB_RISK=$(check_stub "risk_score")
STUB_EV=$(check_stub "ev_bps")
STUB_P50=$(check_stub "p50_bps")
STUB_P95=$(check_stub "p95_bps")
STUB_EDGE_STR=$(check_stub "edge_strength")

# Missing trace_id check
MISSING_TRACE=$(count_jq 'select(.trace_id == null or .trace_id == "")')
MISSING_VERSION=$(count_jq 'select(.version_id == null or .version_id == "")')

# Duplicate event_id check — only flag TRUE idempotency violations where the
# same event_id is consumed more than once by the same worker processing step.
# Excluded messages carry the INPUT event_id as context (not as their own id):
#   - stage_completed      : reuses consumed event_id for trace correlation;
#                            gate_review_collect.sh uses worker_group +
#                            output_status on stage_completed as canonical
#                            per-worker health evidence (not legacy .msg names)
#   - market_probe_failed  : logs input event_id while reporting a probe error
#   - market_probes_completed : logs input event_id for the probe summary
#   - solana_ingestion_emitted: the source event — its id is legitimately
#                               referenced by downstream probe log lines
# Counting these as "duplicates" produces a false-positive on every token that
# triggers a probe failure, incorrectly reducing D6 and inflating OPEN_CRITICAL.
DUP_EVENT_IDS=$(jq -r 'select(
    .event_id != null and
    .msg != "stage_completed" and
    .msg != "market_probe_failed" and
    .msg != "market_probes_completed" and
    .msg != "solana_ingestion_emitted"
  ) | .event_id' "$CLEAN_LOG" 2>/dev/null \
  | sort | uniq -d | wc -l | tr -d ' ')

# Heartbeat zero-emitted check
HB_ZERO_EMITTED=$(count_jq 'select(.msg | test("_heartbeat")) | select(.events_emitted == 0)')

# ── Rescan Worker (Layer 0.5) ─────────────────────────────────────────────────
COUNT_RESCAN_STARTED=$(count_jq 'select(.msg == "rescan_worker_started")')
COUNT_RESCAN_DISABLED=$(count_jq 'select(.msg == "rescan_worker_disabled")')
COUNT_RESCAN_TICK_ERR=$(count_jq 'select(.msg == "rescan_tick_error")')
COUNT_RESCAN_HB_ZERO=$(count_jq 'select(.msg == "rescan_worker_heartbeat" and .events_emitted == 0)')
COUNT_RESCAN_EMITTED=$(count_jq 'select(.transport != null and (.transport | test("^rescan_")))')
COUNT_RESCAN_15M=$(count_jq 'select(.transport == "rescan_15m")')
COUNT_RESCAN_30M=$(count_jq 'select(.transport == "rescan_30m")')
COUNT_RESCAN_45M=$(count_jq 'select(.transport == "rescan_45m")')
COUNT_RESCAN_1H=$(count_jq 'select(.transport == "rescan_1h")')
COUNT_RESCAN_BAND_ZERO=$(count_jq 'select(.msg == "rescan_band_completed" and (.candidates // 1) == 0)')
COUNT_RESCAN_EMIT_FAIL=$(count_jq 'select(.msg == "rescan_emit_failed")')
COUNT_RESCAN_DUP=$(jq -r 'select(.transport != null and (.transport | test("^rescan_"))) | "\(.token_address // "")|\(.transport // "")"' "$CLEAN_LOG" 2>/dev/null \
  | sort | uniq -d | wc -l | tr -d ' ')
COUNT_RESCAN_EDGE=$(count_jq 'select(.msg == "edge_decision" and (.transport // "" | test("^rescan_")))')

# Rescan emit-fail rate for non-tolerable check
RESCAN_ORPHAN_RISK="OK"
if [[ "$COUNT_RESCAN_EMITTED" -gt 0 && "$COUNT_RESCAN_EMIT_FAIL" -gt 0 ]]; then
  RESCAN_FAIL_PCT=$(( COUNT_RESCAN_EMIT_FAIL * 100 / COUNT_RESCAN_EMITTED ))
  [[ "$RESCAN_FAIL_PCT" -gt 5 ]] && RESCAN_ORPHAN_RISK="${RESCAN_FAIL_PCT}% emit failures"
fi

# Rescan enabled + worker alive but producing zero output
RESCAN_SILENT_RISK="OK"
if [[ "$COUNT_RESCAN_STARTED" -gt 0 && "$COUNT_RESCAN_EMITTED" -eq 0 ]]; then
  RESCAN_SILENT_RISK="STUCK — worker started but 0 rescan events emitted"
fi

# ── PRS Dimension Scoring ─────────────────────────────────────────────────────
# Each dimension: 10=full, 5=partial, 0=fail
D1=0; D2=0; D3=0; D4=0; D5=0; D6=0; D7=0; D8=0; D9=0; D10=0

# D1 — Pipeline completeness
# Full score requires end-to-end trace AND rescan either disabled or ≥1 trace reaches edge_decision
RESCAN_D1_OK=0
[[ "$COUNT_RESCAN_STARTED" -eq 0 || "$COUNT_RESCAN_DISABLED" -gt 0 || "$COUNT_RESCAN_EDGE" -gt 0 ]] && RESCAN_D1_OK=1
if [[ "$COUNT_POS_OPEN" -gt 0 && "$RESCAN_D1_OK" -eq 1 ]]; then D1=10
elif [[ "$COUNT_VAL" -gt 0 ]]; then D1=5
fi

# D2 — Data quality not stubbed
if [[ "$STUB_RISK" == "VARIES" ]]; then D2=10
elif [[ "$STUB_RISK" == INSUFFICIENT_SAMPLES* ]]; then D2=5
else D2=0; fi

# D3 — Feature signals vary
FEATURES_VARY=$(jq -r 'select(.msg=="features_extracted") | .tx_velocity_score // empty' "$CLEAN_LOG" 2>/dev/null \
  | sort -u | wc -l | tr -d ' ')
if [[ "$FEATURES_VARY" -gt 5 ]]; then D3=10
elif [[ "$FEATURES_VARY" -gt 1 ]]; then D3=5
fi

# D4 — Probability model real
if [[ "$STUB_PROB_USED" == "VARIES" && "$COUNT_JOIN_TIMEOUT" -eq 0 ]]; then D4=10
elif [[ "$STUB_PROB_USED" == "VARIES" ]]; then D4=5
fi

# D5 — Slippage model calibrated
if [[ "$STUB_P50" == "VARIES" && "$STUB_P95" == "VARIES" ]]; then D5=10
elif [[ "$STUB_P50" == "VARIES" || "$STUB_P95" == "VARIES" ]]; then D5=5
fi

# D6 — Capital safety: no CRITICAL findings (PANIC/FATAL + no dup event_id)
if [[ "$TOTAL_PANIC" -eq 0 && "$DUP_EVENT_IDS" -eq 0 ]]; then D6=10
elif [[ "$TOTAL_PANIC" -eq 0 ]]; then D6=5
fi

# D7 — Execution engine observed
if [[ "$COUNT_EXEC_CON" -gt 0 ]]; then D7=10
elif [[ "$COUNT_EXEC_SUB" -gt 0 ]]; then D7=5
fi

# D8 — Learning records present
if [[ "$COUNT_LEARN" -ge 30 ]]; then D8=10
elif [[ "$COUNT_LEARN" -gt 0 ]]; then D8=5
fi

# D9 — Probe coverage (heuristic: any *Known field = true in raw log)
KNOWN_TRUE=$(count_jq 'select(.lp_stats_known == true or .holder_dist_known == true or .wash_stats_known == true)')
if [[ "$KNOWN_TRUE" -gt 10 ]]; then D9=10
elif [[ "$KNOWN_TRUE" -gt 0 ]]; then D9=5
else D9=2; fi   # scaffold present but not activated

# D10 — Live P&L evidence
EV_POSITIVE=$(count_jq 'select(.msg == "position_closed" and (.realized_ev_bps // -1) > 0)')
EV_NEGATIVE=$(count_jq 'select(.msg == "position_closed" and (.realized_ev_bps // 0) <= 0)')
if [[ "$COUNT_POS_CLOSE" -ge 30 && "$EV_POSITIVE" -gt "$EV_NEGATIVE" ]]; then D10=10
elif [[ "$COUNT_POS_CLOSE" -gt 0 ]]; then D10=5
fi

PRS=$(( D1 + D2 + D3 + D4 + D5 + D6 + D7 + D8 + D9 + D10 ))

# PRS tier
if [[ "$PRS" -ge 90 ]]; then PRS_TIER="OPTIMIZED"
elif [[ "$PRS" -ge 80 ]]; then PRS_TIER="SUSTAINABLE"
elif [[ "$PRS" -ge 65 ]]; then PRS_TIER="LAUNCH_ALLOWED"
elif [[ "$PRS" -ge 50 ]]; then PRS_TIER="CAUTION"
else PRS_TIER="BLOCKED"
fi

# Open critical count (simplified — count non-tolerable patterns)
OPEN_CRITICAL=0
[[ "$TOTAL_PANIC" -gt 0 ]] && (( OPEN_CRITICAL++ )) || true
[[ "$DUP_EVENT_IDS" -gt 0 ]] && (( OPEN_CRITICAL++ )) || true
[[ "$STUB_PROB_USED" != "VARIES" && "$STUB_PROB_USED" != INSUFFICIENT_SAMPLES* ]] && (( OPEN_CRITICAL++ )) || true
[[ "$STUB_RISK" != "VARIES" && "$STUB_RISK" != INSUFFICIENT_SAMPLES* ]] && (( OPEN_CRITICAL++ )) || true
[[ "$STUB_P50" != "VARIES" && "$STUB_P50" != INSUFFICIENT_SAMPLES* && "$STUB_P95" != "VARIES" && "$STUB_P95" != INSUFFICIENT_SAMPLES* ]] && (( OPEN_CRITICAL++ )) || true
if [[ "$COUNT_VAL_REJECT" -gt 0 && "$COUNT_VAL" -gt 0 ]]; then
  REJECT_PCT=$(( COUNT_VAL_REJECT * 100 / COUNT_VAL ))
  [[ "$REJECT_PCT" -gt 95 ]] && (( OPEN_CRITICAL++ )) || true
fi
[[ "$COUNT_RESCAN_DUP" -gt 0 ]] && (( OPEN_CRITICAL++ )) || true
[[ "$RESCAN_SILENT_RISK" != "OK" ]] && (( OPEN_CRITICAL++ )) || true

# ── Phase 3: Write Summary ────────────────────────────────────────────────────
log "Phase 3/3 — Writing summary to $SUMMARY"

{
echo "# Log-Reviewer Pre-Analysis Summary"
echo "# Generated by scripts/collect_logs.sh"
echo "# Service: $SERVICE | Duration: ${DURATION_MINUTES}m | Timestamp: $TIMESTAMP"
echo "# Raw log: $RAW_LOG"
echo ""
echo "═══════════════════════════════════════════════════════════════════"
echo "PRODUCTION READINESS SCORE (PRS)"
echo "═══════════════════════════════════════════════════════════════════"
echo "  PRS:            $PRS / 100"
echo "  Tier:           $PRS_TIER"
echo "  Open Critical:  $OPEN_CRITICAL"
echo ""
echo "  Dimension Breakdown:"
echo "    D1  Pipeline completeness:   $D1/10"
echo "    D2  Data quality:            $D2/10"
echo "    D3  Feature signals:         $D3/10"
echo "    D4  Probability model:       $D4/10"
echo "    D5  Slippage model:          $D5/10"
echo "    D6  Capital safety:          $D6/10"
echo "    D7  Execution engine:        $D7/10"
echo "    D8  Learning/adaptation:     $D8/10"
echo "    D9  Probe coverage:          $D9/10"
echo "    D10 Live P&L evidence:       $D10/10"
echo ""
if [[ "$PRS" -ge 65 && "$OPEN_CRITICAL" -eq 0 ]]; then
echo "  *** VERDICT: PROFITABLE_AND_READY_TO_LAUNCH ***"
echo "  All CRITICAL/HIGH code defects resolved."
echo "  Remaining items (D8/D9/D10) are operational — self-calibrate as trades run."
elif [[ "$PRS_TIER" == "BLOCKED" ]]; then
echo "  VERDICT: BLOCKED — Do not trade. $OPEN_CRITICAL critical issue(s) found."
else
echo "  VERDICT: $PRS_TIER — Delta to LAUNCH_ALLOWED: $(( 65 - PRS > 0 ? 65 - PRS : 0 )) pts"
fi
echo ""
echo "═══════════════════════════════════════════════════════════════════"
echo "WINDOW STATISTICS"
echo "═══════════════════════════════════════════════════════════════════"
echo "  Total lines:         $LINE_COUNT"
echo "  Valid JSON:          $TOTAL_JSON"
echo "  Unstructured:        $TOTAL_UNSTRUCTURED"
echo "  Unique trace_ids:    $UNIQUE_TRACES"
echo ""
echo "  ERROR lines:         $TOTAL_ERROR"
echo "  WARN  lines:         $TOTAL_WARN"
echo "  PANIC/FATAL lines:   $TOTAL_PANIC"
echo ""
echo "═══════════════════════════════════════════════════════════════════"
echo "PIPELINE STAGE COUNTS (R3)"
echo "═══════════════════════════════════════════════════════════════════"
echo "  L0  ingestion:             $COUNT_INGESTION"
echo "  L0.5 rescan_emitted:      $COUNT_RESCAN_EMITTED  (15m=$COUNT_RESCAN_15M 30m=$COUNT_RESCAN_30M 45m=$COUNT_RESCAN_45M 1h=$COUNT_RESCAN_1H)"
echo "  L1  dq_decision:           $COUNT_DQ"
echo "  L2  features_extracted:    $COUNT_FEATURES"
echo "  L3  edge_decision:         $COUNT_EDGE"
echo "  L4  probability_scored:    $COUNT_PROB"
echo "  L4  slippage_estimated:    $COUNT_SLIP"
echo "  L5  validation_decision:   $COUNT_VAL  (ACCEPT=$COUNT_VAL_ACCEPT  REJECT=$COUNT_VAL_REJECT)"
echo "  L6  selection_decision:    $COUNT_SEL"
echo "  L7  allocation_decision:   $COUNT_ALLOC"
echo "  L8  execution_submitted:   $COUNT_EXEC_SUB"
echo "  L8  execution_confirmed:   $COUNT_EXEC_CON"
echo "  L9  position_opened:       $COUNT_POS_OPEN"
echo "  L9  position_closed:       $COUNT_POS_CLOSE"
echo "  L10 learning_record:       $COUNT_LEARN"
echo ""
echo "═══════════════════════════════════════════════════════════════════"
echo "STUB DETECTION (R5)"
echo "═══════════════════════════════════════════════════════════════════"
echo "  probability (L4):          $STUB_PROB"
echo "  probability_used (L5):     $STUB_PROB_USED"
echo "  risk_score (L1):           $STUB_RISK"
echo "  ev_bps (L5):               $STUB_EV"
echo "  p50_bps slippage (L4):     $STUB_P50"
echo "  p95_bps slippage (L4):     $STUB_P95"
echo "  edge_strength (L3):        $STUB_EDGE_STR"
echo ""
echo "═══════════════════════════════════════════════════════════════════"
echo "INVARIANT CHECKS (R4)"
echo "═══════════════════════════════════════════════════════════════════"
echo "  join_timeout rejects:      $COUNT_JOIN_TIMEOUT"
echo "  Missing trace_id:          $MISSING_TRACE"
echo "  Missing version_id:        $MISSING_VERSION"
echo "  Duplicate event_ids:       $DUP_EVENT_IDS"
echo "  Heartbeat zero-emitted:    $HB_ZERO_EMITTED"
echo ""
echo "═══════════════════════════════════════════════════════════════════"
echo "RESCAN WORKER (Layer 0.5)"
echo "═══════════════════════════════════════════════════════════════════"
if [[ "$COUNT_RESCAN_STARTED" -gt 0 ]]; then
echo "  Status:                ENABLED (worker started in window)"
elif [[ "$COUNT_RESCAN_DISABLED" -gt 0 ]]; then
echo "  Status:                DISABLED (rescan_worker_disabled observed)"
else
echo "  Status:                not observed in window"
fi
echo "  rescan_worker_started: $COUNT_RESCAN_STARTED"
echo "  rescan_tick_errors:    $COUNT_RESCAN_TICK_ERR"
echo "  rescan_hb_zero_emit:   $COUNT_RESCAN_HB_ZERO"
echo "  Total emitted:         $COUNT_RESCAN_EMITTED"
echo "    band 15m:            $COUNT_RESCAN_15M"
echo "    band 30m:            $COUNT_RESCAN_30M"
echo "    band 45m:            $COUNT_RESCAN_45M"
echo "    band 1h:             $COUNT_RESCAN_1H"
echo "  Band-completed-zero:   $COUNT_RESCAN_BAND_ZERO"
echo "  Emit-failed:           $COUNT_RESCAN_EMIT_FAIL"
echo "  Duplicate (addr+band): $COUNT_RESCAN_DUP"
echo "  Rescan→edge_decision:  $COUNT_RESCAN_EDGE"
echo ""
echo "═══════════════════════════════════════════════════════════════════"
echo "NON-TOLERABLE PATTERN CHECK"
echo "═══════════════════════════════════════════════════════════════════"
# Check each non-tolerable condition and print status
check_nt() {
  local label="$1"; local result="$2"; local pass="$3"
  if [[ "$result" == "$pass" ]]; then
    printf "  %-40s OK\n" "$label"
  else
    printf "  %-40s *** NON-TOLERABLE: %s ***\n" "$label" "$result"
  fi
}
[[ "$TOTAL_PANIC" -eq 0 ]] && NT_PANIC="NONE" || NT_PANIC="${TOTAL_PANIC} lines"
check_nt "PANIC/FATAL lines"              "$NT_PANIC"           "NONE"
[[ "$STUB_PROB_USED" == "VARIES" || "$STUB_PROB_USED" == INSUFFICIENT_SAMPLES* ]] \
  && NT_PROB="OK" || NT_PROB="STUBBED — probability_used constant"
check_nt "probability_used not constant"  "$NT_PROB"            "OK"
[[ "$STUB_RISK" == "VARIES" || "$STUB_RISK" == INSUFFICIENT_SAMPLES* ]] \
  && NT_RISK="OK" || NT_RISK="STUBBED — risk_score constant"
check_nt "risk_score not constant"        "$NT_RISK"            "OK"
[[ "$STUB_P50" == "VARIES" || "$STUB_P50" == INSUFFICIENT_SAMPLES* || "$STUB_P95" == "VARIES" || "$STUB_P95" == INSUFFICIENT_SAMPLES* ]] \
  && NT_SLIP="OK" || NT_SLIP="STUBBED — p50/p95 both constant"
check_nt "slippage p50/p95 not constant"  "$NT_SLIP"            "OK"
[[ "$DUP_EVENT_IDS" -eq 0 ]] && NT_DUP="NONE" || NT_DUP="${DUP_EVENT_IDS} duplicate(s)"
check_nt "Duplicate event_ids"            "$NT_DUP"             "NONE"
if [[ "$COUNT_VAL" -gt 0 ]]; then
  REJECT_PCT=$(( COUNT_VAL_REJECT * 100 / COUNT_VAL ))
  [[ "$REJECT_PCT" -le 95 ]] && NT_REJ="OK (${REJECT_PCT}%)" || NT_REJ="${REJECT_PCT}% reject rate"
  check_nt "Reject rate ≤95%"             "$NT_REJ"             "OK (${REJECT_PCT}%)"
else
  printf "  %-40s N/A (no validation events)\n" "Reject rate ≤95%"
fi
[[ "$COUNT_RESCAN_DUP" -eq 0 ]] && NT_RSCAN_DUP="NONE" || NT_RSCAN_DUP="${COUNT_RESCAN_DUP} duplicate(s)"
check_nt "Rescan duplicate (addr+band)"   "$NT_RSCAN_DUP"       "NONE"
check_nt "Rescan emit-fail rate"          "$RESCAN_ORPHAN_RISK"  "OK"
check_nt "Rescan enabled but no output"   "$RESCAN_SILENT_RISK"  "OK"
echo ""
echo "═══════════════════════════════════════════════════════════════════"
echo "HOW TO USE THIS SUMMARY WITH COPILOT LOG-REVIEWER"
echo "═══════════════════════════════════════════════════════════════════"
echo ""
echo "  1. Open a new GitHub Copilot chat in VS Code."
echo "  2. Paste the following message:"
echo ""
echo "     Review the log summary below using the log-reviewer skill."
echo "     Produce a full verdict with findings, plan, and Confirmation Gate."
echo ""
echo "     <paste the full content of this file>"
echo ""
echo "  Or use the shortcut: @log-reviewer file:output/logs/summary_${TIMESTAMP}.txt"
echo ""
echo "  Raw log for deep analysis: $RAW_LOG"
echo "═══════════════════════════════════════════════════════════════════"
} > "$SUMMARY"

# Write machine-readable PRS JSON for future tooling
{
  echo "{"
  echo "  \"timestamp\": \"$TIMESTAMP\","
  echo "  \"service\": \"$SERVICE\","
  echo "  \"duration_minutes\": $DURATION_MINUTES,"
  echo "  \"prs\": $PRS,"
  echo "  \"prs_tier\": \"$PRS_TIER\","
  echo "  \"open_critical\": $OPEN_CRITICAL,"
  echo "  \"dimensions\": {"
  echo "    \"d1_pipeline_completeness\": $D1,"
  echo "    \"d2_data_quality\": $D2,"
  echo "    \"d3_feature_signals\": $D3,"
  echo "    \"d4_probability_model\": $D4,"
  echo "    \"d5_slippage_model\": $D5,"
  echo "    \"d6_capital_safety\": $D6,"
  echo "    \"d7_execution_engine\": $D7,"
  echo "    \"d8_learning\": $D8,"
  echo "    \"d9_probe_coverage\": $D9,"
  echo "    \"d10_live_pnl\": $D10"
  echo "  },"
  echo "  \"raw_log\": \"$RAW_LOG\","
  echo "  \"summary\": \"$SUMMARY\""
  echo "}"
} > "$PRS_SNAPSHOT"

log "Phase 3/3 — Done."
echo ""
echo "════════════════════════════════════════════"
echo "  PRS: $PRS / 100   Tier: $PRS_TIER   Critical: $OPEN_CRITICAL"
echo "  Summary: $SUMMARY"
echo "  PRS JSON: $PRS_SNAPSHOT"
echo "════════════════════════════════════════════"
echo ""
echo "Next step: paste $SUMMARY into a new Copilot chat"
echo "and say: 'Review this using the log-reviewer skill'"
