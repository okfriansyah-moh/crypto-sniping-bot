#!/usr/bin/env bash
# run_pipeline_proof_mock.sh — Prove L0→L10 without Helius live traffic
#
# Two modes:
#   offline (default) — analyze a synthetic full-trace log fixture; no Docker/DB/Helius
#   live              — inject a known-good token into Postgres, wait for L10, validate
#
# Usage:
#   ./scripts/run_pipeline_proof_mock.sh                    # offline fixture proof
#   ./scripts/run_pipeline_proof_mock.sh offline
#   ./scripts/run_pipeline_proof_mock.sh live               # requires stack + DATABASE_URL
#   ./scripts/run_pipeline_proof_mock.sh live --token <ADDR>
#
# Environment (live mode):
#   DATABASE_URL     — PostgreSQL DSN (required unless built from SNIPER_DB_* below)
#   SNIPER_DB_USER, SNIPER_DB_PASSWORD, SNIPER_DB_HOST, SNIPER_DB_PORT, SNIPER_DB_NAME
#   WAIT_SECS        — max seconds to wait for L10 (default: 180)
#   SVC              — docker compose service (default: sniper-bot)
#
# Exit 0 when validate_pipeline_proof.sh returns SHADOW_READY.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="$REPO_ROOT/output/logs"
FIXTURE_LOG="$REPO_ROOT/tests/fixtures/gate_pipeline_proof_pass.log"
INJECT_SCRIPT="$REPO_ROOT/scripts/inject_test_token.py"
COLLECT_SCRIPT="$REPO_ROOT/scripts/gate_review_collect.sh"
VALIDATE_SCRIPT="$REPO_ROOT/scripts/validate_pipeline_proof.sh"

DEFAULT_TOKEN="GateProofMockToken1111111111111111111111"
DEFAULT_CHAIN="solana"
WAIT_SECS="${WAIT_SECS:-420}"
SVC="${SVC:-sniper-bot}"

log() { echo "[pipeline_proof_mock] $*" >&2; }
die() { log "FATAL: $*"; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "'$1' not found"
}

build_database_url() {
  if [[ -n "${DATABASE_URL:-}" ]]; then
    return 0
  fi
  local user pass host port db
  user="${SNIPER_DB_USER:-sniper}"
  pass="${SNIPER_DB_PASSWORD:-}"
  host="${SNIPER_DB_HOST:-localhost}"
  port="${SNIPER_DB_PORT:-5432}"
  db="${SNIPER_DB_NAME:-sniper}"
  if [[ -z "$pass" ]]; then
    die "DATABASE_URL unset and SNIPER_DB_PASSWORD empty — export DATABASE_URL or SNIPER_DB_*"
  fi
  local enc_user enc_pass
  enc_user="$(python3 -c "import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1], safe=''))" "$user")"
  enc_pass="$(python3 -c "import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1], safe=''))" "$pass")"
  export DATABASE_URL="postgres://${enc_user}:${enc_pass}@${host}:${port}/${db}"
}

trace_id_for() {
  local chain="$1" token="$2"
  python3 -c "import hashlib,sys; c,t=sys.argv[1:3]; print(hashlib.sha256(f'replay-trace:{c}|{t}'.encode()).hexdigest()[:16])" "$chain" "$token"
}

run_offline() {
  require_cmd jq
  require_cmd bash
  [[ -f "$FIXTURE_LOG" ]] || die "fixture missing: $FIXTURE_LOG"

  log "Mode: offline — analyzing synthetic L0→L10 fixture (no Helius/Docker)"
  mkdir -p "$OUTPUT_DIR"

  bash "$COLLECT_SCRIPT" --analyze "$FIXTURE_LOG" PIPELINE_PROOF
  bash "$VALIDATE_SCRIPT"
}

run_live() {
  require_cmd docker
  require_cmd python3
  require_cmd jq

  local token="$DEFAULT_TOKEN"
  local chain="$DEFAULT_CHAIN"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --token) token="${2:-}"; shift 2 ;;
      --chain) chain="${2:-}"; shift 2 ;;
      *) die "unknown live arg: $1" ;;
    esac
  done
  [[ -n "$token" ]] || die "--token must not be empty"

  build_database_url
  local trace_id
  trace_id="$(trace_id_for "$chain" "$token")"

  log "Mode: live — injecting known-good token (bypasses Helius serial-launcher SKIP)"
  log "  chain=$chain token=$token trace_id=$trace_id wait=${WAIT_SECS}s service=$SVC"

  if ! docker compose ps --status running --services 2>/dev/null | grep -qx "$SVC"; then
    die "docker compose service '$SVC' is not running — start the stack first"
  fi

  local wait_start
  wait_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  local mock_symbol mock_name mock_market
  mock_symbol="$(python3 -c "import hashlib,sys; print('GPM'+hashlib.sha256(sys.argv[1].encode()).hexdigest()[:6].upper())" "$token")"
  mock_name="Gate Proof Mock ${token: -8}"
  mock_market="proof-mock-${trace_id}"

  python3 "$INJECT_SCRIPT" \
    --chain "$chain" \
    --token "$token" \
    --symbol "$mock_symbol" \
    --name "$mock_name" \
    --market "$mock_market" \
    --enriched-only \
    || die "inject_test_token.py failed"

  log "Waiting up to ${WAIT_SECS}s for learning_record_emitted trace_id=$trace_id ..."
  local deadline=$((SECONDS + WAIT_SECS))
  local found=false
  while (( SECONDS < deadline )); do
    if docker compose logs --no-color --since "$wait_start" "$SVC" 2>/dev/null \
      | grep -F '"msg":"learning_record_emitted"' \
      | grep -F "\"trace_id\":\"$trace_id\"" \
      | grep -q .; then
      found=true
      break
    fi
    sleep 3
  done

  if [[ "$found" != "true" ]]; then
    log "L10 not observed in logs — dumping recent stage_completed / dq_decision hints:"
    docker compose logs --no-color --tail 80 "$SVC" 2>/dev/null \
      | grep -E '"msg":"(dq_decision|stage_completed|learning_record_emitted)"' \
      | tail -20 >&2 || true
    die "timeout: no learning_record_emitted for trace_id=$trace_id within ${WAIT_SECS}s"
  fi

  local timestamp raw_log
  timestamp="$(date +%Y%m%d_%H%M%S)"
  raw_log="$OUTPUT_DIR/gate_raw_mock_${timestamp}.log"
  mkdir -p "$OUTPUT_DIR"
  # Strip docker compose log prefix ("bot-1  | ") so gate_review_collect can parse NDJSON.
  docker compose logs --no-color --since "$wait_start" "$SVC" 2>/dev/null \
    | sed -E 's/^[^|]*\|[[:space:]]*//' >"$raw_log"

  log "Captured $(wc -l <"$raw_log" | tr -d ' ') log lines → $raw_log"
  bash "$COLLECT_SCRIPT" --analyze "$raw_log" PIPELINE_PROOF
  bash "$VALIDATE_SCRIPT"
}

MODE="${1:-offline}"
shift || true

case "$MODE" in
  offline|fixture|mock)
    run_offline
    ;;
  live|inject)
    run_live "$@"
    ;;
  -h|--help|help)
    sed -n '2,22p' "$0"
    exit 0
    ;;
  *)
    die "unknown mode '$MODE' — use offline or live (see --help)"
    ;;
esac

log "Pipeline proof mock PASSED (SHADOW_READY)"
