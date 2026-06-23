#!/usr/bin/env bash
# webhook_verify.sh — smoke-test Helius webhook ingress (no Helius credits).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT}/.env"

if [[ -f "$ENV_FILE" ]]; then
	# shellcheck disable=SC1090
	set -a
	source "$ENV_FILE"
	set +a
fi

BASE_URL="${WEBHOOK_PUBLIC_URL:-${1:-http://localhost:${PORT:-8080}}}"
BASE_URL="${BASE_URL%/}"
SECRET="${HELIUS_WEBHOOK_SECRET:-}"

if [[ -z "$SECRET" ]]; then
	echo "ERROR: HELIUS_WEBHOOK_SECRET is not set (source .env or export it)" >&2
	exit 1
fi

WEBHOOK_URL="${BASE_URL}/webhooks/helius"
HEALTH_URL="${BASE_URL}/health"

failures=0
check() {
	local name="$1"
	local code="$2"
	local expect="$3"
	if [[ "$code" == "$expect" ]]; then
		echo "  OK   $name (HTTP $code)"
	else
		echo "  FAIL $name (HTTP $code, want $expect)" >&2
		failures=$((failures + 1))
	fi
}

echo "Webhook verify — base URL: $BASE_URL"

code=$(curl -s -o /dev/null -w "%{http_code}" "$HEALTH_URL" || echo "000")
check "GET /health" "$code" "200"

code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$WEBHOOK_URL" \
	-H "Content-Type: application/json" -d '[]' || echo "000")
check "POST /webhooks/helius without auth" "$code" "401"

code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$WEBHOOK_URL" \
	-H "Authorization: $SECRET" \
	-H "Content-Type: application/json" -d '[]' || echo "000")
if [[ "$code" == "401" ]]; then
	echo "  FAIL POST /webhooks/helius with auth (HTTP 401 — secret mismatch?)" >&2
	failures=$((failures + 1))
elif [[ "$code" == "400" || "$code" == "200" ]]; then
	echo "  OK   POST /webhooks/helius with auth (HTTP $code)"
else
	echo "  WARN POST /webhooks/helius with auth (HTTP $code — expected 400 or 200)" >&2
fi

# Oversize body (65 KiB + 1) should return 413 when handler is reachable with auth.
big_payload="$(python3 -c "print('x' * 70000)")"
code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$WEBHOOK_URL" \
	-H "Authorization: $SECRET" \
	-H "Content-Type: application/json" \
	-d "$big_payload" || echo "000")
if [[ "$code" == "413" ]]; then
	echo "  OK   POST oversize body (HTTP 413)"
elif [[ "$code" == "401" ]]; then
	echo "  SKIP POST oversize (auth failed first)" >&2
else
	echo "  WARN POST oversize body (HTTP $code — expected 413)" >&2
fi

if [[ "$failures" -gt 0 ]]; then
	echo ""
	echo "Webhook verify failed ($failures check(s))." >&2
	exit 1
fi

echo ""
echo "Webhook verify passed."
